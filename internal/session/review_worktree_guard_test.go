package session

import "testing"

// A captured pane is not enough: that pane can cd after the sidebar was read.
// The optional worktree precondition rejects the read or mutation before it
// reaches a different repository. Leaving it out preserves old clients.
func TestReviewVerbsRefuseAMismatchedWorktree(t *testing.T) {
	_, sp, repo, _, c, a, _ := reviewFixture(t)
	tui := attachTUI(t, sp, "work")
	other := t.TempDir()
	for _, verb := range []string{"review-diff", "review-note", "send-review"} {
		p := map[string]any{
			"session": "work", "window": a, "expected_worktree": other,
			"human_nonce": tui.HumanNonce(),
		}
		switch verb {
		case "review-diff":
			delete(p, "human_nonce")
			p["index"] = "staged"
		case "review-note":
			p["action"], p["path"], p["line"], p["text"] = "add", "api.go", 1, "must not land"
		}
		mustRefuse(t, callP(c, t, verb, p), ErrVerbNotRepo, "a pane moved after the sidebar reading: "+verb)
	}
	// The refusal must not leave any note behind in the pane's real repo.
	listed := result(t, callP(c, t, "review-note", map[string]any{"session": "work", "window": a, "action": "list"}))
	if len(notesOf(t, listed)) != 0 {
		t.Fatal("a rejected mutation left a comment behind")
	}
	result(t, callP(c, t, "review-diff", map[string]any{"session": "work", "window": a, "expected_worktree": repo, "index": "staged"}))
}
