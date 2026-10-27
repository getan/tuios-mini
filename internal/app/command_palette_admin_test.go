package app

import (
	"testing"

	"github.com/getan/tuios-mini/internal/config"
)

// TestPaletteHasRunnableAdminToggle: the keybind rows in the palette open the
// keybind manager, so an admin row there would never toggle anything. The
// admin command is a normal runnable row instead, and it exists even when the
// loaded config predates the action's default key.
func TestPaletteHasRunnableAdminToggle(t *testing.T) {
	m := NewOS(OSOptions{UserConfig: config.DefaultConfig()})
	for _, it := range GetCommandPaletteItems(&m.Settings) {
		if it.Name != "Toggle admin for the focused pane" {
			continue
		}
		if it.Keybind {
			t.Fatal("the admin row opens the keybind manager instead of toggling")
		}
		if it.Bind != "prefix_toggle_pane_admin" || it.Action == nil {
			t.Fatalf("admin row = %+v", it)
		}
		return
	}
	t.Fatal("the palette has no runnable Toggle admin row")
}
