package review

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommitStaged(t *testing.T) {
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
	// CommitStaged starts git itself, outside the helper's cmd.Env. Give the
	// fixture a local identity so it works on CI with no global git config.
	run("config", "user.name", "Test")
	run("config", "user.email", "test@example.com")
	run("config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "a.txt")

	commit, err := CommitStaged(context.Background(), dir, "first")
	if err != nil {
		t.Fatalf("CommitStaged: %v", err)
	}
	if len(commit) != 40 {
		t.Fatalf("commit = %q", commit)
	}
	if got := strings.TrimSpace(run("log", "-1", "--format=%s")); got != "first" {
		t.Fatalf("subject = %q", got)
	}

	if _, err := CommitStaged(context.Background(), dir, "second"); !errors.Is(err, ErrNothingToCommit) {
		t.Fatalf("empty index error = %v, want ErrNothingToCommit", err)
	}
	if _, err := CommitStaged(context.Background(), dir, "  "); err == nil {
		t.Fatal("an empty message was accepted")
	}
}
