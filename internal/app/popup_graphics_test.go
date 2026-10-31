package app

import (
	"testing"
	"time"

	"github.com/getan/tuios-mini/internal/config"
	"github.com/getan/tuios-mini/internal/review"
)

// Check the real View graphics wire for every opaque popup, including the
// non-draggable rename editor and menus. A second hand-maintained list in
// flushGraphicsForView used to omit all of those and several panel kinds.
// Each case must hide once, stay hidden, and restore without a new image.
func TestOpaquePopupsProtectAgainstBrowserGraphics(t *testing.T) {
	type popupCase struct {
		name string
		open func(*OS)
	}
	panels := []popupCase{
		{"screenshot", func(m *OS) { m.ShotPreview.Open = true }},
		{"help", func(m *OS) { m.ShowHelp = true }},
		{"palette", func(m *OS) { m.ShowCommandPalette = true }},
		{"launcher", func(m *OS) { m.ShowLauncher = true }},
		{"session", func(m *OS) { m.ShowSessionSwitcher = true }},
		{"agentmail", func(m *OS) { m.ShowAgentMail = true }},
		{"inbox", func(m *OS) { m.ShowInbox = true }},
		{"workspace", func(m *OS) { m.ShowWorkspaceSwitcher = true }},
		{overlayKindBuffers, func(m *OS) { m.buffers.open = true }},
		{overlayKindTransfers, func(m *OS) { m.transfers.open = true }},
		{"navigator", func(m *OS) { m.navigator.open = true }},
		{"layout", func(m *OS) { m.ShowLayoutPicker = true }},
		{"hostpicker", func(m *OS) { m.ShowHostPicker = true }},
		{"aggregate", func(m *OS) { m.ShowAggregateView = true }},
		{"settings", func(m *OS) { m.ShowSettings = true }},
		{"keybinds", func(m *OS) { m.ShowKeybindManager = true }},
		{"themepicker", func(m *OS) { m.ShowThemePicker = true }},
		{"themeeditor", func(m *OS) { m.themeEditor.open = true }},
		{"presetpicker", func(m *OS) { m.presetPicker.open = true }},
		{"glyphpicker", func(m *OS) { m.ShowGlyphPicker = true }},
		{"effectpicker", func(m *OS) { m.ShowEffectPicker = true }},
		{"dockeditor", func(m *OS) { m.ShowDockEditor = true }},
		{"sectioneditor", func(m *OS) { m.ShowSectionEditor = true }},
		{"accent", func(m *OS) { m.ShowAccentPicker = true }},
		{"quit", func(m *OS) { m.ShowQuitMenu = true }},
		{"sessionclose", func(m *OS) { m.ShowSessionClose = true }},
		{"filedialog", func(m *OS) { m.filePrompt.Kind = filePromptCreate }},
		{"gitdrawer", func(m *OS) { m.gitDrawer = gitDrawerState{open: true, kind: review.HistoryCommits} }},
		{overlayKindCopyDialog, func(m *OS) { m.transfers.dialog = &copyDialog{} }},
		{"worktreeprompt", func(m *OS) { m.worktreePrompt.RepoRoot = "/repo" }},
		{overlayKindLogs, func(m *OS) { m.ShowLogs = true }},
		{overlayKindMessage, func(m *OS) { m.msgView.open = true }},
	}
	covered := make(map[string]bool)
	for _, p := range panels {
		covered[p.name] = true
	}
	(&OS{}).eachOverlayGate(func(kind string, _ bool) bool {
		if !covered[kind] {
			t.Errorf("panel %q lacks a browser graphics regression", kind)
		}
		return true
	})
	popups := append(panels, []popupCase{
		{"rename-pane", func(m *OS) { m.BeginRenameWindow(m.Windows[0]) }},
		{"rename-session", func(m *OS) { m.BeginRenameSession("work") }},
		{"rename-workspace", func(m *OS) { m.BeginRenameWorkspace(1) }},
		{"context-menu", func(m *OS) { m.ContextMenu = &ContextMenu{} }},
		{"review", func(m *OS) { m.review.open = true }},
		{"scrollback", func(m *OS) { m.ShowScrollbackBrowser = true }},
		{"tape-manager", func(m *OS) { m.ShowTapeManager = true }},
		{"tape-review", func(m *OS) { m.ShowTapeReview = true }},
		{"cache-stats", func(m *OS) { m.ShowCacheStats = true }},
		{"commit-message", func(m *OS) { m.filePrompt.Kind = filePromptCommit }},
		{"discard-confirmation", func(m *OS) { m.filePrompt.Kind = filePromptConfirm }},
		{"which-key-popup", func(m *OS) {
			m.Settings.WhichKeyEnabled, m.Settings.WhichKeyPosition = true, "popup"
			m.PrefixActive, m.LastPrefixTime = true, time.Now().Add(-2*config.WhichKeyDelay)
		}},
		{"screensaver", func(m *OS) { m.screensaver.active, m.screensaver.frame = true, "saver" }},
		{"copy-save", func(m *OS) { m.MultiCopy = &MultiCopy{IDs: []string{m.Windows[0].ID}, Save: &MultiCopySave{}} }},
	}...)
	for _, p := range popups {
		t.Run(p.name, func(t *testing.T) {
			h := newOcclusionHarness(t)
			h.kp.inlineGraphics = false
			m := gitDrawerGraphicsOS(t, h.kp, h.winID)
			m.flushGraphicsForView()
			base := h.host.count("a=d,d=i,i=")
			p.open(m)
			m.flushGraphicsForView()
			if got := h.host.count("a=d,d=i,i="); got != base+1 {
				t.Fatalf("popup stayed behind browser image: hides %d -> %d", base, got)
			}
			shown := h.host.count("a=p,i=")
			m.flushGraphicsForView()
			if h.host.count("a=p,i=") != shown {
				t.Fatal("browser reappeared through the still-open popup")
			}
			// Reset UI state only. The same window and resident picture must
			// return with no retransmit from the browser process.
			closed := gitDrawerGraphicsOS(t, h.kp, h.winID)
			closed.flushGraphicsForView()
			if h.host.count("a=p,i=") != shown+1 {
				t.Fatal("browser did not return after closing the popup")
			}
		})
	}
}

func TestCaptureAndKeybarDoNotBlankTheBrowser(t *testing.T) {
	h := newOcclusionHarness(t)
	h.kp.inlineGraphics = false
	m := gitDrawerGraphicsOS(t, h.kp, h.winID)
	m.Capture.Active = true
	m.flushGraphicsForView()
	if h.kp.overlayActive {
		t.Fatal("capture mode hid the browser it is supposed to capture")
	}
	m.Capture.Active = false
	m.Settings.WhichKeyEnabled, m.Settings.WhichKeyPosition = true, "keybar"
	m.PrefixActive = true
	m.flushGraphicsForView()
	if h.kp.overlayActive {
		t.Fatal("dock-only key hints hid the browser despite drawing no popup")
	}
	// A transparent mode must not override a real popup opened over it.
	m.Capture.Active = true
	m.BeginRenameWindow(m.Windows[0])
	m.flushGraphicsForView()
	if !h.kp.overlayActive {
		t.Fatal("capture mode let the browser cover an open rename dialog")
	}
}
