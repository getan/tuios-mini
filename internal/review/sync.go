package review

import (
	"context"
	"errors"
	"strings"
)

// SyncResult is what Sync did: the upstream it used and git's own words for
// the pull and the push.
type SyncResult struct {
	Upstream string
	Pull     string
	Push     string
}

// Sync brings the branch up to date with its upstream and pushes it: pull
// --rebase --autostash, then push. It refuses a branch with no upstream rather
// than guessing one, and a conflict leaves the rebase in progress for the
// person to resolve; the error then names git's own message.
func Sync(ctx context.Context, dir string) (SyncResult, error) {
	upstream, err := git(ctx, dir, nil, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if err != nil {
		return SyncResult{}, errors.New("this branch has no upstream; push it once with git push -u")
	}
	upstream = strings.TrimSpace(upstream)
	pull, err := git(ctx, dir, nil, "pull", "--rebase", "--autostash")
	if err != nil {
		return SyncResult{Upstream: upstream}, err
	}
	push, err := git(ctx, dir, nil, "push")
	if err != nil {
		return SyncResult{Upstream: upstream, Pull: pull}, err
	}
	return SyncResult{Upstream: upstream, Pull: pull, Push: push}, nil
}
