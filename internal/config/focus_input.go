package config

import "slices"

// migrateFocusInput keeps old TOML readable without restoring the dual-mode
// input model. The old window-management tables are still addressable by the
// palette and explicit commands; they are never overlaid on pane typing.
func migrateFocusInput(cfg *UserConfig) {
	deprecated := false
	for _, table := range []map[string][]string{cfg.Keybindings.ModeControl, cfg.Keybindings.TerminalMode} {
		for _, action := range []string{"enter_terminal_mode", "enter_window_mode", "terminal_exit_mode", "hold_window_mode"} {
			if len(table[action]) != 0 {
				deprecated = true
			}
			delete(table, action)
		}
	}
	if deprecated {
		const warning = "Focus now owns input: enter/exit/hold-window-mode bindings are ignored. Use the leader for workspace commands; plain Esc, Enter and letters belong to the pane."
		if !slices.Contains(cfg.LoadWarnings, warning) {
			cfg.LoadWarnings = append(cfg.LoadWarnings, warning)
		}
	}
	if cfg.Keybindings.InputVersion >= 1 {
		return
	}
	cfg.Keybindings.InputVersion = 1
	// Release only the former default chords. A deliberately different custom
	// shortcut remains an explicit host binding. Empty slices prevent defaults
	// or presets from filling an obsolete shortcut back in.
	for action, old := range map[string][]string{
		"terminal_next_window": {"opt+tab", "alt+n"},
		"terminal_prev_window": {"opt+shift+tab", "alt+p"},
		"terminal_focus_left":  {"alt+left"}, "terminal_focus_right": {"alt+right"},
		"terminal_focus_up": {"alt+up"}, "terminal_focus_down": {"alt+down"},
	} {
		keys := cfg.Keybindings.TerminalMode[action]
		if slices.Equal(keys, old) ||
			(action == "terminal_next_window" && slices.Equal(keys, []string{"alt+n"})) ||
			(action == "terminal_prev_window" && slices.Equal(keys, []string{"alt+p"})) {
			cfg.Keybindings.TerminalMode[action] = []string{}
		}
	}
	for action, old := range map[string][]string{"command_palette": {"ctrl+p"}, "launcher": {"alt+space"}} {
		if slices.Equal(cfg.Keybindings.Global[action], old) {
			cfg.Keybindings.Global[action] = []string{}
		}
	}
}
