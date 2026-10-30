package app

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/getan/tuios-mini/internal/config"
	"github.com/getan/tuios-mini/internal/overlay"
	"github.com/getan/tuios-mini/internal/theme"
)

const keyBarHeight = 1

// keybarLine is the fixed bottom hint row. It shares the keybinding registry
// and prefix state with which-key, but draws only the current layer in the
// quieter dock palette instead of opening a floating panel.
func (m *OS) keybarLine() string {
	label, hints := m.keybarHints()
	pal := theme.UI()
	bg := pal.Panel
	labelStyle := overlay.Style(bg).Foreground(pal.FgMute)
	labelText := labelStyle.Render(label)
	gap := overlay.Style(bg).Foreground(pal.FgMute).Render("  ")
	hintPal := pal
	// Ambient help, not a mode that owns the keyboard: keys sit at FgDim and
	// descriptions at FgMute. The mode pill and notifications stay louder.
	hintPal.AccentBright = pal.FgDim
	hintPal.FgDim = pal.FgMute
	room := max(m.GetRenderWidth()-lipgloss.Width(labelText)-lipgloss.Width(gap), 0)
	strip, _ := overlay.FitHintStrip(hints, room, bg, hintPal)
	line := labelText + gap + strip
	if width := lipgloss.Width(line); width < m.GetRenderWidth() {
		// Give the padding an explicit foreground as well as the background.
		// The compositor otherwise lets the pane's ink show through those
		// spaces while the fast path paints them with no ink at all, and the
		// two frames disagree on an otherwise blank cell.
		line += lipgloss.NewStyle().
			Background(bg).
			Foreground(pal.FgMute).
			Render(strings.Repeat(" ", m.GetRenderWidth()-width))
	}
	return line
}

// keybarHints returns the label and the layer-specific keys. A prefix replaces
// the normal hints with only the current menu's direct entries, so pressing a
// key changes the line rather than opening a second UI.
func (m *OS) keybarHints() (string, []overlay.Hint) {
	if m.prefixArmed() {
		title, groups := m.whichKeyMenu()
		label := m.printableClientRunes(strings.ToUpper(title))
		if label == "PREFIX" {
			label = "PREFIX"
		}
		var preferred, all []overlay.Hint
		for _, g := range groups {
			for _, b := range g.Bindings {
				h := overlay.Hint{
					Key:   m.printableClientRunes(b.Key),
					Label: m.printableClientRunes(b.Description),
				}
				all = append(all, h)
				if b.Submenu {
					h.Label = "+" + h.Label
					preferred = append(preferred, h)
				}
			}
		}
		if len(preferred) > 0 {
			return label, preferred
		}
		return label, all
	}
	if m.FocusedPaneOwnsInput() {
		return "INPUT", []overlay.Hint{{
			Key:      m.keybarLeaderKey(),
			Label:    "menu",
			Priority: overlay.HintEssential,
		}}
	}
	label := "KEYS"
	if m.SidebarFocused {
		label = "SIDEBAR"
	}
	return label, m.normalModeLegend()
}

func (m *OS) keybarLeaderKey() string {
	if m.KeybindRegistry != nil {
		if cfg := m.KeybindRegistry.GetConfig(); cfg != nil && cfg.Keybindings.LeaderKey != "" {
			return cfg.Keybindings.LeaderKey
		}
	}
	return config.DefaultLeaderKey
}

func (m *OS) renderKeybar() *lipgloss.Layer {
	return lipgloss.NewLayer(m.keybarLine()).
		X(0).Y(m.GetRenderHeight() - keyBarHeight).
		Z(config.ZIndexDock).ID("keybar")
}
