//go:build darwin

package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// macOS kqueue needs an fd for every file, not just every directory. A Go
// checkout exhausted the live client's 10,000-fd limit before opening an admin
// socket. FSEvents watches a whole hierarchy with constant resource use. Use
// the existing purego runtime so CGO_ENABLED=0 release binaries get it too.
var gitFSEvents struct {
	once          sync.Once
	err           error
	stringCreate  func(uintptr, string, uint32) uintptr
	arrayCreate   func(uintptr, unsafe.Pointer, int64, uintptr) uintptr
	release       func(uintptr)
	queueCreate   func(string, uintptr) uintptr
	queueRelease  func(uintptr)
	create        func(uintptr, uintptr, unsafe.Pointer, uintptr, uint64, float64, uint32) uintptr
	setQueue      func(uintptr, uintptr)
	start         func(uintptr) bool
	stop          func(uintptr)
	invalidate    func(uintptr)
	streamRelease func(uintptr)
	callback      uintptr
}

var gitFSCallbacks = struct {
	sync.Mutex
	next      uintptr
	callbacks map[uintptr]func()
}{callbacks: make(map[uintptr]func())}

func loadGitFSEvents() {
	f := &gitFSEvents
	defer func() {
		if p := recover(); p != nil {
			f.err = fmt.Errorf("FSEvents symbols: %v", p)
		}
	}()
	cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		f.err = err
		return
	}
	cs, err := purego.Dlopen("/System/Library/Frameworks/CoreServices.framework/CoreServices", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		f.err = err
		return
	}
	lib, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		f.err = err
		return
	}
	purego.RegisterLibFunc(&f.stringCreate, cf, "CFStringCreateWithCString")
	purego.RegisterLibFunc(&f.arrayCreate, cf, "CFArrayCreate")
	purego.RegisterLibFunc(&f.release, cf, "CFRelease")
	purego.RegisterLibFunc(&f.queueCreate, lib, "dispatch_queue_create")
	purego.RegisterLibFunc(&f.queueRelease, lib, "dispatch_release")
	purego.RegisterLibFunc(&f.create, cs, "FSEventStreamCreate")
	purego.RegisterLibFunc(&f.setQueue, cs, "FSEventStreamSetDispatchQueue")
	purego.RegisterLibFunc(&f.start, cs, "FSEventStreamStart")
	purego.RegisterLibFunc(&f.stop, cs, "FSEventStreamStop")
	purego.RegisterLibFunc(&f.invalidate, cs, "FSEventStreamInvalidate")
	purego.RegisterLibFunc(&f.streamRelease, cs, "FSEventStreamRelease")
	f.callback = purego.NewCallback(func(_ uintptr, info uintptr, _ uintptr, _ uintptr, _ uintptr, _ uintptr) {
		gitFSCallbacks.Lock()
		notify := gitFSCallbacks.callbacks[info]
		gitFSCallbacks.Unlock()
		if notify != nil {
			notify()
		}
	})
}

// nativeGitWatch receives recursive, coalesced events. Even overflow/root
// replacement events trigger a status reread; git, not the event, is truth.
func nativeGitWatch(root string, notify func()) (func(), bool, error) {
	f := &gitFSEvents
	f.once.Do(loadGitFSEvents)
	if f.err != nil {
		return nil, true, f.err
	}
	paths := []string{root}
	gitDir := filepath.Join(root, ".git")
	if data, err := os.ReadFile(gitDir); err == nil {
		if dir, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:"); ok {
			gitDir = strings.TrimSpace(dir)
			if !filepath.IsAbs(gitDir) {
				gitDir = filepath.Join(root, gitDir)
			}
			paths = append(paths, gitDir)
			if data, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
				dir := strings.TrimSpace(string(data))
				if !filepath.IsAbs(dir) {
					dir = filepath.Join(gitDir, dir)
				}
				paths = append(paths, dir)
			}
		}
	}
	refs := make([]uintptr, 0, len(paths))
	for _, path := range paths {
		ref := f.stringCreate(0, path, 0x08000100) // UTF-8
		if ref == 0 {
			for _, r := range refs {
				f.release(r)
			}
			return nil, true, fmt.Errorf("FSEvents path allocation failed")
		}
		refs = append(refs, ref)
	}
	array := f.arrayCreate(0, unsafe.Pointer(&refs[0]), int64(len(refs)), 0)
	if array == 0 {
		for _, r := range refs {
			f.release(r)
		}
		return nil, true, fmt.Errorf("FSEvents path array allocation failed")
	}
	gitFSCallbacks.Lock()
	gitFSCallbacks.next++
	id := gitFSCallbacks.next
	gitFSCallbacks.callbacks[id] = notify
	gitFSCallbacks.Unlock()
	// FSEventStreamContext: version, info, retain, release, copyDescription.
	context := [5]uintptr{0, id, 0, 0, 0}
	stream := f.create(0, f.callback, unsafe.Pointer(&context[0]), array, ^uint64(0), 0.15, 0x4) // WatchRoot; no per-file descriptors
	queue := f.queueCreate("tuios.git-events", 0)
	cleanup := func() {
		gitFSCallbacks.Lock()
		delete(gitFSCallbacks.callbacks, id)
		gitFSCallbacks.Unlock()
		if stream != 0 {
			f.invalidate(stream)
			f.streamRelease(stream)
		}
		if queue != 0 {
			f.queueRelease(queue)
		}
		f.release(array)
		for _, r := range refs {
			f.release(r)
		}
	}
	if stream == 0 || queue == 0 {
		cleanup()
		return nil, true, fmt.Errorf("FSEvents stream allocation failed")
	}
	f.setQueue(stream, queue)
	if !f.start(stream) {
		cleanup()
		return nil, true, fmt.Errorf("FSEvents could not start; using timed reconciliation")
	}
	var once sync.Once
	return func() { once.Do(func() { f.stop(stream); cleanup() }) }, true, nil
}
