package app

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// PrefixTimeout bounds a command chord. A prefix is a short-lived request,
// never a second working mode. There is no buffered text to replay on expiry.
const PrefixTimeout = 3 * time.Second

type PrefixExpiredMsg struct{ Started time.Time }

func (m *OS) BeginPrefix() tea.Cmd {
	m.CancelPrefix()
	m.PrefixActive = true
	m.LastPrefixTime = time.Now()
	started := m.LastPrefixTime
	return tea.Tick(PrefixTimeout, func(time.Time) tea.Msg { return PrefixExpiredMsg{Started: started} })
}

// CancelPrefix ends the complete chord, including a submenu and a repeat
// window. Esc, loss of host focus, pointer focus and the timeout share it.
func (m *OS) CancelPrefix() {
	m.PrefixActive, m.WorkspacePrefixActive, m.MinimizePrefixActive = false, false, false
	m.TilingPrefixActive, m.DebugPrefixActive, m.TapePrefixActive = false, false, false
	m.LayoutPrefixActive, m.TuiosPrefixActive = false, false
	m.ClearPrefixRepeat()
}

func (m *OS) ExpirePrefix(msg PrefixExpiredMsg) {
	if msg.Started.Equal(m.LastPrefixTime) {
		m.CancelPrefix()
	}
}

// CheckPrefixExpiry also covers delayed timers: the next key cannot complete
// a stale chord just because its timer was queued behind an output burst.
func (m *OS) CheckPrefixExpiry() {
	if m.prefixArmed() && !m.LastPrefixTime.IsZero() && time.Since(m.LastPrefixTime) >= PrefixTimeout {
		m.CancelPrefix()
	}
}
