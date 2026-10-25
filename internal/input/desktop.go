package input

import (
	tea "charm.land/bubbletea/v2"
	"github.com/getan/tuios-mini/internal/app"
)

// handleDesktopKey offers only the actions printed on the empty desktop. It
// is not a window-management mode: there is no pane to receive input, and a
// bare q/x/h/j/k/l must never close or manipulate a hidden pane.
func handleDesktopKey(msg tea.KeyPressMsg, o *app.OS) (*app.OS, tea.Cmd) {
	switch commandKey(msg) {
	case "n":
		return GetDispatcher().Dispatch("new_window", msg, o)
	case "N":
		return GetDispatcher().Dispatch("new_session", msg, o)
	case "?":
		return GetDispatcher().Dispatch("toggle_help", msg, o)
	case ",":
		return GetDispatcher().Dispatch("open_settings", msg, o)
	case "U":
		return GetDispatcher().Dispatch("open_keybind_presets", msg, o)
	}
	return o, nil
}
