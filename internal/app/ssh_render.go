package app

import (
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/getan/tuios-mini/internal/config"
)

const sshRenderFPS = 10

func sshTerminalEnvironment() bool {
	return os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != ""
}

// A local process reached through OpenSSH is not ClientSSH. Browser clients
// must never inherit the environment of the server operator's login shell.
func reducedSSHClient(opts OSOptions) bool {
	if opts.Client == ClientBrowser || opts.BrowserClient {
		return false
	}
	if opts.Client == ClientLocal {
		switch strings.ToLower(strings.TrimSpace(os.Getenv("TUIOS_SSH_FULL_UI"))) {
		case "1", "true", "yes", "on":
			return false
		}
		return sshTerminalEnvironment()
	}
	return opts.Client == ClientSSH || opts.IsSSHMode
}

// These overrides belong to this client only. They neither save configuration
// nor alter the terminal bytes/colours produced by guest applications.
func (m *OS) applyReducedRendering() {
	if !m.reducedRendering {
		return
	}
	s := &m.Settings
	if s.NormalFPS <= 0 || s.NormalFPS > sshRenderFPS {
		s.NormalFPS = sshRenderFPS
	}
	s.MaxFPSAuto = false
	s.Motion, s.NoAnimationsFlag = config.MotionNone, true
	s.UseASCIIOnly, s.ASCIIRequested = true, true
	s.ModalDim = 0
	s.BorderStyle, s.BorderFocusedGradient = "ascii", ""
	s.HideWindowButtons, s.DockCompact = true, true
	s.DockPillCaps = false
	s.HideClock = true
	s.ShowClock, s.ShowCPU, s.ShowRAM = false, false, false
	// Keep the rail's one-column marks, but only as the ASCII fallbacks below
	// them: file icons are off, and UseASCIIOnly makes the folder, file and
	// terminal glyphs the text set. Turning the glyph column off entirely would
	// hide the only visible mark a file row has.
	s.SidebarMarquee, s.SidebarShowGlyphs = false, true
	s.SidebarFileIcons, s.SidebarFileIconColors = false, false
	s.FocusFollowsMouse, s.ZoomAnimation = false, false
	m.spotlight.on = false
}

func reducedDockPlan() dockPlan {
	p := dockPlan{
		Left:   []string{config.DockComponentMode, config.DockComponentWorkspaces},
		Center: []string{config.DockComponentWindows},
		Right:  []string{config.DockComponentSessionControls, config.DockComponentSession, config.DockComponentCopyHelp, config.DockComponentTape, config.DockComponentNotifications},
		placed: make(map[string]bool),
	}
	for _, list := range [][]string{p.Left, p.Center, p.Right} {
		for _, name := range list {
			p.placed[name] = true
		}
	}
	return p
}

func (m *OS) reducedWelcome() *lipgloss.Layer {
	w, h := m.ViewContentWidth(), m.ViewUsableHeight()
	lines := []string{"AGENT WORKSPACE", "n new window | N new session", "? help | , settings"}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], max(w-2, 1), "")
	}
	content := strings.Join(lines[:min(len(lines), h)], "\n")
	r := m.viewReserve()
	return lipgloss.NewLayer(content).X(r.Left + 1).Y(r.Top).Z(1).ID("welcome")
}

// Preserve application-requested 1003 motion (editors/games) while no longer
// asking for a network round trip on every cosmetic chrome hover.
func (m *OS) renderMouseMode() tea.MouseMode {
	if m.MouseOff {
		return tea.MouseModeNone
	}
	if !m.reducedRendering {
		return tea.MouseModeAllMotion
	}
	if w := m.KeyboardPane(); w != nil {
		w.LockIO()
		all := w.Terminal != nil && w.Terminal.HasAllMotionMode()
		w.UnlockIO()
		if all {
			return tea.MouseModeAllMotion
		}
	}
	return tea.MouseModeCellMotion
}
