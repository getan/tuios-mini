//go:build unix

package workspace

import (
	"errors"
	"os"
	"syscall"
	"time"
)

// drainLateKeyboardReply removes the tail of a Kitty keyboard-protocol query
// answer that arrived after the program stopped reading. Over SSH the round
// trip can outlive the client, leaving a response such as CSI ? 5u in the
// shell's input, where it renders as "5u". This reads only bytes already
// waiting, and waits briefly for one last delayed packet. A local terminal has
// no such round trip, so it pays no delay.
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
	deadline := time.Now().Add(150 * time.Millisecond)
	for time.Now().Before(deadline) {
		n, err := in.Read(buf)
		if n > 0 {
			continue
		}
		if err != nil {
			if !errors.Is(err, syscall.EAGAIN) && !errors.Is(err, syscall.EWOULDBLOCK) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
}
