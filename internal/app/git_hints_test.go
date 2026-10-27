package app

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/getan/tuios-mini/internal/config"
	"github.com/getan/tuios-mini/internal/theme"
)

func TestGitHintsFollowFocusAndLiveKeys(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Keybindings.SidebarFiles["git_branches"] = []string{"B"}
	cfg.Keybindings.SidebarFiles["git_tags"] = []string{}
	m := NewOS(OSOptions{UserConfig: cfg, KeybindRegistry: config.NewKeybindRegistry(cfg)})
	m.SidebarFocused, m.projectTab = true, 1
	m.SidebarNav = []sidebarNavRow{{Kind: sidebarRowGitEntry}}
	for _, width := range []int{18, 26, 40} {
		lines := m.gitTabKeyHintLines(width, theme.UI())
		text := ansi.Strip(strings.Join(lines, "\n"))
		for _, want := range []string{"B branches", "l commits", "s stage", "u unstage", "c commit", "G sync", "R repos"} {
			if !strings.Contains(text, want) {
				t.Errorf("width=%d missing %q: %s", width, want, text)
			}
		}
		if strings.Contains(text, "t tags") || strings.Contains(text, "b branches") {
			t.Fatal("hint advertised an unbound/default key instead of the live binding")
		}
		for _, line := range lines {
			if lipgloss.Width(line) != width {
				t.Fatalf("hint width = %d, want %d", lipgloss.Width(line), width)
			}
		}
	}
	m.SidebarFocused = false
	if len(m.gitTabKeyHintLines(26, theme.UI())) != 0 {
		t.Fatal("Git hints appeared while the terminal owned input")
	}
	m.SidebarFocused = true
	m.SidebarNav[0].Kind = sidebarRowAgent
	if len(m.gitTabKeyHintLines(26, theme.UI())) != 0 {
		t.Fatal("Git hints appeared on an agent row")
	}
	m.SidebarNav[0].Kind, m.projectTab = sidebarRowFileEntry, 0
	if len(m.gitTabKeyHintLines(26, theme.UI())) != 0 {
		t.Fatal("Git hints appeared on the Files tab")
	}
}
