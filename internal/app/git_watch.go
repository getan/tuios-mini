package app

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/fsnotify/fsnotify"
)

// gitWatchListTimeout bounds the one `git ls-files` a watcher creation runs.
// A repository on a slow or enormous tree must not spend that time; the timed
// status read remains the fallback.
const gitWatchListTimeout = 3 * time.Second

// The Git tab used to ask git again every two seconds while it was visible.
// This watches the repository instead: an edit, a stage, a commit or a branch
// move marks the reading stale and the status is read once, off the render
// path. The timed read stays as a slow reconciliation for the cases file
// events cannot cover — a network filesystem, an inotify watch limit, or a
// changed file below a directory that was skipped.
const gitWatchDebounce = 250 * time.Millisecond

// gitWatchMaxDirs bounds the number of watches. The directory set comes from
// git, so an ignored tree such as .scratch never contributes; the cap is a
// guard on inotify resources rather than on filesystem traversal. Linux also
// has a per-user inotify limit; a failed Add is the same fallback.
const gitWatchMaxDirs = 4096

// gitWatchSkip is the fallback walk's list and a guard for tracked generated
// trees. git ls-files already honors .gitignore, so .scratch and .build
// normally never reach here. Best-effort: a change that slips through only
// costs one extra status read.
var gitWatchSkip = map[string]bool{
	".git": true, ".scratch": true, ".build": true,
	"node_modules": true, "target": true, ".venv": true,
	"venv": true, "dist": true, "build": true, ".idea": true,
	".vscode": true, ".next": true, ".cache": true, "__pycache__": true,
}

// gitWatchMsg says the repository may have changed. Gen names the watcher the
// event came from, so an event from a repository the tab has left is dropped.
type gitWatchMsg struct{ Gen int }

// gitWatcherReadyMsg carries a watcher built off the update goroutine. Gen and
// Root are checked again when it lands: the tab may have moved on while the
// walk was running, in which case the watcher is closed and dropped.
type gitWatcherReadyMsg struct {
	Gen  int
	Root string
	W    *gitWatcher
	Err  error
}

// gitWatcher watches one repository: its worktree recursively, and the git
// metadata that stages, commits and branch moves rewrite without touching a
// working file.
type gitWatcher struct {
	gen        int
	root       string
	fw         *fsnotify.Watcher
	stopNative func()
	raw        chan struct{}
	events     chan struct{}
	done       chan struct{}
	once       sync.Once
}

func newGitWatcher(root string, gen int) (*gitWatcher, error) {
	return newGitWatcherWith(root, gen, gitWatchNetworkFS)
}

// newGitWatcherWith is newGitWatcher with the network-filesystem probe passed
// in, so a test can exercise the fallback without a real network mount.
func newGitWatcherWith(root string, gen int, networkFS func(string) bool) (*gitWatcher, error) {
	if networkFS != nil && networkFS(root) {
		// Every inotify add on NFS/CIFS/FUSE is a round trip, and the events
		// are unreliable; the timed read is the better fallback.
		return nil, nil
	}
	w := &gitWatcher{
		gen: gen, root: root, raw: make(chan struct{}, 1),
		events: make(chan struct{}, 1),
		done:   make(chan struct{}),
	}
	stop, native, err := nativeGitWatch(root, func() {
		select {
		case w.raw <- struct{}{}:
		default:
		}
	})
	if err != nil {
		return nil, err // never fall back to per-file kqueue on macOS
	}
	if native {
		w.stopNative = stop
		go w.run()
		return w, nil
	}
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w.fw = fw
	addGitWatchTree(fw, root)
	go w.run()
	return w, nil
}

// close stops the watches and unblocks a waiting command. It is safe to call
// more than once.
func (w *gitWatcher) close() {
	w.once.Do(func() {
		close(w.done)
		if w.fw != nil {
			_ = w.fw.Close()
		}
		if w.stopNative != nil {
			w.stopNative()
		}
	})
}

func (w *gitWatcher) run() {
	raw := w.raw
	if w.fw != nil {
		go func() {
			for {
				select {
				case <-w.done:
					return
				case ev, ok := <-w.fw.Events:
					if !ok {
						return
					}
					if ev.Op == fsnotify.Chmod {
						continue
					}
					select {
					case raw <- struct{}{}:
					default:
					}
				case _, ok := <-w.fw.Errors:
					if !ok {
						return
					}
					// An overflow may have dropped events; one read puts the tab
					// back in agreement even when none of the lost events arrive.
					select {
					case raw <- struct{}{}:
					default:
					}
				}
			}
		}()
	}
	for {
		select {
		case <-w.done:
			return
		case <-raw:
			if !w.debounce(raw) {
				return
			}
			select {
			case w.events <- struct{}{}:
			default:
			}
		}
	}
}

// debounce waits for a quiet window, restarting it for every event that
// arrives, so a save burst or a checkout that rewrites many files costs one
// status read. It returns false if the watcher closed while waiting.
func (w *gitWatcher) debounce(raw <-chan struct{}) bool {
	t := time.NewTimer(gitWatchDebounce)
	defer t.Stop()
	for {
		select {
		case <-w.done:
			return false
		case <-raw:
			if !t.Stop() {
				select {
				case <-t.C:
				default:
				}
			}
			t.Reset(gitWatchDebounce)
		case <-t.C:
			return true
		}
	}
}

// addGitWatchTree watches the worktree, the gitdir's HEAD/index and refs, and
// nothing under a skipped build directory. A `.git` file (a linked worktree or
// a submodule) names its gitdir instead of being a directory.
func addGitWatchTree(fw *fsnotify.Watcher, root string) {
	addGitMetaFromWorktree(fw, root)
	dirs, err := gitWatchDirs(root)
	if err != nil {
		// git could not list the tree in time (or is absent). Do not fall back
		// to a recursive WalkDir: a home or scratch tree with six figures of
		// files is exactly what this guard exists for. The timed read remains.
		return
	}
	addGitWatchDirs(fw, dirs)
}

// addGitMetaFromWorktree adds the gitdir and refs watches and skips `.git` in
// the worktree walk. A `.git` file names a gitdir; a directory is the gitdir.
func addGitMetaFromWorktree(fw *fsnotify.Watcher, root string) {
	gitPath := filepath.Join(root, ".git")
	fi, err := os.Stat(gitPath)
	if err != nil {
		return
	}
	if fi.Mode().IsRegular() {
		if data, err := os.ReadFile(gitPath); err == nil {
			if rest, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:"); ok {
				addGitMetaWatches(fw, strings.TrimSpace(rest))
			}
		}
		return
	}
	if fi.IsDir() {
		addGitMetaWatches(fw, gitPath)
	}
}

// gitWatchDirs asks git for the paths that can change its answer instead of
// walking the filesystem. `--cached` covers tracked files, `--others
// --exclude-standard` covers untracked non-ignored files, and `--directory`
// collapses an untracked tree to its top. Each file contributes its parent and
// every ancestor so a change anywhere in a tracked tree is seen.
func gitWatchDirs(root string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitWatchListTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--directory")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	dirs := map[string]bool{root: true}
	for _, entry := range strings.Split(string(out), "\x00") {
		if entry == "" {
			continue
		}
		isDir := strings.HasSuffix(entry, "/")
		rel := strings.TrimSuffix(entry, "/")
		if rel == "" {
			continue
		}
		rel = filepath.Clean(filepath.FromSlash(rel))
		if rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		start := rel
		if !isDir {
			start = filepath.Dir(rel)
		}
		for dir := start; dir != "." && dir != ""; dir = filepath.Dir(dir) {
			if gitWatchSkippedPath(dir) {
				continue
			}
			dirs[filepath.Join(root, dir)] = true
		}
	}
	paths := make([]string, 0, len(dirs))
	for dir := range dirs {
		paths = append(paths, dir)
	}
	// Parents before children, so a capped watch set still covers the shallow
	// trees that see the most events. Lexical order breaks ties deterministically.
	sort.Slice(paths, func(i, j int) bool {
		di := strings.Count(paths[i], string(filepath.Separator))
		dj := strings.Count(paths[j], string(filepath.Separator))
		if di != dj {
			return di < dj
		}
		return paths[i] < paths[j]
	})
	return paths, nil
}

// gitWatchSkippedPath reports whether any component of rel is in gitWatchSkip.
// A descendant of a skipped directory is skipped too, while its ancestors are
// still admitted.
func gitWatchSkippedPath(rel string) bool {
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if part != "" && gitWatchSkip[part] {
			return true
		}
	}
	return false
}

// addGitWatchDirs adds the directory set from gitWatchDirs, bounded by the
// existing watch cap. Add failures (the inotify limit) leave the timed read in
// place for the directories not watched.
func addGitWatchDirs(fw *fsnotify.Watcher, dirs []string) {
	added := 0
	for _, dir := range dirs {
		if added >= gitWatchMaxDirs {
			return
		}
		_ = fw.Add(dir)
		added++
	}
}

// addGitMetaWatches watches the paths a stage, commit or branch move rewrites.
// fsnotify has no recursive add, so refs is walked; it is small.
func addGitMetaWatches(fw *fsnotify.Watcher, gitDir string) {
	_ = fw.Add(gitDir)
	_ = filepath.WalkDir(filepath.Join(gitDir, "refs"), func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = fw.Add(path)
		}
		return nil
	})
}

// syncGitWatcher starts, keeps or stops the watcher for the repository the
// project section is showing. It returns the command that waits for the next
// event, or nil when there is nothing to watch.
func (m *OS) syncGitWatcher() tea.Cmd {
	root, ok := m.gitWatchTarget()
	if !ok {
		m.stopGitWatcher()
		return nil
	}
	if m.gitWatch != nil && m.gitWatch.root == root {
		return nil
	}
	if m.gitWatchPendingGen != 0 && m.gitWatchPendingRoot == root {
		// A creation for this root is already off the update goroutine.
		return nil
	}
	m.stopGitWatcher()
	gen := m.gitWatchGen
	m.gitWatchPendingGen, m.gitWatchPendingRoot = gen, root
	// newGitWatcher walks the worktree and registers one watch per directory.
	// On a network filesystem that is seconds of stat latency, so it must run
	// in the command's goroutine, never in Update.
	return func() tea.Msg {
		w, err := newGitWatcher(root, gen)
		return gitWatcherReadyMsg{Gen: gen, Root: root, W: w, Err: err}
	}
}

// gitWatchTarget is the repository the project section should be watching, or
// false when it should not watch one. It is the same rule syncGitWatcher used
// to apply inline; the ready handler asks it again because the tab can move
// while a creation is in flight.
func (m *OS) gitWatchTarget() (string, bool) {
	if !m.GitTabActive() && !m.filesSectionEnabled() {
		return "", false
	}
	origin := m.gitStatus.Origin
	root := m.gitStatus.Status.Root
	if root == "" || origin.Host != "" || origin.AttachedHost != "" || origin.CwdHost != "" {
		return "", false
	}
	return root, true
}

func (m *OS) stopGitWatcher() {
	if m.gitWatch != nil {
		m.gitWatch.close()
		m.gitWatch = nil
	}
	// Invalidate a creation still in flight as well: its ready message carries
	// the old generation and is closed on arrival.
	m.gitWatchGen++
	m.gitWatchPendingGen = 0
	m.gitWatchPendingRoot = ""
}

// waitGitWatch blocks until the watcher has an event or is closed. One waiter
// is armed at a time; the update loop re-arms it after each read.
func (m *OS) waitGitWatch() tea.Cmd {
	w := m.gitWatch
	if w == nil {
		return nil
	}
	return func() tea.Msg {
		select {
		case <-w.events:
			return gitWatchMsg{Gen: w.gen}
		case <-w.done:
			return nil
		}
	}
}
