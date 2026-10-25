package config

import (
	"slices"
	"testing"
)

func TestFocusInputMigratesOldModeAndDefaultShortcuts(t *testing.T) {
	cfg, err := ParseUserConfig([]byte(`
[keybindings.mode_control]
enter_terminal_mode = ["i", "enter"]
enter_window_mode = ["esc"]
hold_window_mode = ["leftalt"]
[keybindings.terminal_mode]
terminal_exit_mode = ["alt+esc"]
terminal_focus_left = ["alt+left"]
terminal_focus_right = ["ctrl+alt+right"]
[keybindings.global]
command_palette = ["ctrl+p"]
`))
	if err != nil {
		t.Fatal(err)
	}
	r := NewKeybindRegistry(cfg)
	if cfg.Keybindings.InputVersion != 1 || len(cfg.LoadWarnings) == 0 {
		t.Fatal("legacy input migration was not marked and explained")
	}
	for _, key := range []string{"i", "enter", "esc"} {
		if action := r.GetAction(key); action == "enter_terminal_mode" || action == "enter_window_mode" {
			t.Fatalf("legacy mode key %s survived", key)
		}
	}
	if r.GetTerminalModeAction("alt+left") != "" || r.GetTerminalModeAction("alt+esc") != "" || r.GetGlobalAction("ctrl+p") != "" {
		t.Fatal("former default chords still steal guest keys")
	}
	if r.GetTerminalModeAction("ctrl+alt+right") != "terminal_focus_right" {
		t.Fatal("a different custom direct shortcut was lost")
	}
}

func TestFocusInputMigrationRespectsAnExplicitNewKeymap(t *testing.T) {
	cfg, err := ParseUserConfig([]byte(`
[keybindings]
input_version = 1
[keybindings.terminal_mode]
terminal_focus_left = ["alt+left"]
[keybindings.global]
command_palette = ["ctrl+p"]
`))
	if err != nil {
		t.Fatal(err)
	}
	data, err := MarshalUserConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := ParseUserConfig(data)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(loaded.Keybindings.TerminalMode["terminal_focus_left"], []string{"alt+left"}) ||
		!slices.Equal(loaded.Keybindings.Global["command_palette"], []string{"ctrl+p"}) {
		t.Fatal("deliberate direct bindings were migrated away again on reload")
	}
}

func TestFocusInputHelpNeverAdvertisesABareWindowModeKey(t *testing.T) {
	r := NewKeybindRegistry(DefaultConfig())
	presses := PressesByAction(r)
	if slices.Contains(presses["new_window"], "n") || slices.Contains(presses["close_window"], "x") ||
		!slices.Contains(presses["new_window"], "ctrl+b c") {
		t.Fatalf("misleading workspace command help: %+v", presses["new_window"])
	}
	if r.Fate("n", PaneFacts{}).SwallowedInTerminal {
		t.Fatal("plain n is reported as stolen from the focused pane")
	}
}
