package review

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/getan/tuios-mini/internal/testutil"
)

func realIndexPath(t *testing.T, repo string) string {
	t.Helper()
	name := testutil.Git(t, repo, "rev-parse", "--git-path", "index")
	if !filepath.IsAbs(name) {
		name = filepath.Join(repo, name)
	}
	return name
}

// A real held lock makes contention deterministic. The oracle is Git itself;
// restore only our fixture after computing it so a cache-tree write is visible.
func TestIndexTreeReadsSnapshotWhileRealIndexLocked(t *testing.T) {
	for _, kind := range []string{"ordinary", "split", "v4", "intent", "linked", "sparse", "unborn-empty", "unborn-staged", "ambient-index", "relative"} {
		t.Run(kind, func(t *testing.T) {
			repo := testutil.GitRepo(t)
			if strings.HasPrefix(kind, "unborn-") {
				repo = filepath.Join(t.TempDir(), "unborn")
				if err := os.MkdirAll(repo, 0o700); err != nil {
					t.Fatal(err)
				}
				testutil.Git(t, repo, "init", "-q")
			}
			if kind == "linked" {
				linked := filepath.Join(t.TempDir(), "linked")
				testutil.Git(t, repo, "worktree", "add", "-q", "--detach", linked)
				repo = linked
			}
			name := "file.txt"
			if kind == "sparse" {
				write(t, repo, "kept/base.txt", "kept\n")
				write(t, repo, "excluded/base.txt", "excluded\n")
				testutil.Git(t, repo, "add", ".")
				testutil.Git(t, repo, "commit", "-q", "-m", "sparse fixture")
				testutil.Git(t, repo, "sparse-checkout", "init", "--cone", "--sparse-index")
				testutil.Git(t, repo, "sparse-checkout", "set", "kept")
				name = "kept/file.txt"
			}
			if kind != "unborn-empty" {
				write(t, repo, name, "staged content\n")
				testutil.Git(t, repo, "add", "--", name)
				write(t, repo, name, "unstaged content\n")
			}
			if kind == "intent" {
				write(t, repo, "intent.txt", "not staged\n")
				testutil.Git(t, repo, "add", "-N", "--", "intent.txt")
			}
			if kind == "split" {
				testutil.Git(t, repo, "config", "core.splitIndex", "true")
				testutil.Git(t, repo, "update-index", "--split-index")
				if testutil.Git(t, repo, "rev-parse", "--shared-index-path") == "" {
					t.Fatal("fixture is not split")
				}
			}
			if kind == "v4" {
				testutil.Git(t, repo, "update-index", "--index-version=4")
			}
			index := realIndexPath(t, repo)
			before, err := os.ReadFile(index)
			missing := os.IsNotExist(err)
			if err != nil && !missing {
				t.Fatal(err)
			}
			want := testutil.Git(t, repo, "write-tree")
			if missing {
				if err := os.Remove(index); err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(index, before, 0o600); err != nil {
				t.Fatal(err)
			}
			var mtime int64
			if !missing {
				info, err := os.Stat(index)
				if err != nil {
					t.Fatal(err)
				}
				mtime = info.ModTime().UnixNano()
			}
			lock := index + ".lock"
			marker := []byte("owned fixture writer is active\n")
			if err := os.WriteFile(lock, marker, 0o600); err != nil {
				t.Fatal(err)
			}
			if kind == "ambient-index" {
				foreign := testutil.GitRepo(t)
				t.Setenv("GIT_INDEX_FILE", realIndexPath(t, foreign))
				t.Setenv("GIT_DIR", filepath.Join(foreign, ".git"))
				t.Setenv("GIT_WORK_TREE", foreign)
			}
			readDir := repo
			if kind == "relative" {
				cwd, err := os.Getwd()
				if err != nil {
					t.Fatal(err)
				}
				readDir, err = filepath.Rel(cwd, repo)
				if err != nil {
					t.Fatal(err)
				}
			}
			got, err := IndexTree(ctx(t), readDir)
			if err != nil {
				t.Fatalf("read under held index.lock: %v", err)
			}
			if got != want {
				t.Fatalf("tree = %s, want Git's %s", got, want)
			}
			for _, mode := range []string{IndexStaged, IndexUnstaged} {
				opt, err := IndexOptions(ctx(t), readDir, mode)
				if err != nil {
					t.Fatalf("%s options: %v", mode, err)
				}
				if _, err := Build(ctx(t), opt); err != nil {
					t.Fatalf("%s preview: %v", mode, err)
				}
			}
			after, err := os.ReadFile(index)
			if missing {
				if !os.IsNotExist(err) {
					t.Fatalf("preview created a real index: %v", err)
				}
			} else {
				if err != nil || !bytes.Equal(before, after) {
					t.Fatal("preview modified the real index", err)
				}
				info, err := os.Stat(index)
				if err != nil || info.ModTime().UnixNano() != mtime {
					t.Fatal("preview refreshed the real index", err)
				}
			}
			kept, err := os.ReadFile(lock)
			if err != nil || !bytes.Equal(kept, marker) {
				t.Fatal("real lock was removed or changed", err)
			}
			if kind != "unborn-empty" {
				data, err := os.ReadFile(filepath.Join(repo, name))
				if err != nil || string(data) != "unstaged content\n" {
					t.Fatal("working content changed", err)
				}
			}
			assertNoReadIndexes(t, index)
			t.Logf("fixture=%s tree=%s real_index_sha256=%x lock_preserved=true", kind, got, sha256.Sum256(after))
		})
	}
}

func assertNoReadIndexes(t *testing.T, realIndex string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(realIndex), ".tuios-review-index-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("snapshot files leaked: %v (%v)", matches, err)
	}
}

func TestConcurrentIndexPreviewsNeverNeedTheRealIndexLock(t *testing.T) {
	repo := testutil.GitRepo(t)
	write(t, repo, "file.txt", "staged\n")
	testutil.Git(t, repo, "add", "--", "file.txt")
	want := testutil.Git(t, repo, "write-tree")
	index := realIndexPath(t, repo)
	before, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(index+".lock", []byte("fixture lock\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	ctx := ctx(t)
	// Four workers leave this developer machine usable while exercising
	// sixteen reads that share one real index and one live writer lock.
	for range 4 {
		wg.Go(func() {
			for range 4 {
				tree, err := IndexTree(ctx, repo)
				if err != nil {
					errs <- err
				} else if tree != want {
					errs <- fmt.Errorf("tree %s, want %s", tree, want)
				}
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	after, err := os.ReadFile(index)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("concurrent reads wrote the real index", err)
	}
	assertNoReadIndexes(t, index)
}

func TestIndexSnapshotErrorsDoNotRemoveARealLock(t *testing.T) {
	for _, kind := range []string{"conflict", "corrupt", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			repo := testutil.GitRepo(t)
			index := realIndexPath(t, repo)
			if kind == "conflict" {
				blob := testutil.Git(t, repo, "rev-parse", "HEAD:README")
				cmd := exec.Command("git", "update-index", "--index-info")
				cmd.Dir = repo
				cmd.Stdin = strings.NewReader(fmt.Sprintf("100644 %s 1\tconflict.txt\n100644 %s 2\tconflict.txt\n", blob, blob))
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("conflict fixture: %v\n%s", err, out)
				}
			}
			if kind == "corrupt" {
				if err := os.WriteFile(index, []byte("corrupt fixture index"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadFile(index)
			if err != nil {
				t.Fatal(err)
			}
			marker := []byte("do not remove another writer's lock\n")
			if err := os.WriteFile(index+".lock", marker, 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if kind == "cancelled" {
				cancel()
			}
			if _, err := IndexTree(ctx, repo); err == nil {
				t.Fatal("invalid/cancelled snapshot was reported as success")
			}
			after, err := os.ReadFile(index)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("error changed real index", err)
			}
			kept, err := os.ReadFile(index + ".lock")
			if err != nil || !bytes.Equal(marker, kept) {
				t.Fatal("error removed real lock", err)
			}
			assertNoReadIndexes(t, index)
		})
	}
}
