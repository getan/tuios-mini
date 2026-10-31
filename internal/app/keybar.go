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
		var submenus, palette, rest []overlay.Hint
		for _, g := range groups {
			for _, b := range g.Bindings {
				h := overlay.Hint{
					Key:   m.printableClientRunes(b.Key),
					Label: m.printableClientRunes(b.Description),
				}
				if b.Submenu {
					h.Label = "+" + h.Label
					submenus = append(submenus, h)
					continue
				}
				if b.Action == "prefix_command_palette" {
					h.Priority = overlay.HintEssential
					palette = append(palette, h)
					continue
				}
				rest = append(rest, h)
			}
		}
		// Zellij's rule: the first layer is the coarse mode list, not every
		// action. A submenu opens the next layer; the command palette is the
		// one global action kept beside those modes. Menus without submenus
		// (Workspace, Window, ...) are already the narrow layer, so they show
		// their own actions.
		if len(submenus) > 0 {
			return label, append(submenus, palette...)
		}
		return label, append(palette, rest...)
	}
	if label, hints, ok := m.keybarPanelHints(); ok {
		return label, hints
	}
	if m.SidebarFocused && m.GitTabActive() {
		return "GIT", nil
	}
	if hints := m.modeLegend(); len(hints) > 0 {
		return "MODE", hints
	}
	if m.InputOverlayOpen() {
		return "PANEL", []overlay.Hint{
			{Key: "esc", Label: "close", Priority: overlay.HintEssential},
		}
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

// keybarPanelHints brings the open panel's own footer into the one keybar.
// The panel keeps its footer for reading next to its content; the keybar is
// the same keys in the same order, so the two cannot drift.
func (m *OS) keybarPanelHints() (string, []overlay.Hint, bool) {
	switch {
	case m.GitDrawerOpen():
		return "GIT", nil, true
	case m.ReviewOpen():
		return "REVIEW", m.reviewKeyHints(), true
	case m.DocPreviewFocused():
		return "PREVIEW", []overlay.Hint{{Key: "esc/ctrl+c", Label: "close", Priority: overlay.HintEssential}}, true
	case m.ShowCommandPalette:
		return "PALETTE", m.paletteFooter(), true
	case m.ShowHelp:
		return "HELP", helpHints(m.HelpSearchMode), true
	case m.ShowSettings:
		return "SETTINGS", m.currentSettingsHints(), true
	case m.FilePromptOpen():
		return "DIALOG", []overlay.Hint{
			{Key: overlay.EnterKey(), Label: m.filePromptSubmitLabel(), Priority: overlay.HintEssential},
			{Key: "esc", Label: "cancel", Priority: overlay.HintEssential},
		}, true
	case m.Renaming():
		return "RENAME", []overlay.Hint{
			{Key: overlay.EnterKey(), Label: "save", Priority: overlay.HintEssential},
			{Key: "esc", Label: "cancel", Priority: overlay.HintEssential},
		}, true
	case m.CaptureActive():
		return "CAPTURE", []overlay.Hint{
			{Key: overlay.EnterKey(), Label: "copy", Priority: overlay.HintEssential},
			{Key: "esc", Label: "cancel", Priority: overlay.HintEssential},
		}, true
	case m.ShowScrollbackBrowser:
		return "SCROLLBACK", []overlay.Hint{
			{Key: "esc", Label: "close", Priority: overlay.HintEssential},
		}, true
	case m.ShowTapeManager:
		return "TAPES", []overlay.Hint{
			{Key: "esc", Label: "close", Priority: overlay.HintEssential},
		}, true
	case m.ShowTapeReview:
		return "TAPE REVIEW", []overlay.Hint{
			{Key: "esc", Label: "close", Priority: overlay.HintEssential},
		}, true
	case m.ShowCacheStats:
		return "CACHE", []overlay.Hint{
			{Key: "esc", Label: "close", Priority: overlay.HintEssential},
		}, true
	case m.ShowThemePicker:
		return "THEME", []overlay.Hint{
			{Key: overlay.EnterKey(), Label: "apply"},
			{Key: "esc", Label: "close", Priority: overlay.HintEssential},
		}, true
	case m.ShowLayoutPicker:
		return "LAYOUT", []overlay.Hint{
			{Key: overlay.EnterKey(), Label: "apply"},
			{Key: "esc", Label: "close", Priority: overlay.HintEssential},
		}, true
	case m.ShowHostPicker:
		return "HOSTS", []overlay.Hint{
			{Key: overlay.EnterKey(), Label: "open"},
			{Key: "esc", Label: "cancel", Priority: overlay.HintEssential},
		}, true
	case m.ShowGlyphPicker:
		return "GLYPHS", []overlay.Hint{
			{Key: overlay.EnterKey(), Label: "apply"},
			{Key: "esc", Label: "cancel", Priority: overlay.HintEssential},
		}, true
	case m.ShowEffectPicker:
		return "EFFECTS", []overlay.Hint{
			{Key: overlay.EnterKey(), Label: "apply"},
			{Key: "esc", Label: "cancel", Priority: overlay.HintEssential},
		}, true
	case m.ShowAccentPicker:
		return "ACCENT", []overlay.Hint{
			{Key: overlay.EnterKey(), Label: "choose"},
			{Key: "esc", Label: "close", Priority: overlay.HintEssential},
		}, true
	case m.ShowDockEditor:
		return "DOCK", []overlay.Hint{
			{Key: "esc", Label: "close", Priority: overlay.HintEssential},
		}, true
	case m.ShowSectionEditor:
		return "SIDEBAR", []overlay.Hint{
			{Key: "esc", Label: "close", Priority: overlay.HintEssential},
		}, true
	case m.ShowKeybindManager:
		return "KEYS", []overlay.Hint{
			{Key: overlay.EnterKey(), Label: "edit"},
			{Key: "esc", Label: "close", Priority: overlay.HintEssential},
		}, true
	case m.ShowInbox:
		return "INBOX", []overlay.Hint{
			{Key: overlay.EnterKey(), Label: "open"},
			{Key: "esc", Label: "close", Priority: overlay.HintEssential},
		}, true
	case m.ShowAgentMail:
		return "MAIL", []overlay.Hint{
			{Key: overlay.EnterKey(), Label: "open"},
			{Key: "esc", Label: "close", Priority: overlay.HintEssential},
		}, true
	case m.ShowAggregateView:
		return "PANES", []overlay.Hint{
			{Key: overlay.EnterKey(), Label: "focus"},
			{Key: "esc", Label: "close", Priority: overlay.HintEssential},
		}, true
	case m.ShowLogs:
		return "LOGS", []overlay.Hint{
			{Key: "q", Label: "close", Priority: overlay.HintEssential},
			{Key: "esc", Label: "close", Priority: overlay.HintEssential},
		}, true
	case m.ShowLauncher:
		return "LAUNCHER", []overlay.Hint{
			{Key: overlay.EnterKey(), Label: "run"},
			{Key: "tab", Label: "type"},
			{Key: "esc", Label: "close"},
		}, true
	case m.ShowWorkspaceSwitcher:
		return "WORKSPACES", []overlay.Hint{
			{Key: overlay.EnterKey(), Label: "open"},
			{Key: "esc", Label: "close"},
		}, true
	case m.ShowSessionSwitcher:
		return "SESSIONS", []overlay.Hint{
			{Key: overlay.EnterKey(), Label: "open"},
			{Key: "esc", Label: "close"},
		}, true
	case m.ShowQuitMenu:
		return "QUIT", []overlay.Hint{
			{Key: "q", Label: "quit"},
			{Key: "esc", Label: "cancel"},
		}, true
	case m.ShowSessionClose:
		return "CLOSE", []overlay.Hint{
			{Key: "y", Label: "close", Priority: overlay.HintEssential},
			{Key: "esc", Label: "cancel", Priority: overlay.HintEssential},
		}, true
	case m.ContextMenuActive():
		return "MENU", []overlay.Hint{
			{Key: overlay.EnterKey(), Label: "choose"},
			{Key: "esc", Label: "close"},
		}, true
	}
	return "", nil, false
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
