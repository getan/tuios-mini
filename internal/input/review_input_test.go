package input

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/getan/tuios-mini/internal/app"
	"github.com/getan/tuios-mini/internal/config"
	"github.com/getan/tuios-mini/internal/review"
	"github.com/getan/tuios-mini/internal/session"
)

// reviewInputOS is a client on a daemon session whose daemon answers
// review-diff with one changed line, and records every verb it is sent.
func reviewInputOS(t *testing.T) (*app.OS, *[]byte, *[]string) {
	t.Helper()
	o := reachOS(t)
	o.IsDaemonSession = true
	o.SessionName = "work"
	var typed []byte
	w := o.GetFocusedWindow()
	w.DaemonMode = true
	w.DaemonWriteFunc = func(b []byte) error { typed = append(typed, b...); return nil }
	var verbs []string
	o.SetInboxVerbCaller(func(verb string, params map[string]any, _ time.Duration) (json.RawMessage, error) {
		verbs = append(verbs, verb)
		switch verb {
		case "review-diff":
			return json.Marshal(map[string]any{
				"session": "work", "window": "reach", "base": "main",
				"files": []review.File{{Path: "a.go", Status: "M", Added: 1, Hunks: []review.Hunk{{
					Header: "@@ -1 +1,2 @@", OldStart: 1, OldLines: 1, NewStart: 1, NewLines: 2,
					Lines: []review.Line{{Op: "context", Old: 1, New: 1, Text: "package a"}, {Op: "add", New: 2, Text: "var x = 1"}},
				}}}},
				"totals": review.Totals{Files: 1, Added: 1},
			})
		case "review-note":
			return json.Marshal(map[string]any{"notes": []review.Note{}})
		}
		return nil, &session.VerbCallError{Code: session.ErrVerbInvalidParams, Message: "no"}
	}, func() string { return "nonce" })
	return o, &typed, &verbs
}

// TestReviewKeyOnAnOlderDaemonReachesThePane: once a daemon has answered
// review-diff with unknown_verb, ctrl+b v in terminal mode does what an
// unbound key does: v is typed into the pane and nothing more is asked of the
// daemon. (The list-verbs probe that finds the same as the Inbox watch starts
// is tested in the app package.)
func TestReviewKeyOnAnOlderDaemonReachesThePane(t *testing.T) {
	o, typed, _ := reviewInputOS(t)
	var verbs []string
	o.SetInboxVerbCaller(func(verb string, _ map[string]any, _ time.Duration) (json.RawMessage, error) {
		verbs = append(verbs, verb)
		return nil, &session.VerbCallError{Code: session.ErrVerbUnknownVerb, Message: "unknown verb " + verb}
	}, func() string { return "nonce" })
	o.Mode = app.TerminalMode
	k := config.DefaultConfig().Keybindings
	o = pressRun(t, o, k.LeaderKey)
	o = pressRun(t, o, "v")
	if o.ReviewOpen() || len(*typed) != 0 || len(verbs) != 1 {
		t.Fatalf("the first ctrl+b v: open %v, typed %q, verbs %v", o.ReviewOpen(), *typed, verbs)
	}
	o = pressRun(t, o, k.LeaderKey)
	o = pressRun(t, o, "v")
	if o.ReviewOpen() {
		t.Fatal("ctrl+b v opened a review on a daemon that cannot review")
	}
	if len(verbs) != 1 {
		t.Fatalf("ctrl+b v asked an older daemon again: %v", verbs)
	}
	if string(*typed) != "" {
		t.Fatalf("unsupported workspace command leaked into the pane: %q", *typed)
	}
}

func TestReviewMouseSelectionRoutesWithoutReachingPane(t *testing.T) {
	o, typed, _ := reviewInputOS(t)
	cmd, handled := o.ReviewFocusedPane()
	if !handled || cmd == nil {
		t.Fatal("review did not start")
	}
	o.Update(cmd())
	_ = o.View()
	frame := strings.Split(ansi.Strip(o.GetCanvas(true).Render()), "\n")
	for y, line := range frame {
		if index := strings.Index(line, "package a"); index >= 0 {
			x := ansi.StringWidth(line[:index])
			HandleInput(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}, o)
			HandleInput(tea.MouseMotionMsg{X: x + 7, Y: y, Button: tea.MouseLeft}, o)
			_, copied := HandleInput(tea.MouseReleaseMsg{X: x + 7, Y: y, Button: tea.MouseLeft}, o)
			if copied == nil {
				t.Fatal("review drag did not copy")
			}
			if len(*typed) != 0 {
				t.Fatalf("mouse leaked to pane: %q", *typed)
			}
			return
		}
	}
	t.Fatal("review diff text not visible")
}
