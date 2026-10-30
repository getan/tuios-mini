package app

import (
	"testing"

	"github.com/getan/tuios-mini/internal/config"
)

func TestNormalModeLegendShowsTheNextKeys(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewOS(OSOptions{
		Client:          ClientLocal,
		KeybindRegistry: config.NewKeybindRegistry(cfg),
		UserConfig:      cfg,
		Width:           120,
		Height:          40,
	})
	defer m.Cleanup()

	if hints := m.normalModeLegend(); len(hints) == 0 {
		t.Fatal("window mode has no bottom hints")
	}
	m.Mode = TerminalMode
	if hints := m.normalModeLegend(); len(hints) == 0 || hints[0].Key == "" {
		t.Fatal("terminal mode has no leader hint")
	}
	m.SidebarFocused = true
	if hints := m.normalModeLegend(); len(hints) == 0 || hints[0].Key != "enter" {
		t.Fatalf("sidebar legend = %+v, want the enter hint", hints)
	}
	m.PrefixActive = true
	if hints := m.normalModeLegend(); hints != nil {
		t.Fatalf("an armed prefix should leave the legend to which-key: %+v", hints)
	}
}
