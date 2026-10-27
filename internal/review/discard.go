package review

import (
	"context"
	"errors"
	"os"
	"path/filepath"
)

// Discard throws away a path's index and working changes. A tracked path goes
// back to HEAD; an untracked file is removed. It is deliberately all-or-nothing
// for the paths named and has no undo: the caller asks first.
func Discard(ctx context.Context, dir string, paths ...string) error {
	for _, p := range paths {
		if err := ValidPath(p); err != nil {
			return err
		}
		if _, err := git(ctx, dir, nil, "ls-files", "--error-unmatch", "--", p); err != nil {
			// Not in the index: an untracked file, which has no older copy to
			// restore. Removing it is the whole of discarding it.
			full := filepath.Join(dir, filepath.FromSlash(p))
			if err := os.Remove(full); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			continue
		}
		if _, err := git(ctx, dir, nil, "restore", "--source=HEAD", "--staged", "--worktree", "--", p); err != nil {
			return err
		}
	}
	return nil
}
