package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/getan/tuios-mini/internal/config"
)

// The async probe must preserve ordinary/symlinked dotfiles, but remain
// advisory: neither a result nor a late result applies a keybind preset.
func TestPresetProbePreservesConfigAndNeverAppliesKeys(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "dotfiles", "tmux.conf")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("set -g prefix C-b\nset -g prefix C-a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(configPath, filepath.Join(home, ".tmux.conf")); err != nil {
		t.Skip("symlinks unavailable")
	}
	t.Setenv("HOME", home)
	m := &OS{Settings: config.Global, Client: ClientLocal, UserConfig: config.DefaultConfig()}
	before := m.UserConfig.Keybindings.Preset
	cmd := m.presetSuggestionCmd()
	if cmd == nil {
		t.Fatal("local client's async suggestion was not scheduled")
	}
	if hint := m.presetSuggestion(); hint.label != "pick keys" || hint.preset != "" {
		t.Fatalf("UI did synchronous I/O instead of showing placeholder: %+v", hint)
	}
	msg, ok := cmd().(presetSuggestionMsg)
	if !ok || msg.suggestion.preset != "tmux" || msg.suggestion.leader != "ctrl+a" {
		t.Fatalf("symlinked tmux leader was lost: %+v", msg)
	}
	m.handleMsg(msg)
	if hint := m.presetSuggestion(); hint.preset != "tmux" || hint.leader != "ctrl+a" {
		t.Fatalf("async hint was not adopted: %+v", hint)
	}
	if m.UserConfig.Keybindings.Preset != before {
		t.Fatal("a suggestion applied keybindings without user choice")
	}
	if m.presetSuggestionCmd() != nil {
		t.Fatal("hint scheduled repeatedly")
	}
	for _, kind := range []ClientKind{ClientUnknown, ClientSSH, ClientBrowser} {
		remote := &OS{Settings: config.Global, Client: kind, UserConfig: config.DefaultConfig()}
		if remote.presetSuggestionCmd() != nil {
			t.Errorf("%v probed the server operator's home", kind)
		}
	}
}
