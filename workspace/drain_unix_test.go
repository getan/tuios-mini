//go:build unix

package workspace

import (
	"errors"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// An SSH keyboard reply may never arrive. Even with the writer still open,
// cleanup must return after its bounded wait instead of blocking in Read.
func TestDrainLateKeyboardReplyReturnsWithoutInput(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "test")
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	done := make(chan struct{})
	go func() {
		drainLateKeyboardReply(r)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("draining an idle input blocked past its deadline")
	}
}

func TestDrainLateKeyboardReplyPreservesNonblockingInput(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "test")
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	fd := int(r.Fd())
	if err := unix.SetNonblock(fd, true); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("\x1b[?5u")); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		drainLateKeyboardReply(r)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("draining a late reply blocked past its deadline")
	}
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil {
		t.Fatal(err)
	}
	if flags&unix.O_NONBLOCK == 0 {
		t.Fatal("drain changed an already nonblocking input to blocking")
	}
	buf := make([]byte, 16)
	if n, err := unix.Read(fd, buf); n > 0 || !errors.Is(err, unix.EAGAIN) {
		t.Fatalf("keyboard reply was not drained: read=%d err=%v", n, err)
	}
}
