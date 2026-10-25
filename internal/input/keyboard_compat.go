package input

import (
	tea "charm.land/bubbletea/v2"
	"github.com/getan/tuios-mini/internal/app"
)

// remoteKeyBypassesCopyMode distinguishes agent/tape input from a person's
// scrollback viewport. Remote typing must not move that viewport.
func remoteKeyBypassesCopyMode(o *app.OS) bool { return o.ProcessingRemoteKeys }

// HandleWindowManagementModeKey is a source-compatible alias, not an alternate
// input model. Old callers get the same focus-based routing as every client.
// Deprecated: use HandleKeyPress.
func HandleWindowManagementModeKey(msg tea.KeyPressMsg, o *app.OS) (*app.OS, tea.Cmd) {
	return HandleKeyPress(msg, o)
}
