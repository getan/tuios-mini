package session

import (
	"context"
	"encoding/json"
	"path/filepath"
	"time"

	"github.com/getan/tuios-mini/internal/review"
)

// verbReviewPull requires the same local-human authority as other Git writes.
func (d *Daemon) verbReviewPull(cs *connState, params json.RawMessage) (any, *verbError) {
	var p struct {
		Session    string `json:"session"`
		Window     string `json:"window"`
		HumanNonce string `json:"human_nonce"`
		Worktree   string `json:"worktree"`
	}
	if verr := decodeParams(params, &p); verr != nil {
		return nil, verr
	}
	if verr := refuseForwardedPane(cs, "review-pull"); verr != nil {
		return nil, verr
	}
	if p.HumanNonce != "" && !d.humanNonceHeld(p.HumanNonce, cs) {
		if _, verr := d.reviewAuthorOf(cs, p.HumanNonce, "", "review-pull"); verr != nil {
			return nil, verr
		}
	}
	sess, target, repo, verr := d.reviewTarget(p.Session, p.Window)
	if verr != nil {
		return nil, verr
	}
	if _, verr := d.reviewAuthorOf(cs, p.HumanNonce, sess.ID, "review-pull"); verr != nil {
		return nil, verr
	}
	if p.Worktree == "" || filepath.Clean(p.Worktree) != filepath.Clean(repo.root) {
		return nil, hintedVerbError(ErrVerbInvalidParams, "repository changed before pull; nothing was sent", &VerbHint{Detail: "Refresh the Git tab and choose Pull again."})
	}
	ctx, cancel := context.WithTimeout(d.ctx, 120*time.Second)
	defer cancel()
	upstream, output, err := review.PullFFOnly(ctx, repo.root)
	if err != nil {
		return nil, hintedVerbError(ErrVerbGitFailed, err.Error(), &VerbHint{Detail: "Inspect repository and remote status before retrying; no automatic retry, rebase, stash or push is performed."})
	}
	return map[string]any{"type": "review_pull", "session": sess.Name(), "window": target.ID, "worktree": repo.root, "upstream": upstream, "pull": truncateVerbText(output)}, nil
}
