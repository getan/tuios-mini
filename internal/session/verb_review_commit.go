package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/getan/tuios-mini/internal/review"
)

// reviewCommitParams is what review-commit takes: the pane whose worktree is
// committed, the message, and the person's nonce when a client sends it.
type reviewCommitParams struct {
	Session    string `json:"session"`
	Window     string `json:"window"`
	Message    string `json:"message"`
	HumanNonce string `json:"human_nonce"`
}

// verbReviewCommit answers review-commit: it commits what is staged in the
// pane's worktree. It never stages anything itself, so the staged/working
// split the Git tab draws is the split git sees.
func (d *Daemon) verbReviewCommit(cs *connState, params json.RawMessage) (any, *verbError) {
	var p reviewCommitParams
	if verr := decodeParams(params, &p); verr != nil {
		return nil, verr
	}
	if verr := refuseForwardedPane(cs, "review-commit"); verr != nil {
		return nil, verr
	}
	message := strings.TrimSpace(p.Message)
	if message == "" {
		return nil, invalidParam("message", "message is required: the commit message")
	}
	if len(message) > 64<<10 {
		return nil, invalidParam("message", "message is at most 64 KiB")
	}
	if p.HumanNonce != "" && !d.humanNonceHeld(p.HumanNonce, cs) {
		if _, verr := d.reviewAuthorOf(cs, p.HumanNonce, "", "review-commit"); verr != nil {
			return nil, verr
		}
	}
	sess, target, repo, verr := d.reviewTarget(p.Session, p.Window)
	if verr != nil {
		return nil, verr
	}
	if _, verr := d.reviewAuthorOf(cs, p.HumanNonce, sess.ID, "review-commit"); verr != nil {
		return nil, verr
	}

	ctx, cancel := context.WithTimeout(d.ctx, reviewTimeout)
	defer cancel()
	commit, err := review.CommitStaged(ctx, repo.root, message)
	if errors.Is(err, review.ErrNothingToCommit) {
		return nil, hintedVerbError(ErrVerbGitFailed, "nothing is staged to commit", &VerbHint{
			Detail: "Stage a change first. review-commit commits the index and never stages the working tree.",
		})
	}
	if err != nil {
		detail := "The index was not changed by a failed commit. Read the diff again with review-diff."
		if errors.Is(err, context.DeadlineExceeded) {
			detail = "git took longer than " + reviewTimeout.String() + ", so the call was given up. The index was not changed."
		}
		return nil, hintedVerbError(ErrVerbGitFailed, err.Error(), &VerbHint{Detail: detail})
	}
	return map[string]any{
		"type":     "review_commit",
		"session":  sess.Name(),
		"window":   target.ID,
		"worktree": repo.root,
		"commit":   commit,
	}, nil
}
