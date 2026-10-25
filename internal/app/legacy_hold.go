package app

import tea "charm.land/bubbletea/v2"

// HoldModeAction and these methods keep old callers/config diagnostics source
// compatible. Holding a key no longer creates an alternate input mode.
// Deprecated: use leader commands; focus always owns ordinary input.
const HoldModeAction = "hold_window_mode"

func (m *OS) HoldModeKey() string {
	if m.KeybindRegistry != nil {
		if keys := m.KeybindRegistry.GetKeys(HoldModeAction); len(keys) > 0 {
			return keys[0]
		}
	}
	return ""
}

func (m *OS) HoldModeNeedsAllKeys() bool                            { return false }
func (m *OS) HoldModeAvailable() bool                               { return false }
func (m *OS) HoldModeActive() bool                                  { return false }
func (m *OS) PressHoldKey(tea.KeyPressMsg) bool                     { return false }
func (m *OS) ReleaseHoldKey(tea.KeyPressMsg) bool                   { return false }
func (m *OS) EndHold()                                              {}
func (m *OS) StripHoldModifier(key tea.KeyPressMsg) tea.KeyPressMsg { return key }

func (m *OS) HoldModeUnsupportedReason() string {
	if m.HoldModeKey() != "" {
		return "Hold-to-window mode has been retired. Focus a pane to type; use the leader for workspace commands."
	}
	return ""
}
