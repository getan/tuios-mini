package session

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/getan/tuios-mini/internal/review"
)

type reviewSyncParams struct {
	Session    string `json:"session"`
	Window     string `json:"window"`
	HumanNonce string `json:"human_nonce"`
}

// verbReviewSync answers review-sync: pull --rebase --autostash, then push.
func (d *Daemon) verbReviewSync(cs *connState, params json.RawMessage) (any, *verbError) {
	var p reviewSyncParams
	if verr := decodeParams(params, &p); verr != nil {
		return nil, verr
	}
	if verr := refuseForwardedPane(cs, "review-sync"); verr != nil {
		return nil, verr
	}
	if p.HumanNonce != "" && !d.humanNonceHeld(p.HumanNonce, cs) {
		if _, verr := d.reviewAuthorOf(cs, p.HumanNonce, "", "review-sync"); verr != nil {
			return nil, verr
		}
	}
	sess, target, repo, verr := d.reviewTarget(p.Session, p.Window)
	if verr != nil {
		return nil, verr
	}
	if _, verr := d.reviewAuthorOf(cs, p.HumanNonce, sess.ID, "review-sync"); verr != nil {
		return nil, verr
	}
	ctx, cancel := context.WithTimeout(d.ctx, 120*time.Second)
	defer cancel()
	res, err := review.Sync(ctx, repo.root)
	if err != nil {
		detail := "Nothing else was changed. A rebase conflict, if there is one, is left in the worktree to resolve."
		if errors.Is(err, context.DeadlineExceeded) {
			detail = "git took longer than 120s and was given up."
		}
		return nil, hintedVerbError(ErrVerbGitFailed, err.Error(), &VerbHint{Detail: detail})
	}
	return map[string]any{
		"type": "review_sync", "session": sess.Name(), "window": target.ID,
		"worktree": repo.root, "upstream": res.Upstream,
		"pull": truncateVerbText(res.Pull), "push": truncateVerbText(res.Push),
	}, nil
}

// truncateVerbText keeps a git command's output from turning one reply into a
// transcript; the status after the sync is the place to read the whole story.
func truncateVerbText(s string) string {
	const limit = 4096
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "…"
}
