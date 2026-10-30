//go:build unix

package workspace

import (
	"os"
	"syscall"
)

// drainLateKeyboardReply removes the tail of a Kitty keyboard-protocol query
// answer that arrived after the program stopped reading. Over SSH the round
// trip can outlive the client, leaving a response such as CSI ? 5u in the
// shell's input, where it renders as "5u". This reads only bytes already
// waiting. The query itself is filtered from host output on SSH, so there is
// nothing to wait for; a local terminal pays nothing.
func drainLateKeyboardReply(in *os.File) {
	if in == nil || !sshTerminalEnvironment() {
		return
	}
	fd := int(in.Fd())
	if err := syscall.SetNonblock(fd, true); err != nil {
		return
	}
	defer func() { _ = syscall.SetNonblock(fd, false) }()

	buf := make([]byte, 256)
	for {
		n, err := in.Read(buf)
		if n > 0 {
			continue
		}
		if err != nil {
			break
		}
	}
}
