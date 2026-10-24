package wire

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"
)

// LogFrame, when set, is called with every frame this package writes or reads:
// "SEND" or "RECV" and the message. It is how the session package hangs its
// protocol log on the bytes without the bytes knowing about the log; the wire
// itself only ever calls it. Nil is no logger.
var LogFrame func(direction string, msg *Message)

// WriteMessage writes one framed message.
// Wire format: [4 bytes BE length][1 byte type][1 byte codec][payload]
//
// The header and the payload go out through net.Buffers. On a *net.UnixConn,
// which is what every production caller passes, that is one writev with no
// copy of the payload; on any other writer it is one write for the header and
// one for the payload. Writing the length, the type and codec bytes and the
// payload separately would be three syscalls under the caller's send lock,
// for the same bytes on the wire.
//
// Measured on a unix socket against three writes, a 64-byte frame takes about
// 1.3us instead of 2.2us and a 4 KiB frame 1.65us instead of 2.2us. At 64 KiB
// and above the copy into the socket dominates and the two are within noise of each other.
func WriteMessage(w io.Writer, msg *Message) error {
	var hdr [6 + ReqIDLen]byte
	head := hdr[:6]
	// Length counts the type and codec bytes plus the payload, and the
	// request id when there is one.
	binary.BigEndian.PutUint32(hdr[:4], uint32(2+len(msg.Payload)))
	hdr[4], hdr[5] = byte(msg.Type), CodecGob
	if msg.ReqID != 0 {
		binary.BigEndian.PutUint32(hdr[:4], uint32(2+ReqIDLen+len(msg.Payload)))
		hdr[5] = CodecGobTagged
		binary.BigEndian.PutUint64(hdr[6:], msg.ReqID)
		head = hdr[:]
	}

	if len(msg.Payload) == 0 {
		if _, err := w.Write(head); err != nil {
			return fmt.Errorf("failed to write message header: %w", err)
		}
	} else {
		bufs := net.Buffers{head, msg.Payload}
		if _, err := bufs.WriteTo(w); err != nil {
			return fmt.Errorf("failed to write message: %w", err)
		}
	}

	// Debug logging
	logFrame("SEND", msg)

	return nil
}

// logFrame hands one frame to the protocol log, if a logger is installed.
func logFrame(direction string, msg *Message) {
	if LogFrame != nil {
		LogFrame(direction, msg)
	}
}

// ReadMessage reads one framed message. The codec byte is read and ignored.
// Wire format: [4 bytes BE length][1 byte type][1 byte codec][payload]
func ReadMessage(r io.Reader) (*Message, error) {
	var totalLen uint32
	if err := binary.Read(r, binary.BigEndian, &totalLen); err != nil {
		if err == io.EOF {
			return nil, err
		}
		return nil, fmt.Errorf("failed to read message length: %w", err)
	}
	return ReadMessageBody(r, totalLen, nil)
}

// ReadMessageBuffered reads a framed message from r, a *bufio.Reader over
// conn, applying boundaryTimeout only to the 4-byte length prefix and
// bodyTimeout to the header and payload. Splitting the deadline keeps a large
// payload that arrives across several reads from being cut mid-frame, which
// would desync the stream. A timeout of 0 clears the read deadline for that
// part: at the boundary, the read then waits for the next frame with no
// wakeup at all.
//
// The client's read loop goes through here. The daemon reads with
// Daemon.readClientFrame, which checks a frame's type and charges its memory
// before it reads the body. Neither may read conn directly once the reader
// holds bytes.
func ReadMessageBuffered(conn net.Conn, r io.Reader, boundaryTimeout, bodyTimeout time.Duration) (*Message, error) {
	totalLen, err := ReadLength(conn, r, boundaryTimeout, bodyTimeout)
	if err != nil {
		return nil, err
	}
	return ReadMessageBody(r, totalLen, nil)
}

// ReadLength reads the length prefix of the next frame with boundaryTimeout,
// then arms bodyTimeout for the rest of the frame.
func ReadLength(conn net.Conn, r io.Reader, boundaryTimeout, bodyTimeout time.Duration) (uint32, error) {
	setBoundaryDeadline(conn, boundaryTimeout)

	var totalLen uint32
	if err := binary.Read(r, binary.BigEndian, &totalLen); err != nil {
		if err == io.EOF {
			return 0, err
		}
		return 0, fmt.Errorf("failed to read message length: %w", err)
	}

	if bodyTimeout > 0 {
		_ = conn.SetReadDeadline(time.Now().Add(bodyTimeout))
	} else {
		_ = conn.SetReadDeadline(time.Time{})
	}
	return totalLen, nil
}

// setBoundaryDeadline arms the deadline for the wait between frames, or
// clears it when there is none.
//
// The read loops do not poll their done channels with a short deadline. A
// 100 ms poll costs an idle connection a timer, a wakeup and an empty read ten
// times a second on each side, and it is not needed: the only things that
// close those channels close the connection with them, and a closed
// connection wakes the read on its own.
func setBoundaryDeadline(conn net.Conn, timeout time.Duration) {
	if timeout > 0 {
		_ = conn.SetReadDeadline(time.Now().Add(timeout))
		return
	}
	_ = conn.SetReadDeadline(time.Time{})
}

// Frame is what precedes a frame's payload: its type, the request id it
// carries (0 when it carries none) and the length of the payload.
type Frame struct {
	Type       MessageType
	ReqID      uint64
	PayloadLen uint32
	TotalLen   uint32
}

// ReadFrameHeader reads the type, codec and request id that follow the
// length prefix, and checks the length against them.
func ReadFrameHeader(r io.Reader, totalLen uint32) (Frame, error) {
	// Sanity check length (max 16MB). A frame past this is not skipped: a
	// length that large is more likely a stream out of step than a frame.
	if totalLen > MaxFrameBytes {
		return Frame{}, fmt.Errorf("message too large: %d bytes (raw: 0x%08x)", totalLen, totalLen)
	}

	if totalLen < 2 {
		return Frame{}, fmt.Errorf("message too small: %d bytes", totalLen)
	}

	// Read type and codec. The codec byte is gob, with or without a request
	// id in front of the payload.
	var header [2]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return Frame{}, fmt.Errorf("failed to read message header (after len=%d): %w", totalLen, err)
	}

	h := Frame{Type: MessageType(header[0]), PayloadLen: totalLen - 2, TotalLen: totalLen}
	tagged := header[1] == CodecGobTagged
	if tagged && h.PayloadLen < ReqIDLen {
		// Too short to hold the id it says it carries, which no sender writes:
		// the same fault as a frame too small for its header.
		return Frame{}, fmt.Errorf("tagged message too small: %d bytes", totalLen)
	}

	if tagged {
		var id [ReqIDLen]byte
		if _, err := io.ReadFull(r, id[:]); err != nil {
			return Frame{}, fmt.Errorf("failed to read request id (len=%d, type=%d): %w", h.PayloadLen, h.Type, err)
		}
		h.ReqID = binary.BigEndian.Uint64(id[:])
		if h.ReqID == 0 {
			// Zero is the id of a message that answers nothing, and that
			// message goes out untagged. A tagged zero would read as the
			// same Message as an untagged frame, so the frame a relay writes
			// back would not be the one it read. No sender writes one.
			return Frame{}, fmt.Errorf("tagged message with request id 0 (type=%d)", h.Type)
		}
		h.PayloadLen -= ReqIDLen
	}
	return h, nil
}

// SkipPayload reads the payload of a refused frame and drops it, so the
// stream stays in step.
func (h Frame) SkipPayload(r io.Reader) error {
	if _, err := io.CopyN(io.Discard, r, int64(h.PayloadLen)); err != nil {
		return fmt.Errorf("failed to skip a refused message payload (len=%d, type=%d): %w", h.PayloadLen, h.Type, err)
	}
	return nil
}

// ReadMessageBody reads the header and payload after the length prefix has
// already been consumed from r. limit, when not nil, is the largest frame
// accepted for each message type. A frame over its type's limit is skipped
// unread and reported as a *FrameTooLargeError, after which the stream is
// still in step. A nil limit allows every type the 16 MB any frame may have.
func ReadMessageBody(r io.Reader, totalLen uint32, limit func(MessageType) uint32) (*Message, error) {
	h, err := ReadFrameHeader(r, totalLen)
	if err != nil {
		return nil, err
	}

	// A frame over its own type's limit is skipped before any of it is
	// decoded, which is the point of the limit: see the session package's
	// wire_bounds.go. The request id was read first so the refusal can answer
	// the request.
	if limit != nil {
		if typeMax := limit(h.Type); totalLen > typeMax {
			if err := h.SkipPayload(r); err != nil {
				return nil, err
			}
			return nil, &FrameTooLargeError{Type: h.Type, Size: totalLen, Limit: typeMax, ReqID: h.ReqID}
		}
	}
	return h.ReadPayload(r)
}

// ReadPayload reads the payload h announces and builds the message.
func (h Frame) ReadPayload(r io.Reader) (*Message, error) {
	payload, err := ReadFramePayload(r, int(h.PayloadLen))
	if err != nil {
		return nil, fmt.Errorf("failed to read message payload (len=%d, type=%d): %w", h.PayloadLen, h.Type, err)
	}

	msg := &Message{
		Type:    h.Type,
		Payload: payload,
		ReqID:   h.ReqID,
	}

	// Debug logging
	logFrame("RECV", msg)

	return msg, nil
}

// ReadFramePayload reads n payload bytes into memory that grows as they
// arrive: it starts at FirstFrameChunk, or twice what a buffered reader
// already holds, and doubles, by FrameGrowStep at most, up to n. A sender that
// announces more than it sends costs at most twice what it sent, not what it
// announced.
func ReadFramePayload(r io.Reader, n int) ([]byte, error) {
	if n == 0 {
		return nil, nil
	}
	first := FirstFrameChunk
	// Bytes a buffered reader already holds have arrived. Starting at twice
	// them costs no more than the doublings would after them, and takes most
	// frames in one buffer.
	if b, ok := r.(interface{ Buffered() int }); ok {
		first = max(first, 2*b.Buffered())
	}
	payload := make([]byte, 0, min(n, first))
	for len(payload) < n {
		if len(payload) == cap(payload) {
			want := min(n, cap(payload)+min(cap(payload), FrameGrowStep))
			next := make([]byte, len(payload), want)
			copy(next, payload)
			payload = next
		}
		k, err := io.ReadFull(r, payload[len(payload):cap(payload)])
		payload = payload[:len(payload)+k]
		if err != nil {
			return nil, err
		}
	}
	return payload, nil
}
