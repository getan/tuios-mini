package app

import (
	"encoding/json"
	"time"

	tea "charm.land/bubbletea/v2"
)

// PaneAdminMsg reports one toggle of the focused pane's admin grant.
type PaneAdminMsg struct {
	Window string
	On     bool
	Err    error
}

// ToggleFocusedPaneAdmin flips the focused pane's admin grant. It is a client
// verb action, not a pane one: the person's client is outside every pane, so
// the daemon lets it grant admin. A process inside a pane cannot (tuios refuses
// a pane that gives more than it holds), which is why this cannot be a pane's
// own command.
//
// The starting point is the pane's explicit grants when it has them, and the
// daemon's default otherwise. Dropping admin keeps every other grant, so a pane
// that also held files or respond does not lose them.
func (m *OS) ToggleFocusedPaneAdmin() tea.Cmd {
	w := m.GetFocusedWindow()
	if w == nil {
		m.ShowNotification("Focus a pane first.", "info", m.Settings.NotificationDuration)
		return nil
	}
	session, window := m.SessionName, w.ID
	call := m.inboxCaller()
	return func() tea.Msg {
		current := []string{"read", "write", "fan"}
		if raw, err := call("pane-grants", nil, 5*time.Second); err == nil {
			var res struct {
				DefaultGrants []string `json:"default_grants"`
			}
			if json.Unmarshal(raw, &res) == nil && len(res.DefaultGrants) > 0 {
				current = res.DefaultGrants
			}
		}
		if raw, err := call("list-windows", map[string]any{"session": session}, 5*time.Second); err == nil {
			var res struct {
				Windows []struct {
					WindowID string   `json:"window_id"`
					Grants   []string `json:"grants"`
				} `json:"windows"`
			}
			if json.Unmarshal(raw, &res) == nil {
				for _, win := range res.Windows {
					if win.WindowID == window && win.Grants != nil {
						current = win.Grants
						break
					}
				}
			}
		}
		if len(current) == 1 && current[0] == "none" {
			current = nil
		}
		on := true
		for _, g := range current {
			if g == "admin" {
				on = false
				break
			}
		}
		next := make([]string, 0, len(current)+1)
		for _, g := range current {
			if g != "admin" {
				next = append(next, g)
			}
		}
		if on {
			next = append(next, "admin")
		}
		if len(next) == 0 {
			next = []string{"none"}
		}
		if _, err := call("set-pane-grants", map[string]any{
			"session": session, "window": window, "grants": next,
		}, 10*time.Second); err != nil {
			return PaneAdminMsg{Window: window, Err: err}
		}
		return PaneAdminMsg{Window: window, On: on}
	}
}

// HandlePaneAdmin says what the toggle did.
func (m *OS) HandlePaneAdmin(msg PaneAdminMsg) {
	switch {
	case msg.Err != nil:
		m.ShowNotification("Pane admin: "+msg.Err.Error(), "error", m.Settings.NotificationDuration)
	case msg.On:
		m.ShowNotification("Admin on for pane "+shortWindowLabel(msg.Window)+": it may open windows outside its folder.", "success", m.Settings.NotificationDuration)
	default:
		m.ShowNotification("Admin off for pane "+shortWindowLabel(msg.Window)+".", "info", m.Settings.NotificationDuration)
	}
}

