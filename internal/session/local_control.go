package session

import "encoding/json"

// Local control: the verbs a workspace's own panes need to arrange themselves.
//
// A golder workspace (mini, ProfileLocalOnly) starts its panes on the strict
// default of [agents.permissions]: read, write and fan. Those grants reach every
// verb that reads or types into the pane's own session, but the verbs that
// *arrange* a session — new-window, close-window, focus-window, move-window,
// set-window and resize — are scopeDeny in verbScopes, so a pane without admin
// is refused them (checkGrants) and a connection that restricted itself is
// refused them (checkScope). The workspace could therefore never open a second
// pane, rename one, focus one, move one to another workspace, resize one or
// close one, although each of those acts only on the session the caller is
// already in and the write grant already lets it type into that session's panes.
//
// The verbs are not moved out of scopeDeny. That table is the standalone
// product's policy, for panes and for restricted connections both, and widening
// it would widen the standalone daemon with it. This file admits the six verbs
// for a local-only daemon only (d.localOnly()), as scopeLayout: an act on the
// panes of the caller's own session, which needs the write grant and is refused
// to a read-only caller. Both layers ask here — checkGrants for a pane's grants
// and checkScope for a restricted connection — so their answers cannot drift
// apart.
//
// What is out of reach, and what local_control_test.go holds:
//
//   - another session. A named session must be the caller's own, and the fan
//     group that read and fan reach is not this kind's reach. move-window's
//     to_session is refused outright: it is the one parameter holdToPane does
//     not see, and it moves a window out of the session the caller may arrange.
//   - another machine. A window a call would start elsewhere (new-window's
//     host) is refused by holdToPane's host rule.
//   - a wider pane. new-window's grants go through launchGrants, so a new pane
//     holds no more than the pane that started it.
//   - a pane that holds more than the caller. close-window refuses to close one,
//     as typing refuses to type into one: what the caller cannot reach it cannot
//     end either. A pane may always close itself.
//   - everything else admin reaches: kill-session, the options, the listings
//     across sessions, the file, transfer and link verbs, the client protocol.
//     Those stay scopeDeny, and scopeLayout changes none of them.

// scopeLayoutVerbs are the verbs a local-only workspace's panes arrange their
// own session with. Each acts on one session's window set and nothing else: it
// opens, closes, renames, focuses, moves and sizes a pane there.
var scopeLayoutVerbs = map[string]bool{
	"new-window":   true,
	"close-window": true,
	"focus-window": true,
	"move-window":  true,
	"set-window":   true,
	"resize":       true,
}

// layoutKind is the kind verb takes on this daemon, and whether it takes one:
// true only for one of scopeLayoutVerbs on a local-only daemon. On the
// standalone product, and for every other verb, the answer is scopeDeny and the
// caller refuses the verb as it did before.
func (d *Daemon) layoutKind(verb string) (scopeKind, bool) {
	if !d.localOnly() || !scopeLayoutVerbs[verb] {
		return scopeDeny, false
	}
	return scopeLayout, true
}

// layoutReach holds a layout verb to the session own and no other. The fan group
// that read and fan reach is not this kind's reach, and neither is a session the
// caller launched: arranging panes is what the write grant already lets a pane
// do in the session it is in.
func (d *Daemon) layoutReach(own string) func(target string) string {
	return func(target string) string {
		if d.sessionIsOwn(own, target) {
			return ""
		}
		return "session " + echoName(target) + " is not the caller's own session, and arranging another session's panes needs the admin grant"
	}
}

// layoutParamsWhy says why a layout verb's parameters reach past the caller's
// own session, and "" when they stay inside it. holdToPane already refuses a
// session, a host, a selector and the whole-table parameters that name more than
// the caller's own; to_session is the one parameter it does not read, because it
// belongs to this verb alone.
func layoutParamsWhy(verb string, params json.RawMessage) string {
	if verb != "move-window" || len(params) == 0 {
		return ""
	}
	var p struct {
		ToSession string `json:"to_session"`
	}
	if json.Unmarshal(params, &p) != nil || p.ToSession == "" {
		return ""
	}
	return "it moves the window into session " + echoName(p.ToSession) + " with to_session, and a pane without the admin grant arranges only its own session's panes"
}

// closeRefusal says why a pane may not close the window a close-window call
// names, and "" when it may. params is the call as holdToPane left it, with the
// caller's own session filled in.
//
// The rule is the one typing has (typingRefusal): a pane reaches only panes that
// hold nothing it does not. Closing hands the caller nothing, but it ends the
// work in that pane, and a pane that holds read and write has no business ending
// a pane that holds admin. A pane may always close itself: it could end its own
// process anyway.
//
// A window the call names that does not resolve is left to the handler, which
// answers it with the window-not-found error a caller expects.
func (d *Daemon) closeRefusal(pa *paneAuth, params json.RawMessage) string {
	var p struct {
		Session string `json:"session"`
		Window  string `json:"window"`
	}
	if len(params) > 0 && json.Unmarshal(params, &p) != nil {
		return ""
	}
	sess := d.findTargetSession(p.Session)
	if sess == nil {
		return ""
	}
	state := sess.GetState()
	id := p.Window
	if id == "" {
		focused, err := focusedWindowID(state)
		if err != nil {
			return ""
		}
		id = focused
	}
	idx, err := findWindowStateIndex(state.Windows, id)
	if err != nil {
		return ""
	}
	win := state.Windows[idx]
	if win.ID == pa.window {
		return ""
	}
	held, _ := d.manager.grants.effective(win.ID)
	if pa.grants.Covers(held) {
		return ""
	}
	return "window " + shortWindowID(win.ID) + " holds " + held.String() + ", more than this pane holds, and what is in it runs with that. " +
		"A pane closes only panes that hold nothing it does not"
}
