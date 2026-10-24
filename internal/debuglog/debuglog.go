// Package debuglog opens the diagnostic logs that TUIOS_DEBUG_INTERNAL=1
// turns on.
//
// The logs sit at fixed names under /tmp so a developer knows where to look,
// and they carry what a pane printed and, for the events log, every key
// pressed. A fixed name in a shared directory is one another local user can
// create first, as a link to a file of the victim's or as a world-readable
// file of their own, so Open refuses both and keeps the file private. The
// render trace, whose name under the temp directory is just as predictable,
// opens through it too.
//
// A fixed name in /tmp is also one name for the whole machine, which is wrong
// for a host that runs a client inside a tree of its own: two workspaces would
// append to one file, and a host that asked for no debug log would still find
// one filled by the client it embedded. Scope points the two logs at a
// directory the host owns and decides whether they are written at all, for as
// long as the caller holds it. Nothing is latched: outside a scope the
// environment answers exactly as it always did.
package debuglog

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// Path is the general debug log of a process that is not scoped.
	Path = "/tmp/tuios-debug.log"
	// EventsPath is the input event log of a process that is not scoped, which
	// records keystrokes.
	EventsPath = "/tmp/tuios-events.log"
)

// Open opens path for appending, creating it with mode 0600. The caller
// closes it.
func Open(path string) (*os.File, error) {
	return openPrivate(path)
}

// scope is the directory a host pointed the debug logs at, and whether it
// wanted them written. There is at most one: the logs are one process's.
var scope struct {
	mu  sync.RWMutex
	set bool
	dir string
	on  bool
}

// Scope points the diagnostic logs at dir for as long as the caller holds it,
// and decides whether they are written: on, or off whatever the environment
// says. It returns the function that puts the process's own logs back, which is
// safe to call more than once and from a defer.
//
// dir is the host's own directory. The two files are debug.log and events.log
// inside it, opened by whoever writes them; this makes nothing.
func Scope(dir string, on bool) (restore func(), err error) {
	if dir == "" {
		return nil, fmt.Errorf("debuglog: no directory; a scope needs the directory the host owns")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("debuglog: resolve %s: %w", dir, err)
	}

	scope.mu.Lock()
	if scope.set {
		open := scope.dir
		scope.mu.Unlock()
		return nil, fmt.Errorf("debuglog: %s is already in scope; end that scope before starting another", open)
	}
	scope.set, scope.dir, scope.on = true, abs, on
	scope.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			scope.mu.Lock()
			scope.set, scope.dir, scope.on = false, "", false
			scope.mu.Unlock()
		})
	}, nil
}

// DebugPath is the file the general debug log is written to now: the host's own
// while a client is scoped, /tmp/tuios-debug.log otherwise.
func DebugPath() string {
	if dir, ok := scopedDir(); ok {
		return filepath.Join(dir, "debug.log")
	}
	return Path
}

// EventsLogPath is the file the input event log is written to now. See
// DebugPath.
func EventsLogPath() string {
	if dir, ok := scopedDir(); ok {
		return filepath.Join(dir, "events.log")
	}
	return EventsPath
}

// scopedDir is the directory a host scoped this process to, when there is one.
func scopedDir() (string, bool) {
	scope.mu.RLock()
	defer scope.mu.RUnlock()
	return scope.dir, scope.set
}

// Enabled reports whether the diagnostic logs are being written: the scope's
// answer while there is one, else TUIOS_DEBUG_INTERNAL.
//
// Outside a scope the environment is read once, as it always was: the switch is
// set at startup, before any pane exists. Inside one it is read every time,
// because a scope is decided by the host per run and a value latched by one
// workspace would keep the next one — and the host after it — logging.
func Enabled() bool {
	scope.mu.RLock()
	if scope.set {
		on := scope.on
		scope.mu.RUnlock()
		return on
	}
	scope.mu.RUnlock()
	return enabledFromEnv()
}

// enabledFromEnv reports whether TUIOS_DEBUG_INTERNAL=1 is set, read once on
// first use.
var enabledFromEnv = sync.OnceValue(func() bool {
	return os.Getenv("TUIOS_DEBUG_INTERNAL") == "1"
})

// Internal is a logger that appends each line to DebugPath, with a timestamp,
// when Enabled. Otherwise it does nothing.
type Internal struct{}

// Printf appends one formatted line to DebugPath.
func (Internal) Printf(format string, v ...any) {
	if !Enabled() {
		return
	}
	f, err := Open(DebugPath())
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(f, "[%s] %s\n", time.Now().Format("15:04:05.000"), fmt.Sprintf(format, v...))
	_ = f.Close()
}
