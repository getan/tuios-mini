package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGitWatcherReportsAWorktreeWrite(t *testing.T) {
	dir := t.TempDir()
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
