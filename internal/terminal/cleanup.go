package terminal

import (
	"io"
	"os"
)

// ResetSequence puts the terminal back in a clean state: it undoes the modes a
// tuios client turns on and leaves the alternate screen.
//
// It must not clear anything. Bubble Tea has already left the alternate
// screen when this runs, so the host shows what it showed before tuios
// started. RIS (ESC c) would erase that screen again, and kitty and ghostty
// also drop the whole scrollback on it. DECSTR, the soft reset, puts the modes, margins, pen and
// character sets back without touching a cell.
const ResetSequence = "\033[!p" + // DECSTR: soft reset, which clears nothing
	"\033[=0;1u" + // Clear Kitty keyboard protocol flags
	"\033[>4m" + // Reset xterm modifyOtherKeys
	"\033[?1000l" + // Disable normal mouse tracking
	"\033[?1002l" + // Disable button event tracking
	"\033[?1003l" + // Disable all motion tracking
	"\033[?1004l" + // Disable focus tracking
	"\033[?1006l" + // Disable SGR extended mouse mode
	"\033[?1016l" + // Disable SGR-pixel mouse reports
	"\033[?2031l" + // Stop colour scheme (light and dark) reports
	"\033[?25h" + // Show cursor
	"\033]22;default\033\\" + // Pointer shape back to default (OSC 22)
	"\033[?47l" + // Exit alternate screen buffer
	"\033[0m" + // Reset all text attributes
	"\r\n" // Clean line ending

// WriteReset writes ResetSequence to w and returns any error from the write.
//
// It is the form a host that is not this process's stdout uses. The client
// always had ResetTerminal, which writes to os.Stdout because that is where a
// terminal client draws; a client whose terminal is a pty it was handed (an
// embedded workspace) must reset that pty and nothing else, or it would paint
// escape sequences onto whatever this process's stdout happens to be, which in
// a host can be another program's pipe.
func WriteReset(w io.Writer) error {
	if w == nil {
		return nil
	}
	_, err := io.WriteString(w, ResetSequence)
	return err
}

// ResetTerminal sends escape sequences to reset the terminal to a clean state.
// This should be called when exiting the application to restore the terminal.
func ResetTerminal() {
	_ = WriteReset(os.Stdout)
	_ = os.Stdout.Sync()
}
