package gitstatus

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/getan/tuios-mini/internal/testutil"
)

func TestBackgroundStatusDoesNotRefreshTheSharedIndex(t *testing.T) {
	repo := testutil.GitRepo(t)
	index := filepath.Join(repo, ".git", "index")
	before, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(10 * time.Second)
	if err := os.Chtimes(filepath.Join(repo, "README"), future, future); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_OPTIONAL_LOCKS", "1")
	st := Read(context.Background(), repo)
	if st.Err != "" {
		t.Fatal(st.Err)
	}
	after, err := os.ReadFile(index)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("background status refreshed the shared index", err)
	}
	// Prove the fixture exposes a real optional refresh on this filesystem.
	testutil.Git(t, repo, "status", "--porcelain")
	control, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(before, control) {
		t.Skip("Git did not expose a stat-cache refresh on this filesystem")
	}
}
