package session

import (
	"io"
	"net"
	"time"

	"github.com/getan/tuios-mini/internal/wire"
)

// The seam between this package and internal/wire.
//
// The binary protocol is not a session's to own: the numbering, the frame
// layout, the gob codec, the depth scan and the read budget are about bytes,
// and internal/wire holds them. What stays here is everything that knows what
// the bytes mean: the payload DTOs, their gob registration (codec.go), the
// state they carry, and the daemon's policy of which type may be how large.
//
// The names below are aliases and one-line delegations, not copies. A caller
// outside this package keeps writing session.MsgInput, session.Message and
// session.WriteMessage, and gets exactly the wire's type, constant and
// function: there is one implementation of every one of them, in
// internal/wire. Aliases also keep the seam verifiable — a value that is
// aliased cannot drift from the value it aliases.

// MessageType is wire.MessageType: one byte on the wire, numbering the
// message types.
type MessageType = wire.MessageType

// Message is wire.Message: a type, a payload and an optional request id.
type Message = wire.Message

// FrameTooLargeError is wire.FrameTooLargeError.
type FrameTooLargeError = wire.FrameTooLargeError

// FrameBusyError is wire.FrameBusyError.
type FrameBusyError = wire.FrameBusyError

// The message types, numbered in internal/wire. Every value is the wire's own:
// see the note on the iota order in wire/messagetype.go before adding one.
const (
	MsgHello             = wire.MsgHello
	MsgAttach            = wire.MsgAttach
	MsgDetach            = wire.MsgDetach
	MsgNew               = wire.MsgNew
	MsgList              = wire.MsgList
	MsgKill              = wire.MsgKill
	MsgInput             = wire.MsgInput
	MsgResize            = wire.MsgResize
	MsgPing              = wire.MsgPing
	MsgCreatePTY         = wire.MsgCreatePTY
	MsgClosePTY          = wire.MsgClosePTY
	MsgListPTYs          = wire.MsgListPTYs
	MsgFocusPTY          = wire.MsgFocusPTY
	MsgGetState          = wire.MsgGetState
	MsgUpdateState       = wire.MsgUpdateState
	MsgSubscribePTY      = wire.MsgSubscribePTY
	MsgUnsubscribePTY    = wire.MsgUnsubscribePTY
	MsgGetTerminalState  = wire.MsgGetTerminalState
	MsgExecuteCommand    = wire.MsgExecuteCommand
	MsgSendKeys          = wire.MsgSendKeys
	MsgSetConfig         = wire.MsgSetConfig
	MsgCapturePane       = wire.MsgCapturePane
	MsgWelcome           = wire.MsgWelcome
	MsgAttached          = wire.MsgAttached
	MsgDetached          = wire.MsgDetached
	MsgSessionList       = wire.MsgSessionList
	MsgOutput            = wire.MsgOutput
	MsgError             = wire.MsgError
	MsgPong              = wire.MsgPong
	MsgSessionEnded      = wire.MsgSessionEnded
	MsgWindowChanged     = wire.MsgWindowChanged
	MsgPTYList           = wire.MsgPTYList
	MsgPTYCreated        = wire.MsgPTYCreated
	MsgPTYClosed         = wire.MsgPTYClosed
	MsgPTYOutput         = wire.MsgPTYOutput
	MsgStateData         = wire.MsgStateData
	MsgTerminalState     = wire.MsgTerminalState
	MsgCommandResult     = wire.MsgCommandResult
	MsgRemoteCommand     = wire.MsgRemoteCommand
	MsgGetLogs           = wire.MsgGetLogs
	MsgLogsData          = wire.MsgLogsData
	MsgQueryWindows      = wire.MsgQueryWindows
	MsgWindowList        = wire.MsgWindowList
	MsgQuerySession      = wire.MsgQuerySession
	MsgSessionInfo       = wire.MsgSessionInfo
	MsgStateSync         = wire.MsgStateSync
	MsgClientJoined      = wire.MsgClientJoined
	MsgClientLeft        = wire.MsgClientLeft
	MsgSessionResize     = wire.MsgSessionResize
	MsgForceRefresh      = wire.MsgForceRefresh
	MsgRequestFullSync   = wire.MsgRequestFullSync
	MsgResurrect         = wire.MsgResurrect
	MsgPTYResized        = wire.MsgPTYResized
	MsgAgentMail         = wire.MsgAgentMail
	MsgHostsChanged      = wire.MsgHostsChanged
	MsgReadDir           = wire.MsgReadDir
	MsgDirListing        = wire.MsgDirListing
	MsgClientFocus       = wire.MsgClientFocus
	MsgLayoutTree        = wire.MsgLayoutTree
	MsgTypeAtPrompt      = wire.MsgTypeAtPrompt
	MsgPromptTyped       = wire.MsgPromptTyped
	MsgClientGraphics    = wire.MsgClientGraphics
	MsgMasterLayout      = wire.MsgMasterLayout
	MsgClientActivity    = wire.MsgClientActivity
	MsgWatchDir          = wire.MsgWatchDir
	MsgDirChanged        = wire.MsgDirChanged
	MsgSidebarVisibility = wire.MsgSidebarVisibility
	MsgSessionUsed       = wire.MsgSessionUsed
	MsgSessionsChanged   = wire.MsgSessionsChanged
	MsgPaneMark          = wire.MsgPaneMark
)

// The codec bytes, the request id, and the limits the frames are held to. The
// values are the wire's, so a size or a codec byte cannot differ between the
// package that writes a frame and the package that names it.
const (
	wireCodecGob       = wire.CodecGob
	wireCodecGobTagged = wire.CodecGobTagged
	reqIDLen           = wire.ReqIDLen
	wireCodecName      = wire.CodecName

	maxFrameBytes = wire.MaxFrameBytes

	largeFrame      = wire.LargeFrame
	firstFrameChunk = wire.FirstFrameChunk
	frameGrowStep   = wire.FrameGrowStep

	personBudgetBytes = wire.PersonBudgetBytes
	peerBudgetBytes   = wire.PeerBudgetBytes
	readBudgetWait    = wire.ReadBudgetWait

	// maxGobNesting and maxGobTypes are the depth scan's bounds. The scan
	// itself is internal/wire/gobscan.go.
	maxGobNesting = wire.MaxGobNesting
	maxGobTypes   = wire.MaxGobTypes
)

// The scan's refusals, as the session's own tests and error text know them.
var (
	errGobNested = wire.ErrGobNested
	errGobTypes  = wire.ErrGobTypes
)

// memBudget is the wire's read budget under the name the daemon's budget
// policy calls it (frame_budget.go). The arithmetic of a charge is in
// internal/wire/budget.go; only the policy of who draws on which budget is
// here.
type memBudget struct {
	*wire.Budget
}

// newMemBudget makes a budget of size bytes.
func newMemBudget(size int64) *memBudget {
	return &memBudget{Budget: wire.NewBudget(size)}
}

// acquire takes n bytes of the budget, waiting up to wait, and returns the
// function that gives them back. False means the budget did not free in time
// and nothing was taken.
func (b *memBudget) acquire(n int64, wait time.Duration) (func(), bool) {
	return b.Budget.Acquire(n, wait)
}

// WriteMessage writes one framed message.
func WriteMessage(w io.Writer, msg *Message) error { return wire.WriteMessage(w, msg) }

// ReadMessage reads one framed message.
func ReadMessage(r io.Reader) (*Message, error) { return wire.ReadMessage(r) }

// ReadMessageBuffered reads a framed message, splitting the deadline between
// the wait for a frame and the arrival of its body.
func ReadMessageBuffered(conn net.Conn, r io.Reader, boundaryTimeout, bodyTimeout time.Duration) (*Message, error) {
	return wire.ReadMessageBuffered(conn, r, boundaryTimeout, bodyTimeout)
}

// NewMessage creates a message with a gob-encoded payload.
func NewMessage(msgType MessageType, payload any) (*Message, error) {
	return wire.NewMessage(msgType, payload)
}

// encodePayload serializes a message payload with gob.
func encodePayload(v any) ([]byte, error) { return wire.EncodePayload(v) }

// decodePayload deserializes a gob payload into v, after the nesting scan.
func decodePayload(data []byte, v any) error { return wire.DecodePayload(data, v) }

// WritePTYOutput writes PTY output in optimized binary format.
func WritePTYOutput(w io.Writer, ptyID string, data []byte) error {
	return wire.WritePTYOutput(w, ptyID, data)
}

// WritePTYInput writes PTY input in optimized binary format.
func WritePTYInput(w io.Writer, ptyID string, data []byte) error {
	return wire.WritePTYInput(w, ptyID, data)
}

// writePTYFrame writes one binary PTY frame in a single Write.
func writePTYFrame(w io.Writer, msg MessageType, ptyID string, data []byte) error {
	return wire.WritePTYFrame(w, msg, ptyID, data)
}

// ParseBinaryPTYMessage parses a binary PTY message (Input or Output).
func ParseBinaryPTYMessage(payload []byte) (ptyID string, data []byte, err error) {
	return wire.ParseBinaryPTYMessage(payload)
}

// readMessageBody reads the header and payload after the length prefix has
// already been consumed from r, refusing a frame over its type's limit.
func readMessageBody(r io.Reader, totalLen uint32, limit func(MessageType) uint32) (*Message, error) {
	return wire.ReadMessageBody(r, totalLen, limit)
}

// readFramePayload reads n payload bytes into memory that grows as they
// arrive.
func readFramePayload(r io.Reader, n int) ([]byte, error) { return wire.ReadFramePayload(r, n) }
