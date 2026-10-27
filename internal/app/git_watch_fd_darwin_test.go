package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// Reproduce on a low-limit helper, never change the test runner's descriptor
// limit. A single directory with thousands of files used to exhaust kqueue
// during Add(root), even though the 4096-directory cap had not been reached.
func TestGitWatcherDescriptorBudget(t *testing.T) {
	if os.Getenv("TUIOS_TEST_GIT_FD_CHILD") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestGitWatcherDescriptorBudget$", "-test.count=1")
		cmd.Env = append(os.Environ(), "TUIOS_TEST_GIT_FD_CHILD=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("descriptor-budget helper: %v\n%s", err, out)
		}
		return
	}
	root := t.TempDir()
	for i := 0; i < 1500; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprint(i)), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var limit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &limit); err != nil {
		t.Fatal(err)
	}
	if limit.Cur > 128 {
		limit.Cur = 128
	}
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &limit); err != nil {
		t.Fatal(err)
	}
	for cycle := 0; cycle < 3; cycle++ {
		w, err := newGitWatcher(root, cycle)
		if err != nil {
			t.Fatal(err)
		}
		if w.fw != nil {
			t.Fatal("macOS must not recursively open file descriptors with fsnotify/kqueue")
		}
		if err := os.WriteFile(filepath.Join(root, "0"), []byte(fmt.Sprint(cycle)), 0o600); err != nil {
			t.Fatal(err)
		}
		select {
		case <-w.events:
		case <-time.After(5 * time.Second):
			w.close()
			t.Fatal("existing-file edit produced no FSEvent")
		}
		fds, err := os.ReadDir("/dev/fd")
		if err != nil {
			t.Fatal(err)
		}
		if len(fds) > 64 {
			t.Fatalf("watcher used %d descriptors for one root", len(fds))
		}
		w.close()
	}
	gitFSCallbacks.Lock()
	n := len(gitFSCallbacks.callbacks)
	gitFSCallbacks.Unlock()
	if n != 0 {
		t.Fatalf("%d FSEvents callbacks leaked after close", n)
	}
}
