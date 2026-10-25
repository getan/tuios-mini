package app

import (
	tea "charm.land/bubbletea/v2"
	"github.com/getan/tuios-mini/internal/tape"
)

// EnterTerminalMode records the compatibility value used by old tapes/callers.
// Deprecated: focus now owns input; no raw-reader or input routing changes.
func (m *OS) EnterTerminalMode() tea.Cmd {
	// Record mode switch for tape recording
	if m.TapeRecorder != nil && m.TapeRecorder.IsRecording() {
		m.TapeRecorder.RecordModeSwitch(tape.CommandTypeTerminalMode)
	}

	m.Mode = TerminalMode

	// The raw reader is disabled. Bubbletea handles all input correctly,
	// including:
	// - Bracketed paste for Cmd+V (via PasteMsg)
	// - OSC 52 clipboard reading for Ctrl+Shift+V (via ClipboardMsg)
	// - All key events properly parsed
	// The raw reader conflicts with Bubbletea in modern terminals using CSI u
	// encoding.
	return nil
}

// ExitTerminalMode records the compatibility value used by old tapes/callers.
// Deprecated: this does not suspend input in a focused pane.
func (m *OS) ExitTerminalMode() tea.Cmd {
	// Record mode switch for tape recording
	if m.TapeRecorder != nil && m.TapeRecorder.IsRecording() {
		m.TapeRecorder.RecordModeSwitch(tape.CommandTypeWindowManagementMode)
	}

	m.Mode = WindowManagementMode

	// The raw reader is disabled. Bubbletea handles all input correctly.
	return nil
}
