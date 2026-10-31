package app

import (
	"time"

	"github.com/getan/tuios-mini/internal/terminal"
)

// InputTarget describes who owns input on this client. It is derived from
// focus and the surfaces currently open, never saved or toggled as a mode.
type InputTarget string

const (
	InputDesktop   InputTarget = "desktop"
	InputPane      InputTarget = "pane"
	InputSidebar   InputTarget = "sidebar"
	InputOverlay   InputTarget = "dialog"
	InputCommands  InputTarget = "command"
	InputSelection InputTarget = "selection"
	InputPointer   InputTarget = "pointer"
	InputPending   InputTarget = "opening"
)

// InputOverlayOpen includes keyboard-only surfaces as well as the draggable
// panels in AnyOverlayOpen. A surface owns even the keys it does not bind;
// neither typing nor a paste may fall through to a hidden shell.
func (m *OS) InputOverlayOpen() bool {
	return m.opaqueOverlayOpen() || m.CaptureActive() || m.HintsOpen() || m.PaneLabelsOpen()
}

// opaqueOverlayOpen is shared by input routing and pane-image suppression.
// Use the panel registry rather than a second list of flags: a new dialog must
// not take the keyboard while a browser's Kitty image still paints over it.
// Capture, hints and pane labels need the underlying pane to remain visible.
func (m *OS) opaqueOverlayOpen() bool {
	return m.AnyOverlayOpen() || m.ContextMenuActive() || m.ReviewOpen() ||
		m.Renaming() ||
		m.ShowScrollbackBrowser || m.ShowTapeReview || m.ShowTapeManager || m.ShowCacheStats
}

// PointerGestureActive is a grab of the pane's frame, not text selection in
// its content. Mouse forwarding must yield to it until the grab ends.
func (m *OS) PointerGestureActive() bool {
	return m.pointerGestureActive || m.Resizing ||
		(m.Dragging && m.DraggedWindowIndex >= 0 && m.DraggedWindowIndex < len(m.Windows) &&
			!m.Windows[m.DraggedWindowIndex].InCopyMode())
}

// FocusedInputPane is the visible pane focus identifies, before a dialog,
// prefix or selection temporarily borrows its keyboard. A pane in the rail is
// an input target too; it does not change the center session's focus.
func (m *OS) FocusedInputPane() *terminal.Window {
	if m.SidebarSessionFocused() {
		return m.SidebarSessionWindow()
	}
	if m.SidebarFocused {
		return nil
	}
	w := m.GetFocusedWindow()
	if w == nil || w.Minimized || w.ProcessExited() {
		return nil
	}
	return w
}

// InputTarget is the single answer shared by input routing, the cursor and
// keyboard negotiation. The old OS.Mode value is intentionally not read.
func (m *OS) InputTarget() InputTarget {
	if m.InputOverlayOpen() {
		return InputOverlay
	}
	if m.prefixArmed() {
		return InputCommands
	}
	if m.SidebarFocused && !m.SidebarSessionFocused() {
		return InputSidebar
	}
	if m.PointerGestureActive() {
		return InputPointer
	}
	if m.paneInputPending() {
		return InputPending
	}
	w := m.FocusedInputPane()
	if w == nil {
		return InputDesktop
	}
	if w.InCopyMode() && !w.InImplicitCopyMode() {
		return InputSelection
	}
	return InputPane
}

// KeyboardPane returns a pane only while it owns ordinary typing and paste.
// An implicit scrollback view is left by typing; an explicit selection is not.
func (m *OS) KeyboardPane() *terminal.Window {
	if m.InputTarget() != InputPane {
		return nil
	}
	return m.FocusedInputPane()
}

// FocusedPaneOwnsInput is for chrome around the center pane. A focused rail
// pane must not make the center pane look as though it owns the keyboard.
func (m *OS) FocusedPaneOwnsInput() bool {
	return !m.SidebarFocused && !m.SidebarSessionFocused() && m.KeyboardPane() != nil
}

// InputPaneByID resolves a key-release destination by the identity saved at
// key-down, even after focus moves or a dialog opens. No unmatched release is
// sent to the new pane, and the old program cannot get a stuck key.
func (m *OS) InputPaneByID(id string) *terminal.Window {
	for _, w := range m.Windows {
		if w.ID == id && !w.ProcessExited() {
			return w
		}
	}
	return m.sidebarSessionWindowByID(id)
}

// paneInputPending prevents keys intended for a newly requested pane from
// reaching the old one while the daemon is still creating its replacement.
func (m *OS) paneInputPending() bool {
	return m.pendingInputPane && m.pendingInputWorkspace == m.CurrentWorkspace
}

func (m *OS) beginPaneInputRequest() {
	if m.pendingInputPane && m.pendingInputWorkspace == m.CurrentWorkspace {
		m.pendingInputCount++
		return
	}
	m.pendingInputPane = true
	m.pendingInputCount = 1
	m.pendingInputStarted = time.Now()
	m.pendingInputWorkspace = m.CurrentWorkspace
	m.pendingInputWindows = make(map[string]bool, len(m.Windows))
	for _, w := range m.Windows {
		m.pendingInputWindows[w.ID] = true
	}
}

// CancelPendingPaneInput is called by an explicit focus action. Choosing an
// existing pane is a new input destination, not typing for the pending pane.
func (m *OS) CancelPendingPaneInput() {
	m.pendingInputPane = false
	m.pendingInputCount = 0
	m.pendingInputWindows = nil
}

func (m *OS) settlePendingPaneInput() {
	if !m.pendingInputPane {
		return
	}
	arrived := 0
	for _, w := range m.Windows {
		if w.Workspace == m.pendingInputWorkspace && !m.pendingInputWindows[w.ID] {
			arrived++
		}
	}
	if w := m.GetFocusedWindow(); arrived >= m.pendingInputCount && w != nil &&
		w.Workspace == m.pendingInputWorkspace && !m.pendingInputWindows[w.ID] {
		m.CancelPendingPaneInput()
	}
}

// A failed asynchronous spawn cannot silently send subsequent typing back to
// the old shell. Give up the pending destination explicitly, with no focus;
// clicking an existing pane or creating another is the way forward.
func (m *OS) checkPendingPaneInput(now time.Time) {
	if !m.pendingInputPane || now.Sub(m.pendingInputStarted) < 10*time.Second {
		return
	}
	if m.CurrentWorkspace == m.pendingInputWorkspace {
		m.FocusedWindow = -1
	}
	m.CancelPendingPaneInput()
	m.ShowNotification("The new pane did not arrive. Select a pane or try opening it again.", "error", m.Settings.NotificationDuration)
}
