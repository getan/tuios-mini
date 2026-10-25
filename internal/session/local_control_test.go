package session

import (
	"encoding/json"
	"strings"
	"testing"
)

// A workspace's panes arrange their own session. These tests build the daemon,
// its sessions, its panes and their grants in memory and ask the two admission
// layers directly (checkGrants for a pane's grants, checkScope for a connection
// that restricted itself). Nothing here starts a PTY, so the answers are the
// same on every machine and in a sandbox with no terminal; mini/local_control_test.go
// runs the same policy through a real daemon and client.

// layoutVerbsInOrder is the set the admission is about, as a test may name it.
var layoutVerbsInOrder = []string{"new-window", "close-window", "focus-window", "move-window", "set-window", "resize"}

// localLayoutFixture is a daemon of the given profile with sessions a (two
// windows) and b (one), the pane of a1 placed in conn, and no PTY anywhere. The
// panes hold the grants the caller passes, recorded as explicit so the default
// does not stand in for them.
func localLayoutFixture(t *testing.T, profile Profile) (d *Daemon, a1, a2, b1 string) {
	t.Helper()
	d = NewDaemon(&DaemonConfig{
		Profile:               profile,
		Version:               "test",
		StateDir:              t.TempDir(),
		DisableAutoRestore:    true,
		DisableSignalHandling: true,
	})
	t.Cleanup(d.cancel)
	a1, a2, b1 = addLayoutSessions(t, d)
	return d, a1, a2, b1
}

// addLayoutSessions makes the three windows the tests address, in two sessions,
// with no process behind any of them.
func addLayoutSessions(t *testing.T, d *Daemon) (a1, a2, b1 string) {
	t.Helper()
	add := func(sess *Session, name string) string {
		id := "win-" + name
		if err := sess.mutateState(func(st *SessionState) error {
			st.Windows = append(st.Windows, WindowState{ID: id, Title: name, Workspace: 1})
			if st.FocusedWindowID == "" {
				st.FocusedWindowID = id
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	sa, err := d.manager.CreateSession("a", &SessionConfig{}, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sa.Stop)
	sb, err := d.manager.CreateSession("b", &SessionConfig{}, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sb.Stop)
	return add(sa, "a1"), add(sa, "a2"), add(sb, "b1")
}

// holdGrants gives the pane of every window the same grants, explicitly, so a
// test is not measuring [agents.permissions].
func holdGrants(t *testing.T, d *Daemon, g Grants, windows ...string) {
	t.Helper()
	for _, id := range windows {
		sess := d.sessionHoldingWindow(id)
		if sess == nil {
			t.Fatalf("window %s is in no session", id)
		}
		d.manager.grants.add(id, "pty-"+id, sess.Name(), &g)
	}
}

// paneConn is a connection placed in window by its token, which is how a pane's
// process reaches this daemon.
func paneConn(window string) *connState {
	cs := &connState{clientID: "pane " + window}
	w := window
	cs.paneBound.Store(&w)
	return cs
}

// ownScope restricts cs to its own session the way restrict-connection does.
func ownScope(d *Daemon, cs *connState, window string, readOnly bool) {
	sess := d.sessionHoldingWindow(window)
	cs.scope.Store(&connScope{own: true, readOnly: readOnly, window: window, sessionID: sess.ID, via: "token"})
}

func layoutVerbParams(a1, a2, b1 string) map[string]map[string]json.RawMessage {
	return map[string]map[string]json.RawMessage{
		"new-window":   {"own": json.RawMessage(`{"name":"next"}`), "other": json.RawMessage(`{"session":"b","name":"next"}`)},
		"close-window": {"own": json.RawMessage(`{"window":"` + a2 + `"}`), "other": json.RawMessage(`{"session":"b","window":"` + b1 + `"}`)},
		"focus-window": {"own": json.RawMessage(`{"window":"` + a2 + `"}`), "other": json.RawMessage(`{"session":"b","window":"` + b1 + `"}`)},
		"move-window":  {"own": json.RawMessage(`{"window":"` + a2 + `","workspace":2}`), "other": json.RawMessage(`{"session":"b","window":"` + b1 + `","workspace":2}`)},
		"set-window":   {"own": json.RawMessage(`{"window":"` + a2 + `","name":"renamed"}`), "other": json.RawMessage(`{"session":"b","window":"` + b1 + `","name":"renamed"}`)},
		"resize":       {"own": json.RawMessage(`{"width":100,"height":30}`), "other": json.RawMessage(`{"session":"b","width":100,"height":30}`)},
	}
}

// TestAWorkspacePaneArrangesItsOwnSession: a pane holding write, in a local-only
// workspace, may open, close, focus, rename, move and resize the panes of its own
// session, and the call it passes on names that session.
func TestAWorkspacePaneArrangesItsOwnSession(t *testing.T) {
	d, a1, a2, b1 := localLayoutFixture(t, ProfileLocalOnly)
	holdGrants(t, d, GrantRead|GrantWrite|GrantFan, a1, a2, b1)
	for _, verb := range layoutVerbsInOrder {
		cs := paneConn(a1)
		out, verr := d.checkGrants(cs, verb, layoutVerbParams(a1, a2, b1)[verb]["own"])
		if verr != nil {
			t.Errorf("%s from a pane holding write = %v, want it admitted", verb, verr)
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(out, &m); err != nil {
			t.Fatalf("%s params = %s, want JSON", verb, out)
		}
		if m["session"] != "a" {
			t.Errorf("%s passed on %v, want the caller's own session a", verb, m)
		}
	}
}

// TestAWorkspacePaneNeedsWriteToArrange: read alone, and no grants at all, hold
// the six verbs away, and the refusal names the write grant.
func TestAWorkspacePaneNeedsWriteToArrange(t *testing.T) {
	d, a1, a2, b1 := localLayoutFixture(t, ProfileLocalOnly)
	for _, tc := range []struct {
		name   string
		grants Grants
	}{{"read", GrantRead}, {"none", 0}, {"fan without write", GrantRead | GrantFan}} {
		holdGrants(t, d, tc.grants, a1, a2, b1)
		for _, verb := range layoutVerbsInOrder {
			_, verr := d.checkGrants(paneConn(a1), verb, layoutVerbParams(a1, a2, b1)[verb]["own"])
			if verr == nil {
				t.Errorf("%s from a pane holding %s was admitted", verb, tc.name)
				continue
			}
			if verr.Code != ErrVerbForbidden {
				t.Errorf("%s holding %s answered %s, want forbidden", verb, tc.name, verr.Code)
			}
			if !strings.Contains(verr.Message, "write grant") {
				t.Errorf("%s holding %s was refused with %q, want it to name the write grant", verb, tc.name, verr.Message)
			}
		}
	}
}

// TestTheStandaloneProductStillRefusesTheLayoutVerbs: the same panes, the same
// grants, the profile the standalone product uses. Every one of the six is
// refused to a pane and to a restricted connection, as before this admission
// existed.
func TestTheStandaloneProductStillRefusesTheLayoutVerbs(t *testing.T) {
	d, a1, a2, b1 := localLayoutFixture(t, ProfileStandalone)
	holdGrants(t, d, GrantRead|GrantWrite|GrantFan, a1, a2, b1)
	for _, verb := range layoutVerbsInOrder {
		granted, verr := d.checkGrants(paneConn(a1), verb, layoutVerbParams(a1, a2, b1)[verb]["own"])
		if verr == nil || verr.Code != ErrVerbForbidden {
			t.Fatalf("%s from a pane holding write on a standalone daemon = %s, %v; want forbidden", verb, granted, verr)
		}
		if !strings.Contains(verr.Message, "needs the admin grant") {
			t.Errorf("%s was refused with %q, want the admin-grant refusal of before", verb, verr.Message)
		}

		cs := paneConn(a1)
		ownScope(d, cs, a1, false)
		if scoped, verr := d.checkScope(cs, verb, layoutVerbParams(a1, a2, b1)[verb]["own"]); verr == nil || verr.Code != ErrVerbForbidden {
			t.Fatalf("%s on a restricted own connection to a standalone daemon = %s, %v; want forbidden", verb, scoped, verr)
		}
	}
}

// TestABoundedConnectionArrangesItsOwnSession: a connection that restricted
// itself to its own session may use the six verbs there, and a read-only one may
// not. A connection restricted to every session gets neither: the admission is
// for a caller held to one session.
func TestABoundedConnectionArrangesItsOwnSession(t *testing.T) {
	d, a1, a2, b1 := localLayoutFixture(t, ProfileLocalOnly)
	holdGrants(t, d, GrantRead|GrantWrite|GrantFan, a1, a2, b1)
	params := layoutVerbParams(a1, a2, b1)
	for _, verb := range layoutVerbsInOrder {
		cs := paneConn(a1)
		ownScope(d, cs, a1, false)
		out, verr := d.checkScope(cs, verb, params[verb]["own"])
		if verr != nil {
			t.Errorf("%s on a writable own connection = %v, want it admitted", verb, verr)
		} else if !strings.Contains(string(out), `"session":"a"`) {
			t.Errorf("%s passed on %s, want the caller's own session a", verb, out)
		}

		ro := paneConn(a1)
		ownScope(d, ro, a1, true)
		_, verr = d.checkScope(ro, verb, params[verb]["own"])
		if verr == nil || verr.Code != ErrVerbForbidden {
			t.Errorf("%s on a read-only own connection = %v, want forbidden", verb, verr)
		} else if !strings.Contains(verr.Message, "read-only") {
			t.Errorf("%s on a read-only connection was refused with %q, want it to say so", verb, verr.Message)
		}

		all := paneConn(a1)
		all.scope.Store(&connScope{own: false, window: a1, sessionID: d.sessionHoldingWindow(a1).ID})
		if _, verr := d.checkScope(all, verb, params[verb]["own"]); verr == nil {
			t.Errorf("%s on a connection restricted to all was admitted", verb)
		}
	}
}

// TestTheTwoLayersAgreeOnALayoutVerb: a pane holding write and a connection that
// is not read-only both admit the six verbs; a pane holding read alone and a
// read-only connection both refuse them. One answer for both layers is the point
// of the shared admission.
func TestTheTwoLayersAgreeOnALayoutVerb(t *testing.T) {
	d, a1, a2, b1 := localLayoutFixture(t, ProfileLocalOnly)
	params := layoutVerbParams(a1, a2, b1)
	for _, verb := range layoutVerbsInOrder {
		holdGrants(t, d, GrantRead|GrantWrite|GrantFan, a1, a2, b1)
		cs := paneConn(a1)
		ownScope(d, cs, a1, false)
		_, grantsErr := d.checkGrants(cs, verb, params[verb]["own"])
		_, scopeErr := d.checkScope(cs, verb, params[verb]["own"])
		if grantsErr != nil || scopeErr != nil {
			t.Errorf("%s with write and a writable own connection: grants %v, scope %v; want both to admit it", verb, grantsErr, scopeErr)
		}

		holdGrants(t, d, GrantRead, a1, a2, b1)
		cs = paneConn(a1)
		ownScope(d, cs, a1, true)
		_, grantsErr = d.checkGrants(cs, verb, params[verb]["own"])
		_, scopeErr = d.checkScope(cs, verb, params[verb]["own"])
		if grantsErr == nil || scopeErr == nil {
			t.Errorf("%s with read only and a read-only connection: grants %v, scope %v; want both to refuse it", verb, grantsErr, scopeErr)
		}
	}
}

// TestTheLayoutVerbsReachOnlyTheCallersSession: another session is refused
// whatever the caller names, and so is one in the caller's fan group. The
// reach read and fan have is not this kind's reach.
func TestTheLayoutVerbsReachOnlyTheCallersSession(t *testing.T) {
	d, a1, a2, b1 := localLayoutFixture(t, ProfileLocalOnly)
	holdGrants(t, d, GrantRead|GrantWrite|GrantFan, a1, a2, b1)
	params := layoutVerbParams(a1, a2, b1)
	for _, verb := range layoutVerbsInOrder {
		// Both layers refuse a session that is not the caller's own.
		if _, verr := d.checkGrants(paneConn(a1), verb, params[verb]["other"]); verr == nil || verr.Code != ErrVerbForbidden {
			t.Errorf("%s naming session b, refused by the pane's grants = %v, want forbidden", verb, verr)
		}
		cs := paneConn(a1)
		ownScope(d, cs, a1, false)
		if _, verr := d.checkScope(cs, verb, params[verb]["other"]); verr == nil || verr.Code != ErrVerbForbidden {
			t.Errorf("%s naming session b, on a restricted own connection = %v, want forbidden", verb, verr)
		}
	}

	// The fan group a read grant reaches is out of reach for arranging.
	mustSetWorktree(t, d, "b", &WorktreeInfo{LaunchedFrom: "a"})
	for _, verb := range layoutVerbsInOrder {
		_, verr := d.checkGrants(paneConn(a1), verb, params[verb]["other"])
		if verr == nil {
			t.Errorf("%s naming the session a launched = nil, want forbidden: the reach is the pane's session only", verb)
			continue
		}
		if !strings.Contains(verr.Message, "admin grant") {
			t.Errorf("%s on the fan group was refused with %q, want it to name the admin grant", verb, verr.Message)
		}
	}
	// read still reaches it, so the fixture is what this test says it is.
	if _, verr := d.checkGrants(paneConn(a1), "list-windows", json.RawMessage(`{"session":"b"}`)); verr != nil {
		t.Fatalf("read does not reach the launched session either: %v", verr)
	}
}

// TestTheLayoutVerbsRefuseAnotherSessionMove: to_session takes a window out of
// the session the caller may arrange, so it is refused in both layers even
// though every session it names is one the caller reaches by other means.
func TestTheLayoutVerbsRefuseAnotherSessionMove(t *testing.T) {
	d, a1, a2, b1 := localLayoutFixture(t, ProfileLocalOnly)
	holdGrants(t, d, GrantRead|GrantWrite|GrantFan, a1, a2, b1)
	mustSetWorktree(t, d, "b", &WorktreeInfo{LaunchedFrom: "a"})
	for _, to := range []string{"b", "a"} {
		params := json.RawMessage(`{"window":"` + a2 + `","to_session":"` + to + `"}`)
		_, verr := d.checkGrants(paneConn(a1), "move-window", params)
		if verr == nil || verr.Code != ErrVerbForbidden {
			t.Errorf("move-window to_session %s from a pane = %v, want forbidden", to, verr)
		} else if !strings.Contains(verr.Message, "to_session") || !strings.Contains(verr.Message, echoName(to)) {
			t.Errorf("move-window to_session %s was refused with %q, want it to name the session", to, verr.Message)
		}

		cs := paneConn(a1)
		ownScope(d, cs, a1, false)
		if _, verr := d.checkScope(cs, "move-window", params); verr == nil {
			t.Errorf("move-window to_session %s over a restricted own connection was admitted", to)
		}
	}
}

// TestAWorkspacePaneStartsOnlyItsOwnPanes: new-window from a pane without admin
// gives the new pane no more than the pane that opened it, and starts nothing on
// another machine.
func TestAWorkspacePaneStartsOnlyItsOwnPanes(t *testing.T) {
	d, a1, a2, b1 := localLayoutFixture(t, ProfileLocalOnly)
	holdGrants(t, d, GrantRead|GrantWrite|GrantFan, a1, a2, b1)
	cs := paneConn(a1)

	// The handler reads the grants through launchGrants, which a pane cannot
	// talk past.
	if g, verr := d.launchGrants(cs, nil); verr != nil || g == nil || *g != GrantRead|GrantWrite|GrantFan {
		t.Errorf("new-window with no grants = %v, %v; want the caller's own", g, verr)
	}
	if _, verr := d.launchGrants(cs, []string{"admin"}); verr == nil || verr.Code != ErrVerbForbidden {
		t.Errorf("new-window asked for admin and got %v", verr)
	}
	if _, verr := d.launchGrants(cs, []string{"read", "respond"}); verr == nil || verr.Code != ErrVerbForbidden {
		t.Errorf("new-window asked for respond and got %v", verr)
	}

	// A window on another machine is another machine's pane.
	for _, host := range []string{"build", "elsewhere"} {
		params := json.RawMessage(`{"name":"remote","host":"` + host + `"}`)
		if _, verr := d.checkGrants(cs, "new-window", params); verr == nil || verr.Code != ErrVerbForbidden {
			t.Errorf("new-window on host %s = %v, want forbidden", host, verr)
		}
	}
	// "local" is this machine, which is what the caller meant.
	if _, verr := d.checkGrants(cs, "new-window", json.RawMessage(`{"name":"here","host":"local"}`)); verr != nil {
		t.Errorf("new-window on the local host = %v, want it admitted", verr)
	}
}

// TestCloseWindowHoldsTheTargetToTheCallersGrants: a pane closes panes that hold
// nothing it does not, and itself, but not a pane holding more.
func TestCloseWindowHoldsTheTargetToTheCallersGrants(t *testing.T) {
	d, a1, a2, b1 := localLayoutFixture(t, ProfileLocalOnly)
	holdGrants(t, d, GrantRead|GrantWrite|GrantFan, a1, a2, b1)
	if _, verr := d.checkGrants(paneConn(a1), "close-window", json.RawMessage(`{"window":"`+a2+`"}`)); verr != nil {
		t.Fatalf("closing a sibling that holds the same = %v, want it admitted", verr)
	}
	// A pane on the open default holds admin, which this one does not.
	admin := GrantAdmin
	d.manager.grants.add(a2, "pty-a2", "a", &admin)
	// The focused pane in a is a1; a call with no window means the focused one,
	// so a2 takes the focus for that case.
	focus := func(id string) {
		t.Helper()
		if err := d.sessionHoldingWindow(id).mutateState(func(st *SessionState) error {
			st.FocusedWindowID = id
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	focus(a2)
	for _, call := range []struct {
		what   string
		params json.RawMessage
	}{
		{"by id", json.RawMessage(`{"window":"` + a2 + `"}`)},
		{"the focused pane, which holds admin too", json.RawMessage(`{}`)},
	} {
		_, verr := d.checkGrants(paneConn(a1), "close-window", call.params)
		if verr == nil || verr.Code != ErrVerbForbidden {
			t.Errorf("closing an admin pane %s = %v, want forbidden", call.what, verr)
			continue
		}
		if !strings.Contains(verr.Message, "more than this pane holds") {
			t.Errorf("closing an admin pane %s was refused with %q, want the grant refusal", call.what, verr.Message)
		}
	}
	// The pane may always close itself.
	if _, verr := d.checkGrants(paneConn(a1), "close-window", json.RawMessage(`{"window":"`+a1+`"}`)); verr != nil {
		t.Errorf("a pane closing itself = %v, want it admitted", verr)
	}
}

// TestWhatAdminReachesStaysRefused: the admission is six verbs wide. Everything
// else a pane without admin could not call before, it cannot call now, and the
// list is the one the workspace host has no business with: the session itself,
// the options, the listings across sessions, the workspace and layout verbs, the
// file verbs, the client protocol's own verbs and the agent conversation.
func TestWhatAdminReachesStaysRefused(t *testing.T) {
	d, a1, a2, b1 := localLayoutFixture(t, ProfileLocalOnly)
	holdGrants(t, d, GrantRead|GrantWrite|GrantFan, a1, a2, b1)
	for _, call := range []struct {
		verb   string
		params json.RawMessage
	}{
		{"kill-session", json.RawMessage(`{"session":"a"}`)},
		{"rename-session", json.RawMessage(`{"session":"a","name":"z"}`)},
		{"set-session-name", json.RawMessage(`{"session":"a","name":"z"}`)},
		{"set-option", json.RawMessage(`{"session":"a","key":"x","value":"y"}`)},
		{"list-sessions", json.RawMessage(`{}`)},
		{"search-panes", json.RawMessage(`{}`)},
		{"select-workspace", json.RawMessage(`{"workspace":2}`)},
		{"set-layout", json.RawMessage(`{"tiling":true}`)},
		{"split-window", json.RawMessage(`{"window":"` + a2 + `","direction":"vertical"}`)},
		{"popup", json.RawMessage(`{"command":["true"]}`)},
		{"close-workspace", json.RawMessage(`{"workspace":2}`)},
		{"set-workspace-name", json.RawMessage(`{"workspace":1,"name":"z"}`)},
		{"run-command", json.RawMessage(`{"command":"ToggleZoom"}`)},
		{"resize-pane", json.RawMessage(`{"pane":"` + a2 + `","width":10,"height":10}`)},
		{"new-session", json.RawMessage(`{"name":"z"}`)},
		{"file-read", json.RawMessage(`{"path":"/etc/hosts"}`)},
		{"file-stat", json.RawMessage(`{"path":"/etc/hosts"}`)},
		{"transfer-list", json.RawMessage(`{}`)},
		{"new-worktree", json.RawMessage(`{"session":"a"}`)},
		{"open-host-connection", json.RawMessage(`{"host":"build"}`)},
		{"agent-transcript", json.RawMessage(`{"window":"` + a2 + `"}`)},
		{"detach-client", json.RawMessage(`{}`)},
		{"switch-session", json.RawMessage(`{"session":"a"}`)},
	} {
		_, verr := d.checkGrants(paneConn(a1), call.verb, call.params)
		if verr == nil || verr.Code != ErrVerbForbidden {
			t.Errorf("%s from a pane holding write = %v, want forbidden", call.verb, verr)
			continue
		}
		if !strings.Contains(verr.Message, "needs the admin grant") && !strings.Contains(verr.Message, "grant") {
			t.Errorf("%s was refused with %q, want it to name the missing grant", call.verb, verr.Message)
		}
		cs := paneConn(a1)
		ownScope(d, cs, a1, false)
		if _, verr := d.checkScope(cs, call.verb, call.params); verr == nil {
			t.Errorf("%s over a restricted own connection was admitted", call.verb)
		}
	}
}

// TestTheLayoutKindIsLocalOnlyAndKeepsTheTable holds the admission to its shape:
// the six verbs are scopeDeny in verbScopes (so the standalone product and every
// other reader of the table is unchanged), grantKind still answers scopeDeny for
// them, and only a local-only daemon hands out scopeLayout.
func TestTheLayoutKindIsLocalOnlyAndKeepsTheTable(t *testing.T) {
	for _, verb := range layoutVerbsInOrder {
		if kind, ok := verbScopes[verb]; !ok || kind != scopeDeny {
			t.Errorf("verbScopes[%q] = %v, %v; want the scopeDeny the standalone product reads", verb, kind, ok)
		}
		if grantKind(verb) != scopeDeny {
			t.Errorf("grantKind(%q) = %v, want scopeDeny without the profile", verb, grantKind(verb))
		}
	}
	if len(scopeLayoutVerbs) != len(layoutVerbsInOrder) {
		t.Errorf("scopeLayoutVerbs holds %d verbs, want %d", len(scopeLayoutVerbs), len(layoutVerbsInOrder))
	}

	d, _, _, _ := localLayoutFixture(t, ProfileLocalOnly)
	standalone, _, _, _ := localLayoutFixture(t, ProfileStandalone)
	for _, verb := range layoutVerbsInOrder {
		if kind, ok := d.layoutKind(verb); !ok || kind != scopeLayout {
			t.Errorf("a local-only daemon gives %q %v, %v; want scopeLayout", verb, kind, ok)
		}
		if kind, ok := standalone.layoutKind(verb); ok || kind != scopeDeny {
			t.Errorf("a standalone daemon gives %q %v, %v; want the refusal", verb, kind, ok)
		}
	}
	// The kind is nobody else's: an admin-holding pane is not a layout caller and
	// a verb outside the set never becomes one.
	for _, verb := range []string{"kill-session", "set-option", "close-workspace", "send-text", "hello"} {
		if _, ok := d.layoutKind(verb); ok {
			t.Errorf("a local-only daemon gives %q a layout kind", verb)
		}
	}
}
