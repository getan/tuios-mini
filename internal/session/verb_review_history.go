package session

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/getan/tuios-mini/internal/review"
)

type reviewHistoryParams struct {
	Session string `json:"session"`
	Window  string `json:"window"`
	Kind    string `json:"kind"`
	Path    string `json:"path"`
	Limit   int    `json:"limit"`
}

// verbReviewHistory answers the read-only Git drawers: commits, graph,
// branches, tags, stashes, remotes, one file's history, and the repositories
// the session's panes are in.
func (d *Daemon) verbReviewHistory(cs *connState, params json.RawMessage) (any, *verbError) {
	var p reviewHistoryParams
	if verr := decodeParams(params, &p); verr != nil {
		return nil, verr
	}
	if verr := refuseForwardedPane(cs, "review-history"); verr != nil {
		return nil, verr
	}
	sess, _, repo, verr := d.reviewTarget(p.Session, p.Window)
	if verr != nil {
		return nil, verr
	}
	kind := review.HistoryKind(p.Kind)
	if kind == review.HistoryRepos {
		rows := d.historyRepos(sess)
		return map[string]any{
			"type": "review_history", "kind": kind, "title": "repositories", "rows": rows,
		}, nil
	}
	ctx, cancel := context.WithTimeout(d.ctx, 15*time.Second)
	defer cancel()
	res, err := review.History(ctx, repo.root, kind, p.Path, p.Limit)
	if err != nil {
		return nil, hintedVerbError(ErrVerbGitFailed, err.Error(), &VerbHint{
			Detail: "Nothing was changed. The drawer is read-only.",
		})
	}
	return map[string]any{
		"type": "review_history", "kind": res.Kind, "title": res.Title, "rows": res.Rows,
	}, nil
}

// historyRepos lists the local repositories the session's panes are in, one
// row per repository as "root\twindow id". The first pane found in a
// repository names it, so choosing the row focuses a pane showing that repo.
func (d *Daemon) historyRepos(sess *Session) []string {
	ctx, cancel := context.WithTimeout(d.ctx, 10*time.Second)
	defer cancel()
	seen := map[string]bool{}
	rows := make([]string, 0, 8)
	for _, w := range sess.GetState().Windows {
		if w.Host != "" || w.Cwd == "" {
			continue
		}
		root, err := review.TopLevel(ctx, w.Cwd)
		if err != nil || root == "" || seen[root] {
			continue
		}
		seen[root] = true
		name := w.CustomName
		if name == "" {
			name = w.Title
		}
		if name == "" {
			name = w.ID
		}
		rows = append(rows, root+"\t"+w.ID+"\t"+name)
		if len(rows) >= 50 {
			break
		}
	}
	sort.Strings(rows)
	return rows
}
