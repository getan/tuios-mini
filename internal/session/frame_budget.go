package session

import (
	"bufio"
	"encoding/json"
	"errors"
	"log"
	"net"
	"sync/atomic"
	"time"

	"github.com/getan/tuios-mini/internal/wire"
)

// Bounding the memory the daemon gives to what clients send.
//
// The binary protocol frames each message with a length the sender writes, up
// to maxFrameBytes. Any process that can reach a socket can send a header, so
// the daemon reads every client frame through readClientFrame, which holds
// these limits:
//
//   - The frame's type is checked before its body is read: the type's own
//     size limit (daemonFrameLimit), then the link policy and the pane grants.
//     A refused frame is skipped unread, and the stream stays in step.
//   - A body over wire.LargeFrame is charged to a read budget before any of it
//     is read: its whole announced size, wire.FrameCopies times, in one
//     acquire that takes all of it or nothing. The budget is a FIFO semaphore,
//     so large frames are read one after another and never hold part of the
//     budget while they wait for the rest. A frame that cannot get its charge
//     within wire.ReadBudgetWait is skipped and refused with ErrCodeBusy.
//   - The body is read into memory that grows as its bytes arrive, never
//     allocated whole from the header. See wire.ReadFramePayload.
//   - The body must arrive within wire.FrameBodyDeadline, so a sender that
//     stalls cannot hold its charge.
//
// There are two budgets (readBudgetFor). The person's clients draw on one,
// and so does the person working through a hub. Other links, hosted pane
// calls and panes without the admin grant draw on the other, so no peer and no pane can spend the budget the person's own paste
// needs. The JSON verb line reader (verb_lines.go) charges the same budgets.
//
// The charge is given back when the message has been handled, or for
// MsgInput as soon as it is read: a paste into a pane that does not read its
// input blocks in the write to the pane, and must not hold the budget there.
// A verb line gives its charge back once its envelope is decoded, for the
// same reason (verb_lines.go). What such a write holds is bounded per pane
// instead: one large input may wait on each pane, from the client or from
// any verb that types (PTY.Write). The next waits for it, and is refused as
// busy once the write holding the pane has waited paneWriteWait, so the pane
// is not reading. So the memory blocked writes hold is at most maxFrameBytes
// for each pane, plus the writes waiting for their turn, each until the pane
// has not read for paneWriteWait.
// That is accepted: only a caller that may already write to the pane can
// cause it.
//
// And the daemon serves at most maxConnections connections on its socket and
// maxLinkConnections on its link sockets. See admitConnection.

const (
	// firstByteDeadline is how long a new connection may stay silent before
	// it says what it speaks. Every client speaks first.
	firstByteDeadline = 30 * time.Second

	// maxConnections is the most connections the daemon serves at once on
	// its own socket. One TUI client takes a few; a command takes one for as
	// long as it runs. An idle connection costs about 30 KB, so this many
	// cost about 30 MB.
	maxConnections = 1024

	// maxLinkConnections is the most it serves on its two link sockets
	// together. A peer with link rights cannot take the slots the person
	// needs on the main socket.
	maxLinkConnections = 256

	// connReadBuffer is the read buffer each connection holds for its life.
	// Small frames, which are most of them, are read through it; a larger
	// read goes past it to the connection.
	connReadBuffer = 4 << 10
)

// readBudgetFor is the budget a large read on cs draws on: the person's, for
// the person's own clients and the person working through a hub on the
// link-human socket, or for any other link, a pane without the admin grant or
// a hosted pane call, the budget those share.
func (d *Daemon) readBudgetFor(cs *connState) *memBudget {
	d.readBudgetOnce.Do(func() {
		d.personBudget = newMemBudget(personBudgetBytes)
		d.peerBudget = newMemBudget(peerBudgetBytes)
	})
	if cs == nil || cs.paneOnly {
		return d.peerBudget
	}
	if cs.viaLink {
		// The person working through a hub is the person. Any other link
		// is a peer.
		if cs.linkHuman {
			return d.personBudget
		}
		return d.peerBudget
	}
	if d.manager != nil && !d.bufferAccess(cs).person() {
		return d.peerBudget
	}
	return d.personBudget
}

// FrameForbiddenError is a frame whose type the connection may not send, under
// its link policy or its pane's grants. The body was skipped unread.
type FrameForbiddenError struct {
	ReqID uint64
	Err   *verbError
}

func (e *FrameForbiddenError) Error() string { return e.Err.Message }

// readClientFrame reads the next frame from a client. It returns the message
// and the function that gives back its read budget, which the caller calls
// once it is done with the message's memory. A *FrameTooLargeError,
// *FrameForbiddenError or *FrameBusyError leaves the stream in step.
func (d *Daemon) readClientFrame(cs *connState, br *bufio.Reader) (*Message, func(), error) {
	noop := func() {}
	// No deadline between frames: the wait costs nothing until a frame
	// arrives or the connection is closed, and both drop and shutdown close
	// it.
	totalLen, err := wire.ReadLength(cs.conn, br, 0, wire.FrameBodyDeadline)
	if err != nil {
		return nil, noop, err
	}
	h, err := wire.ReadFrameHeader(br, totalLen)
	if err != nil {
		return nil, noop, err
	}
	if typeMax := daemonFrameLimit(h.Type); totalLen > typeMax {
		if err := h.SkipPayload(br); err != nil {
			return nil, noop, err
		}
		return nil, noop, &FrameTooLargeError{Type: h.Type, Size: totalLen, Limit: typeMax, ReqID: h.ReqID}
	}
	// The link policy and the pane grants are held before the body is read,
	// so a frame the connection may not send never takes memory.
	verr := d.checkLinkMessage(cs, h.Type)
	if verr == nil {
		verr = d.checkGrantMessage(cs, h.Type)
	}
	if verr != nil {
		if err := h.SkipPayload(br); err != nil {
			return nil, noop, err
		}
		return nil, noop, &FrameForbiddenError{ReqID: h.ReqID, Err: verr}
	}
	release := noop
	if h.PayloadLen > wire.LargeFrame {
		var ok bool
		release, ok = d.readBudgetFor(cs).acquire(int64(h.PayloadLen)*wire.FrameCopies, wire.ReadBudgetWait)
		if !ok {
			if err := h.SkipPayload(br); err != nil {
				return nil, noop, err
			}
			return nil, noop, &FrameBusyError{Type: h.Type, Size: totalLen, ReqID: h.ReqID}
		}
	}
	msg, err := h.ReadPayload(br)
	if err != nil {
		release()
		return nil, noop, err
	}
	return msg, release, nil
}

// errTooManyConnections is what a connection over the cap is told before it
// is closed.
const errTooManyConnections = "The tuios daemon has too many connections. Close some clients and try again."

// ErrTooManyConnections is the error a client gets when the daemon refused
// its connection over the cap.
var ErrTooManyConnections = errors.New("the tuios daemon has too many connections")

// admitConnection counts a connection just accepted on a socket whose count
// is open and whose cap is limit, or refuses it when the socket already has
// that many. A caller that admits one subtracts it from open when the
// connection ends. A refused connection is told why before it is closed.
// A refusal is logged at most once in connRefusedLogEvery, with the count
// since the last line, so a flood of connections does not also flood the log.
func (d *Daemon) admitConnection(conn net.Conn, open *atomic.Int64, limit int64) bool {
	if open.Add(1) <= limit {
		return true
	}
	open.Add(-1)
	d.refuseConnection(conn)
	n := d.connRefusals.Add(1)
	now := time.Now().UnixNano()
	last := d.connRefusedLog.Load()
	if now-last >= int64(connRefusedLogEvery) && d.connRefusedLog.CompareAndSwap(last, now) {
		d.connRefusals.Add(-n)
		if n == 1 {
			log.Printf("Refused a new connection. The socket already serves %d connections, which is the maximum.", limit)
		} else {
			log.Printf("Refused %d new connections. The socket already serves %d connections, which is the maximum.", n, limit)
		}
	}
	return false
}

// maxRefusalsTold is how many refused connections are told why at once. The
// rest are closed with nothing said, so a flood of connections cannot make
// the daemon hold a goroutine for each.
const maxRefusalsTold = 32

// refuseConnection tells a connection over the cap why, in the protocol it
// speaks, and closes it. It waits a short time for the first byte: a JSON
// client gets an error line and a binary client an error frame.
func (d *Daemon) refuseConnection(conn net.Conn) {
	if d.refusalsTold.Add(1) > maxRefusalsTold {
		d.refusalsTold.Add(-1)
		_ = conn.Close()
		return
	}
	go func() {
		defer d.refusalsTold.Add(-1)
		defer func() { _ = conn.Close() }()
		_ = conn.SetDeadline(time.Now().Add(time.Second))
		var first [1]byte
		n, _ := conn.Read(first[:])
		_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
		if n == 1 && isJSONStart(first[0]) {
			line, err := json.Marshal(verbResponse{Error: newVerbError(ErrVerbTooManyConnections, errTooManyConnections)})
			if err == nil {
				_, _ = conn.Write(append(line, '\n'))
			}
			return
		}
		msg, err := NewMessage(MsgError, &ErrorPayload{Code: ErrCodeBusy, Message: errTooManyConnections})
		if err == nil {
			_ = WriteMessage(conn, msg)
		}
	}()
}

// connRefusedLogEvery is the least time between two log lines about refused
// connections.
const connRefusedLogEvery = 10 * time.Second

// errFirstByteTimeout is a connection that said nothing within
// firstByteDeadline.
var errFirstByteTimeout = errors.New("the connection sent nothing")

// ErrVerbBusy refuses a verb the daemon has no room for now: its request
// line could not get its read budget, or the pane it types into has not read
// the last large input. Nothing was done, and the caller may try again. It is
// the verb protocol's ErrCodeBusy.
const ErrVerbBusy = "busy"

// ErrVerbTooManyConnections is the verb error a JSON client gets when the
// daemon refuses its connection over the cap.
const ErrVerbTooManyConnections = "too_many_connections"

// isJSONStart reports whether b starts a JSON verb request: '{' or leading
// whitespace. A binary client's first byte is the high byte of a big-endian
// length, 0x00 or 0x01 for any frame under the 16 MB cap.
func isJSONStart(b byte) bool {
	switch b {
	case '{', ' ', '\t', '\n', '\r':
		return true
	}
	return false
}
