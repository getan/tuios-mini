package review

import (
	"context"
	"errors"
	"strings"
)

// ErrNothingToCommit reports a commit with nothing in the index.
var ErrNothingToCommit = errors.New("nothing is staged to commit")

// CommitStaged commits the index at dir with message and returns the new
// commit. Only what is staged is committed; the working tree is untouched. A
// hook or a signing failure leaves the index as it was, because git commit
// does not change it when the commit fails.
func CommitStaged(ctx context.Context, dir, message string) (string, error) {
	if strings.TrimSpace(message) == "" {
		return "", errors.New("the commit message is empty")
	}
	staged, err := git(ctx, dir, nil, "diff", "--cached", "--name-only")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(staged) == "" {
		return "", ErrNothingToCommit
	}
	if _, err := git(ctx, dir, nil, "commit", "-q", "-m", message); err != nil {
		return "", err
	}
	out, err := git(ctx, dir, nil, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}
