//go:build unix

package workspace

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/unix"
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
	raw, err := in.SyscallConn()
	if err != nil {
		return
	}
	// Fd() can itself switch an os.File to blocking mode. Control keeps the
	// descriptor valid while preserving the caller's original flags.
	_ = raw.Control(func(rawFD uintptr) {
		fd := int(rawFD)
		flags, err := unix.FcntlInt(rawFD, unix.F_GETFL, 0)
		if err != nil {
			return
		}
		if flags&unix.O_NONBLOCK == 0 {
			if err := unix.SetNonblock(fd, true); err != nil {
				return
			}
			defer func() { _ = unix.SetNonblock(fd, false) }()
		}

		buf := make([]byte, 256)
		deadline := time.Now().Add(150 * time.Millisecond)
		for time.Now().Before(deadline) {
			// os.File.Read may wait in Go's poller after EAGAIN, defeating the
			// deadline even though the descriptor is nonblocking.
			n, err := unix.Read(fd, buf)
			if n > 0 {
				continue
			}
			if err != nil {
				if !errors.Is(err, unix.EAGAIN) && !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EINTR) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
		}
	})
}
