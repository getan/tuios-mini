package review

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscardRestoresTrackedAndRemovesUntracked(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}
	run("init", "-q")
	tracked := filepath.Join(dir, "tracked.txt")
	if err := os.WriteFile(tracked, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "tracked.txt")
	run("commit", "-q", "-m", "base")
	if err := os.WriteFile(tracked, []byte("after\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	untracked := filepath.Join(dir, "new.txt")
	if err := os.WriteFile(untracked, []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Discard(context.Background(), dir, "tracked.txt", "new.txt"); err != nil {
		t.Fatalf("Discard: %v", err)
	}
	got, err := os.ReadFile(tracked)
	if err != nil || string(got) != "before\n" {
		t.Fatalf("tracked = %q, %v", got, err)
	}
	if _, err := os.Stat(untracked); !os.IsNotExist(err) {
		t.Fatalf("untracked still exists: %v", err)
	}
}
