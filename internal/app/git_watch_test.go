package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGitWatcherReportsAWorktreeWrite(t *testing.T) {
	dir := t.TempDir()
	// The Linux watcher asks git for the directory set; an empty directory
	// outside a repository yields no watch at all. Make this a real worktree
	// so the test exercises the same path a pane does.
	if err := exec.Command("git", "init", "-q", dir).Run(); err != nil {
		t.Skipf("git init unavailable: %v", err)
	}
	w, err := newGitWatcher(dir, 1)
	if err != nil {
		t.Skipf("no filesystem watcher here: %v", err)
	}
	defer w.close()

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case <-w.events:
	case <-w.done:
		t.Fatal("the watcher closed before the write was seen")
	case <-time.After(5 * time.Second):
		t.Fatal("a worktree write produced no event")
	}
}

func TestGitWatcherCloseUnblocksTheWait(t *testing.T) {
	dir := t.TempDir()
	w, err := newGitWatcher(dir, 1)
	if err != nil {
		t.Skipf("no filesystem watcher here: %v", err)
	}
	w.close()
	select {
	case <-w.done:
	case <-time.After(time.Second):
		t.Fatal("close did not signal the waiter")
	}
}

// gitWatchTestOS is the smallest OS that can reach syncGitWatcher from Update:
// a focused local pane in the Git tab, with a status reading that matches the
// origin GitStatusSyncCmd computes, so the post-handler reconciliation does
// not immediately stop what the test just started.
func gitWatchTestOS(t *testing.T, root string) *OS {
	t.Helper()
	m := keystrokeOS(t, 1, 100, 30)
	m.projectTab = 1
	w := m.GetFocusedWindow()
	if w == nil {
		t.Fatal("the test OS has no focused window")
	}
	w.Cwd = root
	origin := gitStatusOrigin{
		Session: m.SessionName, Window: w.ID, Dir: root,
		Host: w.Host, AttachedHost: m.AttachedHost, CwdHost: w.CwdHost,
	}
	m.gitStatus = gitStatusView{gen: 1, asked: origin, Origin: origin, at: time.Now()}
	m.gitStatus.Status.Root = root
	t.Cleanup(m.stopGitWatcher)
	return m
}

// TestGitWatcherCreationDoesNotBlockUpdate is the regression for the freeze:
// a repository with thousands of directories must not be walked on the event
// loop. Update only records the pending creation and returns a command.
func TestGitWatcherCreationDoesNotBlockUpdate(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 3000; i++ {
		if err := os.Mkdir(filepath.Join(root, fmt.Sprintf("d%04d", i)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	m := gitWatchTestOS(t, root)
	msg := GitStatusMsg{Gen: 1, Origin: m.gitStatus.asked}
	msg.Status.Root = root

	started := time.Now()
	m.Update(msg)
	elapsed := time.Since(started)

	if m.gitWatch != nil {
		t.Fatal("Update created the git watcher synchronously")
	}
	if m.gitWatchPendingGen == 0 || m.gitWatchPendingRoot != root {
		t.Fatalf("Update did not schedule the watcher: gen=%d root=%q",
			m.gitWatchPendingGen, m.gitWatchPendingRoot)
	}
	if elapsed > slowUpdateThreshold {
		t.Fatalf("Update held the event loop for %s with a %d-directory repo", elapsed, 3000)
	}
	for _, entry := range m.LogMessages {
		if strings.Contains(entry.Message, "SLOW_UPDATE") {
			t.Fatalf("the asynchronous update still logged a slow frame: %s", entry.Message)
		}
	}
}

// TestGitWatcherReadyInstallsWatcher covers the other half of the handshake:
// the command builds the watcher, and the ready message installs it without
// another synchronous walk.
func TestGitWatcherReadyInstallsWatcher(t *testing.T) {
	root := t.TempDir()
	m := gitWatchTestOS(t, root)
	cmd := m.syncGitWatcher()
	if cmd == nil {
		t.Fatal("syncGitWatcher returned no creation command")
	}
	msg := cmd()
	ready, ok := msg.(gitWatcherReadyMsg)
	if !ok {
		t.Fatalf("creation command returned %T", msg)
	}
	if ready.Err != nil {
		t.Skipf("no filesystem watcher here: %v", ready.Err)
	}
	if ready.W == nil {
		t.Skip("this filesystem does not get a watcher")
	}
	m.Update(ready)
	if m.gitWatch == nil {
		t.Fatal("the ready message did not install the watcher")
	}
	if m.gitWatch.root != root || m.gitWatch.gen != ready.Gen {
		t.Fatalf("installed watcher root=%q gen=%d, want %q/%d",
			m.gitWatch.root, m.gitWatch.gen, root, ready.Gen)
	}
	if m.gitWatchPendingGen != 0 {
		t.Fatalf("ready watcher left a pending generation %d", m.gitWatchPendingGen)
	}
}

// TestStaleGitWatcherIsClosed covers the tab moving while a slow creation is
// in flight: the generation no longer matches, so the ready watcher is closed
// instead of installed.
func TestStaleGitWatcherIsClosed(t *testing.T) {
	root := t.TempDir()
	m := gitWatchTestOS(t, root)
	cmd := m.syncGitWatcher()
	if cmd == nil {
		t.Fatal("syncGitWatcher returned no creation command")
	}
	ready, ok := cmd().(gitWatcherReadyMsg)
	if !ok {
		t.Fatalf("creation command returned %T", cmd)
	}
	if ready.Err != nil {
		t.Skipf("no filesystem watcher here: %v", ready.Err)
	}
	if ready.W == nil {
		t.Skip("this filesystem does not get a watcher")
	}
	m.stopGitWatcher()
	m.Update(ready)
	if m.gitWatch != nil {
		t.Fatal("a watcher from a superseded generation was installed")
	}
	select {
	case <-ready.W.done:
	default:
		t.Fatal("the stale watcher was not closed")
	}
}

// TestGitWatchDirsFollowsGitignore pins the move away from WalkDir: git's own
// view of the repository excludes ignored trees, so .scratch and .build never
// become watches.
func TestGitWatchDirsFollowsGitignore(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".gitignore", ".scratch/\n.build/\n")
	write("src/a/file.go", "package a\n")
	write("docs/readme.md", "docs\n")
	write(".scratch/deep/cache", "cache\n")
	write(".build/obj/out", "out\n")
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init")
	git("add", ".gitignore", "src/a/file.go")

	dirs, err := gitWatchDirs(root)
	if err != nil {
		t.Fatalf("gitWatchDirs: %v", err)
	}
	got := make(map[string]bool, len(dirs))
	for _, dir := range dirs {
		got[dir] = true
	}
	for _, want := range []string{
		root,
		filepath.Join(root, "src"),
		filepath.Join(root, "src", "a"),
		filepath.Join(root, "docs"),
	} {
		if !got[want] {
			t.Errorf("gitWatchDirs missing %s: %v", want, dirs)
		}
	}
	for _, skip := range []string{".scratch", ".build", ".git"} {
		for dir := range got {
			rel, err := filepath.Rel(root, dir)
			if err != nil {
				continue
			}
			if rel == skip || strings.HasPrefix(rel, skip+string(filepath.Separator)) {
				t.Errorf("ignored tree %s contributed a watch: %s", skip, dir)
			}
		}
	}
}

// TestGitWatcherSkipsNetworkFilesystems pins the fallback: on a filesystem
// whose events are unreliable, no watcher is built at all.
func TestGitWatcherSkipsNetworkFilesystems(t *testing.T) {
	w, err := newGitWatcherWith(t.TempDir(), 1, func(string) bool { return true })
	if err != nil {
		t.Fatalf("newGitWatcherWith: %v", err)
	}
	if w != nil {
		w.close()
		t.Fatal("a network filesystem got a watcher")
	}
}
