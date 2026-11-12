package review

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPullFFOnlyLocalRemote(t *testing.T) {
	base := t.TempDir()
	remote, one, two := filepath.Join(base, "remote.git"), filepath.Join(base, "one"), filepath.Join(base, "two")
	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s (%v)", args, out, err)
		}
		return strings.TrimSpace(string(out))
	}
	run(base, "init", "--bare", "-q", remote)
	run(base, "clone", "-q", remote, one)
	for _, dir := range []string{one} {
		run(dir, "config", "user.name", "Test")
		run(dir, "config", "user.email", "test@example.invalid")
	}
	write := func(dir, value string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "tracked"), []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
		run(dir, "add", "tracked")
		run(dir, "commit", "-qm", value)
	}
	write(one, "base")
	run(one, "push", "-qu", "origin", "HEAD")
	run(base, "clone", "-q", remote, two)
	run(two, "config", "user.name", "Test")
	run(two, "config", "user.email", "test@example.invalid")
	write(one, "remote-change")
	run(one, "push", "-q")
	upstream, _, err := PullFFOnly(context.Background(), two)
	if err != nil || upstream == "" || run(one, "rev-parse", "HEAD") != run(two, "rev-parse", "HEAD") {
		t.Fatalf("fast-forward failed: %q %v", upstream, err)
	}
	write(one, "second-remote")
	run(one, "push", "-q")
	write(two, "local-divergence")
	before := run(two, "rev-parse", "HEAD")
	if _, _, err := PullFFOnly(context.Background(), two); err == nil || run(two, "rev-parse", "HEAD") != before {
		t.Fatal("diverged branch was changed")
	}
	if run(one, "rev-parse", "HEAD") == before {
		t.Fatal("fixture did not diverge")
	}
}
