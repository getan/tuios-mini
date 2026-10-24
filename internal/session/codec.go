package session

import (
	"encoding/gob"

	"github.com/getan/tuios-mini/internal/wire"
)

// The gob codec itself is internal/wire: the codec bytes, the payload encode
// and decode, and the nesting scan every decode passes through. What is here
// is this package's half of it, the registration of the payload types, which
// are this package's own.
//
// The registrations have to stay next to the types. gob sends the name of a
// value held in an interface — its package path and type name — and that name
// is on the wire, so a payload type that moved to another package would be
// renamed for every peer. (One name is already frozen from a package this code
// no longer lives in: see the TerminalState line in init.)

// Register all payload types with gob, and give internal/wire this package's
// protocol log.
// This must be called before any gob encoding/decoding.
func init() {
	wire.LogFrame = LogMessage

	// Protocol payloads
	gob.Register(HelloPayload{})
	gob.Register(WelcomePayload{})
	gob.Register(AttachPayload{})
	gob.Register(AttachedPayload{})
	gob.Register(NewPayload{})
	gob.Register(SessionInfo{})
	gob.Register(SessionListPayload{})
	gob.Register(KillPayload{})
	gob.Register(SessionEndedPayload{})
	gob.Register(ResizePayload{})
	gob.Register(ErrorPayload{})
	gob.Register(CreatePTYPayload{})
	gob.Register(PTYCreatedPayload{})
	gob.Register(ClosePTYPayload{})
	gob.Register(ResizePTYPayload{})
	gob.Register(PTYResizedPayload{})
	gob.Register(SubscribePTYPayload{})
	gob.Register(GetTerminalStatePayload{})
	gob.Register(TerminalStatePayload{})

	// Session state types
	gob.Register(SessionState{})
	gob.Register(WindowState{})
	// Under the name gob.Register gave it while it was declared in this
	// package: the package path and the type name. gob sends the name of a
	// value held in an interface, so a peer of an older build and this one
	// have to agree on it.
	gob.RegisterName("github.com/Gaurav-Gosain/tuios/internal/session.TerminalState", TerminalState{})
	gob.Register(SerializedBSPTree{})
	gob.Register(SerializedBSPNode{})

	// Map types used in payloads (gob requires explicit registration)
	gob.Register(map[int]string{})
	gob.Register(map[string]int{})
	gob.Register(map[int]*SerializedBSPTree{})
	gob.Register(map[string]any{})
	// Slice types used in command result Data field
	gob.Register([]map[string]any{})
	gob.Register([]int{})
	gob.Register([]string{})

	// Remote command payloads
	gob.Register(ExecuteCommandPayload{})
	gob.Register(RemoteCommandPayload{})
	gob.Register(CommandResultPayload{})
	gob.Register(GetLogsPayload{})
	gob.Register(LogsDataPayload{})
}
