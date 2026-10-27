package app

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/fsnotify/fsnotify"
)

// The Git tab used to ask git again every two seconds while it was visible.
// This watches the repository instead: an edit, a stage, a commit or a branch
// move marks the reading stale and the status is read once, off the render
// path. The timed read stays as a slow reconciliation for the cases file
// events cannot cover — a network filesystem, an inotify watch limit, or a
// changed file below a directory that was skipped.
const gitWatchDebounce = 250 * time.Millisecond

// gitWatchMaxDirs bounds the worktree walk. A repository larger than this
// falls back to the timed read for the directories beyond the cap rather than
// spending the first frame walking every directory under a monorepo. Linux
// also has a per-user inotify limit; a failed Add is the same fallback.
const gitWatchMaxDirs = 4096

// gitWatchSkip are the directories whose contents cannot change git's answer
// in a way worth a read, and which are the usual sources of event storms.
// Best-effort: a change that slips through only costs one extra status read.
var gitWatchSkip = map[string]bool{
	".git": true, "node_modules": true, "target": true, ".venv": true,
	"venv": true, "dist": true, "build": true, ".idea": true,
	".vscode": true, ".next": true, ".cache": true, "__pycache__": true,
}

// gitWatchMsg says the repository may have changed. Gen names the watcher the
// event came from, so an event from a repository the tab has left is dropped.
type gitWatchMsg struct{ Gen int }

// gitWatcher watches one repository: its worktree recursively, and the git
// metadata that stages, commits and branch moves rewrite without touching a
// working file.
type gitWatcher struct {
	gen    int
	root   string
	fw     *fsnotify.Watcher
	events chan struct{}
	done   chan struct{}
	once   sync.Once
}

func newGitWatcher(root string, gen int) (*gitWatcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &gitWatcher{
		gen: gen, root: root, fw: fw,
		events: make(chan struct{}, 1),
		done:   make(chan struct{}),
	}
	addGitWatchTree(fw, root)
	go w.run()
	return w, nil
}

// close stops the watches and unblocks a waiting command. It is safe to call
// more than once.
func (w *gitWatcher) close() {
	w.once.Do(func() {
		close(w.done)
		_ = w.fw.Close()
	})
}

func (w *gitWatcher) run() {
	raw := make(chan struct{}, 1)
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
	gitPath := filepath.Join(root, ".git")
	if fi, err := os.Stat(gitPath); err == nil && fi.Mode().IsRegular() {
		if data, err := os.ReadFile(gitPath); err == nil {
			if rest, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:"); ok {
				addGitMetaWatches(fw, strings.TrimSpace(rest))
			}
		}
	}
	added := 0
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if path != root {
			if d.Name() == ".git" {
				addGitMetaWatches(fw, path)
				return fs.SkipDir
			}
			if gitWatchSkip[d.Name()] {
				return fs.SkipDir
			}
		}
		if added >= gitWatchMaxDirs {
			return fs.SkipAll
		}
		_ = fw.Add(path)
		added++
		return nil
	})
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
	if !m.GitTabActive() && !m.filesSectionEnabled() {
		m.stopGitWatcher()
		return nil
	}
	origin := m.gitStatus.Origin
	root := m.gitStatus.Status.Root
	if root == "" || origin.Host != "" || origin.AttachedHost != "" || origin.CwdHost != "" {
		m.stopGitWatcher()
		return nil
	}
	if m.gitWatch != nil && m.gitWatch.root == root {
		return nil
	}
	m.stopGitWatcher()
	m.gitWatchGen++
	w, err := newGitWatcher(root, m.gitWatchGen)
	if err != nil {
		// The timed read remains; a filesystem without a watcher behaves as
		// it did before this file existed.
		return nil
	}
	m.gitWatch = w
	return m.waitGitWatch()
}

func (m *OS) stopGitWatcher() {
	if m.gitWatch != nil {
		m.gitWatch.close()
		m.gitWatch = nil
	}
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
