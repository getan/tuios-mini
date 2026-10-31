package app

import (
	"github.com/charmbracelet/x/ansi"
	"tuios.dev/vt"
)

// NotePaneKeyDown records that the press of the key with this code went to the
// pane with this window ID.
//
// A pane that asked for key releases must get the release of every key it saw
// pressed, and must not get the release of a key it never saw. The leader's b
// is the plain case: tuios keeps the press, so its release is tuios's too.
func (m *OS) NotePaneKeyDown(code rune, windowID string) {
	w := m.InputPaneByID(windowID)
	if !m.HostReportsReleases() || w == nil || w.Terminal == nil ||
		w.Terminal.KittyKeyboardFlags()&ansi.KittyReportEventTypes == 0 {
		return
	}
	if m.paneKeysDown == nil {
		m.paneKeysDown = map[rune]string{}
	}
	m.paneKeysDown[code] = windowID
}

// TakePaneKeyDown returns the window ID the press of the key with this code
// went to, and forgets it. ok is false when the press went to no pane.
func (m *OS) TakePaneKeyDown(code rune) (windowID string, ok bool) {
	windowID, ok = m.paneKeysDown[code]
	if ok {
		delete(m.paneKeysDown, code)
	}
	return windowID, ok
}

// prunePaneKeysDown retires owners that disappeared or stopped asking for
// releases. A live owner is kept across pane/workspace focus changes, but a
// dead one must not keep the host in report-all-keys mode indefinitely.
func (m *OS) prunePaneKeysDown() {
	for code, id := range m.paneKeysDown {
		w := m.InputPaneByID(id)
		if w == nil || w.Terminal == nil || w.Terminal.KittyKeyboardFlags()&ansi.KittyReportEventTypes == 0 {
			delete(m.paneKeysDown, code)
		}
	}
}

// releasePaneKeysDown handles host focus loss: key-up may happen in another
// application and never arrive here. Release only keys sent to their original
// live owners, then forget them so neither a late release nor another blur can
// release twice. A pane that turned releases off receives nothing.
func (m *OS) releasePaneKeysDown() {
	down := m.paneKeysDown
	m.paneKeysDown = nil
	for code, id := range down {
		w := m.InputPaneByID(id)
		if w == nil || w.Terminal == nil {
			continue
		}
		if raw := vt.EncodeKeyReleaseCSIu(vt.KeyPressEvent{Code: code}, w.Terminal.KittyKeyboardFlags()); raw != "" {
			if err := w.SendInput([]byte(raw)); err != nil {
				m.LogWarn("Failed to release pane key on host focus loss: %v", err)
			}
		}
	}
}
