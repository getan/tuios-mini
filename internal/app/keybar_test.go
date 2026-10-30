package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/getan/tuios-mini/internal/config"
	"github.com/getan/tuios-mini/internal/terminal"
)

func keybarTestOS(t *testing.T, client ClientKind) *OS {
	t.Helper()
	cfg := config.DefaultConfig()
	m := NewOS(OSOptions{
		Client:          client,
		KeybindRegistry: config.NewKeybindRegistry(cfg),
		UserConfig:      cfg,
		Caps:            &HostCapabilities{Term: "xterm-256color"},
		Width:           100,
		Height:          30,
	})
	t.Cleanup(m.Cleanup)
	return m
}

func TestKeybarReservesTheBottomRow(t *testing.T) {
	m := keybarTestOS(t, ClientLocal)
	m.Settings.DockbarPosition = "top"
	if got := m.GetBottomMargin(); got != keyBarHeight {
		t.Fatalf("bottom margin = %d, want the keybar row %d", got, keyBarHeight)
	}
	if y := m.renderKeybar().GetY(); y != m.GetRenderHeight()-keyBarHeight {
		t.Fatalf("keybar y = %d, want %d", y, m.GetRenderHeight()-keyBarHeight)
	}
}

func TestKeybarChangesWithThePrefixLayer(t *testing.T) {
	m := keybarTestOS(t, ClientLocal)
	m.PrefixActive = true
	m.WorkspacePrefixActive = true
	line := ansi.Strip(m.keybarLine())
	if !strings.Contains(line, "WORKSPACE") {
		t.Fatalf("workspace keybar lost its layer label: %q", line)
	}
	if !strings.Contains(strings.ToLower(line), "switch") {
		t.Fatalf("workspace keybar lost its keys: %q", line)
	}
}

func TestKeybarShowsPrefixHelpWhenThePaneOwnsInput(t *testing.T) {
	m := keybarTestOS(t, ClientLocal)
	w := newTestWindow(t, "agent-input", 80, 20)
	w.Workspace = 1
	m.Windows = []*terminal.Window{w}
	m.CurrentWorkspace, m.FocusedWindow = 1, 0
	line := ansi.Strip(m.keybarLine())
	if !strings.Contains(line, "INPUT") || !strings.Contains(line, "ctrl+b") {
		t.Fatalf("a focused pane should point at the prefix key, got %q", line)
	}
	if strings.Contains(line, "split right") {
		t.Fatalf("pane input still advertised window management: %q", line)
	}
}

func TestSSHKeybarStaysASCII(t *testing.T) {
	m := keybarTestOS(t, ClientSSH)
	line := ansi.Strip(m.keybarLine())
	if strings.IndexFunc(line, func(r rune) bool { return r > 127 }) >= 0 {
		t.Fatalf("SSH keybar is not ASCII: %q", line)
	}
}
