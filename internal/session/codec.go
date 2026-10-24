package session

import (
	"encoding/gob"
	"reflect"

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

	// These names predate the fork's module path. Moving Go source must not
	// change the names sent for values carried through an interface.
	registerSessionPayload(HelloPayload{})
	registerSessionPayload(WelcomePayload{})
	registerSessionPayload(AttachPayload{})
	registerSessionPayload(AttachedPayload{})
	registerSessionPayload(NewPayload{})
	registerSessionPayload(SessionInfo{})
	registerSessionPayload(SessionListPayload{})
	registerSessionPayload(KillPayload{})
	registerSessionPayload(SessionEndedPayload{})
	registerSessionPayload(ResizePayload{})
	registerSessionPayload(ErrorPayload{})
	registerSessionPayload(CreatePTYPayload{})
	registerSessionPayload(PTYCreatedPayload{})
	registerSessionPayload(ClosePTYPayload{})
	registerSessionPayload(ResizePTYPayload{})
	registerSessionPayload(PTYResizedPayload{})
	registerSessionPayload(SubscribePTYPayload{})
	registerSessionPayload(GetTerminalStatePayload{})
	registerSessionPayload(TerminalStatePayload{})

	// Session state types
	registerSessionPayload(SessionState{})
	registerSessionPayload(WindowState{})
	// Under the name gob.Register gave it while it was declared in this
	// package: the package path and the type name. gob sends the name of a
	// value held in an interface, so a peer of an older build and this one
	// have to agree on it.
	gob.RegisterName("github.com/Gaurav-Gosain/tuios/internal/session.TerminalState", TerminalState{})
	registerSessionPayload(SerializedBSPTree{})
	registerSessionPayload(SerializedBSPNode{})

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
	registerSessionPayload(ExecuteCommandPayload{})
	registerSessionPayload(RemoteCommandPayload{})
	registerSessionPayload(CommandResultPayload{})
	registerSessionPayload(GetLogsPayload{})
	registerSessionPayload(LogsDataPayload{})
}

func registerSessionPayload(value any) {
	const originalPackage = "github.com/Gaurav-Gosain/tuios/internal/session."
	gob.RegisterName(originalPackage+reflect.TypeOf(value).Name(), value)
}
