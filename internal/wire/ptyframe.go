package wire

import (
	"encoding/binary"
	"fmt"
	"io"
	"strings"
	"sync"
)

// Binary message helpers for high-frequency PTY I/O
// These bypass the codec system for maximum performance.
// Format: [4 bytes length][1 byte type][1 byte codec=0][36 bytes PTY ID][raw data]

// PTYFrameHeaderLen is the fixed part of a binary PTY frame: the length
// prefix, the type and codec bytes, and the padded PTY id.
const PTYFrameHeaderLen = 4 + 2 + 36

// ptyFrameBufs hands out frame buffers so a keystroke does not allocate one.
var ptyFrameBufs = sync.Pool{New: func() any {
	b := make([]byte, 0, PTYFrameHeaderLen+4096)
	return &b
}}

// WritePTYFrame writes one binary PTY frame in a single Write.
//
// Both callers write straight to an unbuffered unix socket, so separate
// writes for the length, the type and codec bytes, the 36-byte id and the data
// would be four syscalls. On the input path the user waits through them with
// the client's whole-client mutex held. One assembled frame and one write
// measured 1140ns for a keystroke, against 3346ns for four writes.
//
// The bytes are unchanged. The id field is still a fixed 36 bytes, zero padded
// when the id is shorter and truncated when it is longer, which is what copy
// into a 36-byte slice did before.
func WritePTYFrame(w io.Writer, msg MessageType, ptyID string, data []byte) error {
	bufp := ptyFrameBufs.Get().(*[]byte)
	buf := (*bufp)[:0]
	if cap(buf) < PTYFrameHeaderLen+len(data) {
		buf = make([]byte, 0, PTYFrameHeaderLen+len(data))
	}
	buf = buf[:PTYFrameHeaderLen]
	binary.BigEndian.PutUint32(buf, uint32(2+36+len(data)))
	buf[4], buf[5] = byte(msg), CodecGob
	clear(buf[6:PTYFrameHeaderLen])
	copy(buf[6:PTYFrameHeaderLen], ptyID)
	buf = append(buf, data...)

	_, err := w.Write(buf)

	// A batch far larger than the pool's buffers should not be kept alive by
	// it; anything up to the usual size goes back.
	if cap(buf) <= PTYFrameHeaderLen+256*1024 {
		*bufp = buf
		ptyFrameBufs.Put(bufp)
	}
	return err
}

// WritePTYOutput writes PTY output in optimized binary format.
func WritePTYOutput(w io.Writer, ptyID string, data []byte) error {
	return WritePTYFrame(w, MsgPTYOutput, ptyID, data)
}

// WritePTYInput writes PTY input in optimized binary format.
func WritePTYInput(w io.Writer, ptyID string, data []byte) error {
	return WritePTYFrame(w, MsgInput, ptyID, data)
}

// ParseBinaryPTYMessage parses a binary PTY message (Input or Output).
// Returns ptyID and data.
func ParseBinaryPTYMessage(payload []byte) (ptyID string, data []byte, err error) {
	if len(payload) < 36 {
		return "", nil, fmt.Errorf("payload too short for PTY message: %d bytes", len(payload))
	}
	// Trim the NUL padding from the ID. The loop this replaces stopped at the
	// first byte that was not NUL, so an ID of nothing but padding came back
	// as 36 NUL bytes rather than empty (FuzzReadMessageFraming).
	ptyID = strings.TrimRight(string(payload[:36]), "\x00")
	data = payload[36:]
	return ptyID, data, nil
}
