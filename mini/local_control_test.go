package mini_test

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/getan/tuios-mini/mini"
	"github.com/getan/tuios-mini/mini/control"
)

// A workspace's agent runs in a pane, and through mini/control it drives the
// workspace it is in: it opens a pane for a browser or a build, renames one,
// focuses one, moves one to another workspace, resizes one and closes one. Under
// the strict default of mini (read, write and fan) all six used to be refused,
// because they were the admin verbs they are on the standalone product. These
// tests drive the real daemon and a real PTY, and hold the line the admission
// draws: the caller's own session, and nothing beyond it.

// miniPane is the pane a test calls as: its window id and the token its process
// presents.
type miniPane struct {
	window string
	token  string
}

// startStrictWorkspace starts a workspace whose panes hold the strict default of
// read, write and fan, makes a second session so a foreign target exists, and
// returns the first pane of session work, the person's own connection to the
// daemon, and the window of the session other.
func startStrictWorkspace(t *testing.T) (*mini.Server, *control.Client, miniPane, string) {
	t.Helper()
	srv, c := startMiniAt(t, mini.Config{
		Session:         "work",
		PermissionsMode: "strict",
		// The default grants, named explicitly: read, write and fan.
		Grants: []string{"read", "write", "fan"},
	})
	first := call[windowList](t, c, "list-windows", map[string]any{"session": "work"}).Windows[0].WindowID
	other := call[newSession](t, c, "new-session", map[string]any{"name": "other", "window": true})
	if other.WindowID == "" {
		t.Fatal("new-session other returned no window")
	}
	return srv, c, miniPane{window: first, token: paneToken(t, c, first)}, other.WindowID
}

// paneToken reads the TUIOS_PANE_TOKEN of a pane of session work out of the pane
// itself, which is what a process there would do. The session is named because a
// second one exists by then and a connection outside every pane would otherwise
// look in the most recently active one. The command line is built from two quoted
// halves, so only the output line matches the pattern.
func paneToken(t *testing.T, c *control.Client, window string) string {
	t.Helper()
	call[json.RawMessage](t, c, "send-text", map[string]any{
		"session": "work", "window": window,
		"text": `printf 'T%s=%s\n' "OKEN" "$TUIOS_PANE_TOKEN"`, "submit": true,
	})
	call[waitResult](t, c, "wait-for", map[string]any{
		"session": "work", "window": window, "condition": "window-output", "pattern": "TOKEN=", "timeout": 5000,
	})
	content := call[capture](t, c, "capture-pane", map[string]any{
		"session": "work", "window": window, "source": "visible", "lines": 40,
	}).Content
	match := regexp.MustCompile(`TOKEN=(\S+)`).FindStringSubmatch(content)
	if len(match) != 2 {
		t.Fatalf("capture of %s = %q, want TOKEN=<token>", window, content)
	}
	return match[1]
}

// restrictToPane opens a second connection and holds it to the pane's own
// session, the way a process in the pane does: it presents the pane's token with
// `pane-grants`, which is how the CLI places a connection the kernel cannot
// place, and then restricts itself.
func restrictToPane(t *testing.T, srv *mini.Server, p miniPane, readOnly bool) *control.Client {
	t.Helper()
	c, err := control.Dial(context.Background(), srv.Socket())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	placed := call[json.RawMessage](t, c, "pane-grants", map[string]any{"pane_id": p.window, "pane_token": p.token})
	if !strings.Contains(string(placed), `"pane":true`) {
		t.Fatalf("the connection was not placed in pane %s: %s", p.window, placed)
	}
	params := map[string]any{"scope": "own", "pane_id": p.window, "pane_token": p.token}
	if readOnly {
		params["read_only"] = true
	}
	call[json.RawMessage](t, c, "restrict-connection", params)
	return c
}

// wantRefused asserts that a call is refused as forbidden.
func wantRefused(t *testing.T, c *control.Client, verb string, params map[string]any) {
	t.Helper()
	raw, err := c.Call(context.Background(), verb, params)
	var de *control.Error
	if !errors.As(err, &de) || de.Code != "forbidden" {
		t.Fatalf("%s %v = %s, %v; want forbidden", verb, params, raw, err)
	}
}

// TestMiniAPaneArrangesItsOwnWorkspace drives the six verbs a workspace's agent
// needs through mini/control, from a connection held to the pane's own session:
// open a pane, rename it, focus it, move it to another workspace, resize it, and
// close it.
func TestMiniAPaneArrangesItsOwnWorkspace(t *testing.T) {
	srv, person, p, _ := startStrictWorkspace(t)
	c := restrictToPane(t, srv, p, false)
	// The pane reports the grants it was started with, which are the strict
	// default and not admin. The marker is split so the echoed command line
	// cannot be what the wait matched.
	call[json.RawMessage](t, person, "send-text", map[string]any{
		"session": "work", "window": p.window, "text": `printf 'G%s=%s\n' "RANTS" "$TUIOS_PANE_GRANTS"`, "submit": true,
	})
	call[waitResult](t, person, "wait-for", map[string]any{
		"session": "work", "window": p.window, "condition": "window-output", "pattern": "RANTS=read", "timeout": 5000,
	})
	if got := call[capture](t, person, "capture-pane", map[string]any{
		"session": "work", "window": p.window, "source": "visible", "lines": 60,
	}).Content; !strings.Contains(got, "GRANTS=read,write,fan") {
		t.Fatalf("the pane was not started on the strict default:\n%s", got)
	}

	// Open a pane in the caller's own session. It holds no more than the pane
	// that asked for it, and the workspace can close it again.
	opened := call[newWindow](t, c, "new-window", map[string]any{
		"session": "work", "name": "opened-by-a-pane", "focus": false,
		"command": []string{"/bin/sh", "-c", "sleep 30"},
	})
	if opened.WindowID == "" {
		t.Fatal("new-window from a pane holding write returned no window")
	}

	// Rename it, and read back what the verb says it did.
	renamed := call[setResult](t, c, "set-window", map[string]any{
		"session": "work", "window": opened.WindowID, "name": "renamed-by-a-pane",
	})
	if renamed.DisplayName != "renamed-by-a-pane" {
		t.Fatalf("set-window reported name %q, want renamed-by-a-pane", renamed.DisplayName)
	}

	// Focus it, and read the focus from the person's side.
	call[json.RawMessage](t, c, "focus-window", map[string]any{"session": "work", "window": opened.WindowID})
	if got := call[windowList](t, person, "list-windows", map[string]any{"session": "work"}).FocusedWindowID; got != opened.WindowID {
		t.Fatalf("after the pane focused %s, the focused window is %s", opened.WindowID, got)
	}

	// Move it to another workspace of the same session, then resize it there.
	call[json.RawMessage](t, c, "move-window", map[string]any{
		"session": "work", "window": opened.WindowID, "workspace": 2,
	})
	call[json.RawMessage](t, c, "resize", map[string]any{
		"session": "work", "window": opened.WindowID, "width": 90, "height": 20,
	})

	// Close it. The pane opened it, so it holds nothing the pane does not.
	call[json.RawMessage](t, c, "close-window", map[string]any{"session": "work", "window": opened.WindowID})
	for _, row := range call[windowList](t, person, "list-windows", map[string]any{"session": "work"}).Windows {
		if row.WindowID == opened.WindowID {
			t.Fatalf("close-window from the pane left %s open", opened.WindowID)
		}
	}
}

// TestMiniAPaneCannotReachPastItsSession holds the admission to the one session
// the caller is in: another session, another session's window, a move into
// another session, and a pane on another machine are all refused, and so is
// every verb admin still owns.
func TestMiniAPaneCannotReachPastItsSession(t *testing.T) {
	srv, _, p, other := startStrictWorkspace(t)
	c := restrictToPane(t, srv, p, false)

	wantRefused(t, c, "new-window", map[string]any{"session": "other", "name": "x"})
	wantRefused(t, c, "close-window", map[string]any{"session": "other", "window": other})
	wantRefused(t, c, "set-window", map[string]any{"session": "other", "window": other, "name": "x"})
	wantRefused(t, c, "focus-window", map[string]any{"session": "other", "window": other})
	wantRefused(t, c, "move-window", map[string]any{"session": "other", "window": other, "workspace": 2})
	wantRefused(t, c, "resize", map[string]any{"session": "other", "window": other, "width": 80, "height": 20})
	// to_session takes a window out of the session the pane may arrange.
	wantRefused(t, c, "move-window", map[string]any{"session": "work", "window": p.window, "to_session": "other"})
	// A pane on another machine is another machine's.
	wantRefused(t, c, "new-window", map[string]any{"session": "work", "name": "x", "host": "build"})

	// Everything else admin owns stays refused, in the caller's own session too.
	for _, call := range []struct {
		verb   string
		params map[string]any
	}{
		{"kill-session", map[string]any{"session": "work"}},
		{"set-option", map[string]any{"session": "work", "key": "appearance.sidebar.enabled", "value": "true"}},
		{"list-sessions", nil},
		{"search-panes", nil},
		{"close-workspace", map[string]any{"session": "work", "workspace": 1}},
		{"set-layout", map[string]any{"session": "work", "tiling": true}},
		{"split-window", map[string]any{"session": "work", "window": p.window, "direction": "vertical"}},
		{"file-read", map[string]any{"path": "/etc/hosts"}},
		{"new-session", map[string]any{"name": "second"}},
	} {
		wantRefused(t, c, call.verb, call.params)
	}
}

// TestMiniAReadOnlyPaneCannotArrangeItsWorkspace: a connection restricted to
// read_only keeps its reach for reading and loses all six verbs, whatever the
// pane's grants are.
func TestMiniAReadOnlyPaneCannotArrangeItsWorkspace(t *testing.T) {
	srv, person, p, _ := startStrictWorkspace(t)
	c := restrictToPane(t, srv, p, true)

	// Reading the session still works, so the refusals below are about writing.
	call[windowList](t, c, "list-windows", map[string]any{"session": "work"})
	wantRefused(t, c, "new-window", map[string]any{"session": "work", "name": "x", "command": []string{"/bin/sh", "-c", "sleep 30"}})
	wantRefused(t, c, "close-window", map[string]any{"session": "work", "window": p.window})
	wantRefused(t, c, "set-window", map[string]any{"session": "work", "window": p.window, "name": "x"})
	wantRefused(t, c, "focus-window", map[string]any{"session": "work", "window": p.window})
	wantRefused(t, c, "move-window", map[string]any{"session": "work", "window": p.window, "workspace": 2})
	wantRefused(t, c, "resize", map[string]any{"session": "work", "window": p.window, "width": 80, "height": 20})

	// The person's own connection, outside every pane, is untouched.
	call[json.RawMessage](t, person, "close-window", map[string]any{"session": "work", "window": p.window})
}

// TestMiniAPaneGivenNoWriteCannotArrangeItsWorkspace: the admission is the write
// grant's, not the workspace's. A pane narrowed to read still cannot open, close
// or arrange anything.
func TestMiniAPaneGivenNoWriteCannotArrangeItsWorkspace(t *testing.T) {
	srv, person, p, _ := startStrictWorkspace(t)
	// The person narrows the pane, which is how a workspace hands a pane only
	// what it should have.
	call[json.RawMessage](t, person, "set-pane-grants", map[string]any{
		"session": "work", "window": p.window, "grants": []string{"read"},
	})
	c := restrictToPane(t, srv, p, false)
	wantRefused(t, c, "new-window", map[string]any{"session": "work", "name": "x"})
	wantRefused(t, c, "close-window", map[string]any{"session": "work", "window": p.window})
	wantRefused(t, c, "set-window", map[string]any{"session": "work", "window": p.window, "name": "x"})
	wantRefused(t, c, "focus-window", map[string]any{"session": "work", "window": p.window})
	wantRefused(t, c, "move-window", map[string]any{"session": "work", "window": p.window, "workspace": 2})
	wantRefused(t, c, "resize", map[string]any{"session": "work", "window": p.window, "width": 80, "height": 20})
}

// TestMiniAPaneOpensOnlyPanesThatHoldNoMore: the pane a workspace's agent opens
// holds what the agent holds, never more, and asking for more is refused. The
// pane it opened it can close; one that holds more than it does, it cannot.
func TestMiniAPaneOpensOnlyPanesThatHoldNoMore(t *testing.T) {
	srv, person, p, _ := startStrictWorkspace(t)
	c := restrictToPane(t, srv, p, false)

	wantRefused(t, c, "new-window", map[string]any{"session": "work", "name": "wide", "grants": []string{"admin"}})
	wantRefused(t, c, "new-window", map[string]any{"session": "work", "name": "wide", "grants": []string{"read", "respond"}})

	opened := call[newWindow](t, c, "new-window", map[string]any{
		"session": "work", "name": "narrow", "focus": false, "grants": []string{"read"},
	})
	if opened.WindowID == "" {
		t.Fatal("new-window naming grants the caller holds returned no window")
	}

	// A pane holding read and not write arranges nothing, including the pane
	// that opened it.
	narrow := restrictToPane(t, srv, miniPane{window: opened.WindowID, token: paneToken(t, person, opened.WindowID)}, false)
	wantRefused(t, narrow, "close-window", map[string]any{"session": "work", "window": p.window})

	// So does a pane holding more than the caller: the person may give one
	// admin, and the caller may not end the work in it.
	wide := call[newWindow](t, person, "new-window", map[string]any{
		"session": "work", "name": "wide", "focus": false,
	})
	call[json.RawMessage](t, person, "set-pane-grants", map[string]any{
		"session": "work", "window": wide.WindowID, "grants": []string{"admin"},
	})
	wantRefused(t, c, "close-window", map[string]any{"session": "work", "window": wide.WindowID})

	// A pane holding no more than the caller, it may close.
	call[json.RawMessage](t, c, "close-window", map[string]any{"session": "work", "window": opened.WindowID})
}
