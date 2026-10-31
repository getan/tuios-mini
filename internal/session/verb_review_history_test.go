package session

import (
	"fmt"
	"strings"
	"testing"

	"github.com/getan/tuios-mini/internal/testutil"
)

// Exercise the public dispatcher, not just verbReviewHistory: the handler
// understood details while the wire registry rejected both ref and the kinds.
func TestReviewHistoryListsAndDetailsOverWire(t *testing.T) {
	_, _, repo, _, c, window, _ := reviewFixture(t)
	testutil.Git(t, repo, "branch", "drawer-branch")
	testutil.Git(t, repo, "tag", "drawer-tag")
	testutil.Git(t, repo, "remote", "add", "drawer-remote", "https://example.invalid/drawer.git")
	writeIn(t, repo, "api.go", "package api\n// drawer stash\n")
	testutil.Git(t, repo, "stash", "push", "-q", "-m", "drawer-stash")

	for _, tt := range []struct{ kind, detail, want string }{
		{"commits", "commit", "commit "},
		{"graph", "commit", "commit "},
		{"branches", "branch", "commit "},
		{"tags", "tag", "commit "},
		{"stashes", "stash", "drawer stash"},
		{"remotes", "remote", "https://example.invalid/drawer.git"},
		{"file", "commit", "commit "},
		{"repos", "", repo},
	} {
		t.Run(tt.kind, func(t *testing.T) {
			params := map[string]any{"session": "work", "window": window, "kind": tt.kind, "limit": 100}
			if tt.kind == "file" {
				params["path"] = "api.go"
			}
			list := result(t, callP(c, t, "review-history", params))
			rows, _ := list["rows"].([]any)
			if len(rows) == 0 {
				t.Fatalf("empty %s list: %v", tt.kind, list)
			}
			if tt.detail == "" {
				if !strings.Contains(fmt.Sprint(rows), tt.want) {
					t.Fatalf("repositories lost %q: %v", tt.want, list)
				}
				return
			}
			fields := strings.Fields(rows[0].(string))
			if tt.kind == "graph" {
				fields = fields[1:] // the fixture's single commit starts with '*'
			}
			params["kind"], params["ref"], params["limit"] = tt.detail, fields[0], 500
			delete(params, "path")
			detail := result(t, callP(c, t, "review-history", params))
			if detail["kind"] != tt.detail || !strings.Contains(fmt.Sprint(detail["rows"]), tt.want) {
				t.Fatalf("detail lost %q: %v", tt.want, detail)
			}
			t.Logf("%s -> %s %s: %v", tt.kind, tt.detail, fields[0], detail["title"])
		})
	}
}
