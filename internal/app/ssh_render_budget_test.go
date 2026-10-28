package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/getan/tuios-mini/internal/config"
	"github.com/getan/tuios-mini/internal/theme"
)

// Perf/transport budgets missed by the existing narrow-splash and fast-PTY
// tests. See docs/SSH_REDUCED_RENDERING.md for the pre-implementation inventory.
func TestSSHRenderingBudgetAndClientIsolation(t *testing.T) {
	t.Cleanup(func() { syncOverlayASCII(&config.Global) })
	t.Setenv("SSH_CONNECTION", "fixture")
	t.Setenv("SSH_TTY", "/dev/pts/fixture")
	t.Setenv("TUIOS_SSH_FULL_UI", "")
	for _, kind := range []ClientKind{ClientLocal, ClientSSH, ClientBrowser} {
		t.Run(kind.String(), func(t *testing.T) {
			seed := config.Global
			seed.NormalFPS = 60
			seed.Motion = config.MotionFull
			seed.NoAnimationsFlag = false
			seed.AnimationsSuppressed = false
			seed.ShowClock, seed.ShowCPU, seed.ShowRAM = true, true, true
			m := NewOS(OSOptions{Client: kind, Settings: &seed, UserConfig: &config.UserConfig{}, Caps: &HostCapabilities{Term: "xterm-256color"}, Width: 120, Height: 40})
			defer m.Cleanup()
			if kind == ClientBrowser {
				if m.framePeriod() != time.Second/60 || m.Settings.NoAnimationsFlag {
					t.Fatal("a browser inherited its server's SSH profile")
				}
				return
			}
			if m.framePeriod() < 100*time.Millisecond || !m.Settings.NoAnimationsFlag || !m.Settings.UseASCIIOnly {
				t.Errorf("SSH rendering is not bounded/reduced: fps=%d motion_off=%t ascii=%t", m.Settings.NormalFPS, m.Settings.NoAnimationsFlag, m.Settings.UseASCIIOnly)
			}
			if m.Settings.ShowClock || m.Settings.ShowCPU || m.Settings.ShowRAM {
				t.Error("cosmetic dock meters remain enabled")
			}
			syncOverlayASCII(&m.Settings)
			for _, file := range []struct {
				name string
				dir  bool
			}{{"src", true}, {"main.go", false}, {"script.py", false}, {"config.json", false}, {"中文.txt", false}} {
				mark := fileRowMark(fileIconFor(file.name, file.dir), file.dir, false, &m.Settings)
				if mark.Hex != "" || mark.Glyph == " " || strings.IndexFunc(mark.Glyph, func(r rune) bool { return r > 127 }) >= 0 {
					t.Errorf("SSH file mark is not a visible, uncoloured ASCII fallback for %s", file.name)
				}
			}
			motion := tea.MouseMotionMsg{X: 1, Y: 1, Button: tea.MouseNone}
			if FilterMouseMotion(m, motion) != nil {
				t.Error("cosmetic SSH mouse hover still reaches Update")
			}
			if cmd := m.InitDockComponents(); cmd != nil {
				t.Error("cosmetic SSH dock scheduling remains active")
			}
			for _, layer := range m.renderOverlays() {
				if layer.GetID() == "welcome" {
					body := layer.GetContent()
					if strings.ContainsAny(body, "█╔╗╚╝") || len(body) > 512 {
						t.Errorf("SSH welcome still contains a large decorative frame: bytes=%d", len(body))
					}
					if !strings.Contains(body, "AGENT WORKSPACE") || !strings.Contains(body, "new window") || !strings.Contains(body, "help") {
						t.Error("SSH welcome lost core navigation hints")
					}
				}
			}
			if view := m.View(); view.MouseMode != tea.MouseModeCellMotion {
				t.Errorf("idle SSH chrome still requests all-motion reporting: %v", view.MouseMode)
			}
			if seed.NormalFPS != 60 || seed.NoAnimationsFlag || !seed.ShowClock {
				t.Error("client policy mutated its caller's appearance seed")
			}
		})
	}
}

// ASCII transport chrome must not turn a real filename into a different name,
// or discard what the user types into a create/rename field.
func TestSSHRenderingKeepsUnicodeFileNames(t *testing.T) {
	t.Cleanup(func() { syncOverlayASCII(&config.Global) })
	m := NewOS(OSOptions{Client: ClientSSH, UserConfig: &config.UserConfig{}, Width: 120, Height: 40})
	defer m.Cleanup()
	syncOverlayASCII(&m.Settings)
	const name = "中文-café.txt"
	row := m.sidebarFileRow(fileRowSpec{Name: name, Kind: sidebarRowFileEntry}, 40, theme.UI(), sidebarRowState{})
	if !strings.Contains(row, name) {
		t.Errorf("ASCII chrome hid part of the filename: %q", row)
	}
	m.filePrompt.Kind = filePromptCreate
	m.FilePromptType(name)
	if m.filePrompt.Input != name {
		t.Errorf("ASCII chrome changed typed filename to %q", m.filePrompt.Input)
	}
	m.BeginRenameSession("work")
	m.RenameBuffer = ""
	m.RenameAppend(name)
	if m.RenameBuffer != name {
		t.Errorf("ASCII chrome changed typed session name to %q", m.RenameBuffer)
	}
}
