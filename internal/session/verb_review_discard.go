package session

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"time"

	"github.com/getan/tuios-mini/internal/review"
)

type reviewDiscardParams struct {
	Session    string   `json:"session"`
	Window     string   `json:"window"`
	Paths      []string `json:"paths"`
	HumanNonce string   `json:"human_nonce"`
}

// verbReviewDiscard answers review-discard: it restores the named paths to
// HEAD and removes untracked ones. There is no undo, so the caller is expected
// to have asked the person first.
func (d *Daemon) verbReviewDiscard(cs *connState, params json.RawMessage) (any, *verbError) {
	var p reviewDiscardParams
	if verr := decodeParams(params, &p); verr != nil {
		return nil, verr
	}
	if verr := refuseForwardedPane(cs, "review-discard"); verr != nil {
		return nil, verr
	}
	if len(p.Paths) == 0 {
		return nil, invalidParam("paths", "paths is required: the files to discard")
	}
	if len(p.Paths) > 200 {
		return nil, invalidParam("paths", "paths is at most 200 entries")
	}
	for i, path := range p.Paths {
		if err := review.ValidPath(path); err != nil {
			return nil, invalidParam("paths", err.Error())
		}
		p.Paths[i] = filepath.ToSlash(filepath.Clean(path))
	}
	if p.HumanNonce != "" && !d.humanNonceHeld(p.HumanNonce, cs) {
		if _, verr := d.reviewAuthorOf(cs, p.HumanNonce, "", "review-discard"); verr != nil {
			return nil, verr
		}
	}
	sess, target, repo, verr := d.reviewTarget(p.Session, p.Window)
	if verr != nil {
		return nil, verr
	}
	if _, verr := d.reviewAuthorOf(cs, p.HumanNonce, sess.ID, "review-discard"); verr != nil {
		return nil, verr
	}
	ctx, cancel := context.WithTimeout(d.ctx, 30*time.Second)
	defer cancel()
	if err := review.Discard(ctx, repo.root, p.Paths...); err != nil {
		detail := "Nothing else was changed. A path that is not in HEAD cannot be restored."
		if errors.Is(err, context.DeadlineExceeded) {
			detail = "git took longer than 30s and was given up."
		}
		return nil, hintedVerbError(ErrVerbGitFailed, err.Error(), &VerbHint{Detail: detail})
	}
	return map[string]any{
		"type": "review_discard", "session": sess.Name(), "window": target.ID,
		"worktree": repo.root, "paths": p.Paths,
	}, nil
}
