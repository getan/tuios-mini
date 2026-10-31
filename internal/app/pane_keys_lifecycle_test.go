package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// These protocol-state regressions cover failures a PTY that only types text
// cannot see: a lost release after host blur, an owner that disappeared or
// stopped requesting releases, and a still-live owner on another workspace.
// Clearing everything on pane focus changes would fix the stale state while
// breaking the last case, so it is checked separately.
func TestPaneKeyReleasesOnHostBlur(t *testing.T) {
	m := inputTargetOS(t)
	w := m.Windows[0]
	w.Terminal.Write([]byte("\x1b[=31;1u"))
	m.KeyboardFlags = 31
	var got string
	w.DaemonMode = true
	w.DaemonWriteFunc = func(p []byte) error { got += string(p); return nil }
	m.NotePaneKeyDown('a', w.ID)
	if len(m.paneKeysDown) != 1 {
		t.Fatal("the fixture never recorded the key press")
	}
	m.Update(tea.BlurMsg{})
	if len(m.paneKeysDown) != 0 {
		t.Fatalf("host blur kept %d pending releases", len(m.paneKeysDown))
	}
	if got != "\x1b[97;1:3u" {
		t.Fatalf("blur sent %q, want one release to the press owner", got)
	}
	m.Update(tea.BlurMsg{})
	if got != "\x1b[97;1:3u" {
		t.Fatalf("repeated blur duplicated a release: %q", got)
	}
	w.Terminal.Write([]byte("\x1b[=1;1u"))
	if e := m.keyboardEnhancements(); e.ReportAllKeysAsEscapeCodes || e.ReportEventTypes {
		t.Fatalf("normal pane still requests all keys/releases after blur: %+v", e)
	}
}

func TestPaneKeyOwnersThatNoLongerNeedReleasesArePruned(t *testing.T) {
	for _, reason := range []string{"closed pane", "no emulator", "releases disabled"} {
		t.Run(reason, func(t *testing.T) {
			m := inputTargetOS(t)
			w := m.Windows[0]
			w.Terminal.Write([]byte("\x1b[=31;1u"))
			m.KeyboardFlags = 31
			m.NotePaneKeyDown('a', w.ID)
			switch reason {
			case "closed pane":
				w.ID = "replacement-pane"
				w.Terminal.Write([]byte("\x1b[=1;1u"))
			case "no emulator":
				w.Terminal = nil
			case "releases disabled":
				w.Terminal.Write([]byte("\x1b[=1;1u"))
			}
			e := m.keyboardEnhancements()
			if len(m.paneKeysDown) != 0 || e.ReportEventTypes || e.ReportAllKeysAsEscapeCodes {
				t.Fatalf("%s kept host in key-reporting mode: pending=%d enhancements=%+v", reason, len(m.paneKeysDown), e)
			}
		})
	}
}

func TestLivePaneKeyOwnerSurvivesWorkspaceChange(t *testing.T) {
	m := inputTargetOS(t)
	w := m.Windows[0]
	w.Terminal.Write([]byte("\x1b[=31;1u"))
	m.KeyboardFlags = 31
	m.NotePaneKeyDown('a', w.ID)
	m.CurrentWorkspace = 2
	if e := m.keyboardEnhancements(); !e.ReportEventTypes {
		t.Fatal("focus moving to another workspace lost a live pane's release")
	}
	if id, ok := m.TakePaneKeyDown('a'); !ok || id != w.ID {
		t.Fatalf("release owner = %q, %t, want %q", id, ok, w.ID)
	}
}
