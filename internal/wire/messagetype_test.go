package wire

import (
	"strings"
	"testing"
	"time"
)

// The numbering is the wire format: a peer of another build numbers the types
// from the same list, and a value that moved would be read as another message.
// So the list is written out here, in order, and pinned. Adding a type means
// appending it here and in messagetype.go; changing one means a protocol
// version, not an edit.
var pinnedTypes = []struct {
	name string
	v    MessageType
}{
	{"MsgHello", MsgHello},
	{"MsgAttach", MsgAttach},
	{"MsgDetach", MsgDetach},
	{"MsgNew", MsgNew},
	{"MsgList", MsgList},
	{"MsgKill", MsgKill},
	{"MsgInput", MsgInput},
	{"MsgResize", MsgResize},
	{"MsgPing", MsgPing},
	{"MsgCreatePTY", MsgCreatePTY},
	{"MsgClosePTY", MsgClosePTY},
	{"MsgListPTYs", MsgListPTYs},
	{"MsgFocusPTY", MsgFocusPTY},
	{"MsgGetState", MsgGetState},
	{"MsgUpdateState", MsgUpdateState},
	{"MsgSubscribePTY", MsgSubscribePTY},
	{"MsgUnsubscribePTY", MsgUnsubscribePTY},
	{"MsgGetTerminalState", MsgGetTerminalState},
	{"MsgExecuteCommand", MsgExecuteCommand},
	{"MsgSendKeys", MsgSendKeys},
	{"MsgSetConfig", MsgSetConfig},
	{"MsgCapturePane", MsgCapturePane},
	{"MsgWelcome", MsgWelcome},
	{"MsgAttached", MsgAttached},
	{"MsgDetached", MsgDetached},
	{"MsgSessionList", MsgSessionList},
	{"MsgOutput", MsgOutput},
	{"MsgError", MsgError},
	{"MsgPong", MsgPong},
	{"MsgSessionEnded", MsgSessionEnded},
	{"MsgWindowChanged", MsgWindowChanged},
	{"MsgPTYList", MsgPTYList},
	{"MsgPTYCreated", MsgPTYCreated},
	{"MsgPTYClosed", MsgPTYClosed},
	{"MsgPTYOutput", MsgPTYOutput},
	{"MsgStateData", MsgStateData},
	{"MsgTerminalState", MsgTerminalState},
	{"MsgCommandResult", MsgCommandResult},
	{"MsgRemoteCommand", MsgRemoteCommand},
	{"MsgGetLogs", MsgGetLogs},
	{"MsgLogsData", MsgLogsData},
	{"MsgQueryWindows", MsgQueryWindows},
	{"MsgWindowList", MsgWindowList},
	{"MsgQuerySession", MsgQuerySession},
	{"MsgSessionInfo", MsgSessionInfo},
	{"MsgStateSync", MsgStateSync},
	{"MsgClientJoined", MsgClientJoined},
	{"MsgClientLeft", MsgClientLeft},
	{"MsgSessionResize", MsgSessionResize},
	{"MsgForceRefresh", MsgForceRefresh},
	{"MsgRequestFullSync", MsgRequestFullSync},
	{"MsgResurrect", MsgResurrect},
	{"MsgPTYResized", MsgPTYResized},
	{"MsgAgentMail", MsgAgentMail},
	{"MsgHostsChanged", MsgHostsChanged},
	{"MsgReadDir", MsgReadDir},
	{"MsgDirListing", MsgDirListing},
	{"MsgClientFocus", MsgClientFocus},
	{"MsgLayoutTree", MsgLayoutTree},
	{"MsgTypeAtPrompt", MsgTypeAtPrompt},
	{"MsgPromptTyped", MsgPromptTyped},
	{"MsgClientGraphics", MsgClientGraphics},
	{"MsgMasterLayout", MsgMasterLayout},
	{"MsgClientActivity", MsgClientActivity},
	{"MsgWatchDir", MsgWatchDir},
	{"MsgDirChanged", MsgDirChanged},
	{"MsgSidebarVisibility", MsgSidebarVisibility},
	{"MsgSessionUsed", MsgSessionUsed},
	{"MsgSessionsChanged", MsgSessionsChanged},
	{"MsgPaneMark", MsgPaneMark},
}

func TestMessageTypeNumberingIsPinned(t *testing.T) {
	if len(pinnedTypes) != NumMessageTypes {
		t.Fatalf("the pinned list holds %d types and the protocol defines %d", len(pinnedTypes), NumMessageTypes)
	}
	for i, p := range pinnedTypes {
		// Numbered from 1 with no gaps, so the position in the list is the
		// value on the wire.
		if want := MessageType(i + 1); p.v != want {
			t.Errorf("%s is %d, and the wire numbers it %d: a peer of another build reads another message",
				p.name, p.v, want)
		}
	}
	if MsgPaneMark != 70 {
		t.Errorf("the last type is %d, and 70 is what a build that has no later type reads", MsgPaneMark)
	}
}

func TestEveryMessageTypeHasAName(t *testing.T) {
	for _, p := range pinnedTypes {
		name := MessageTypeName(p.v)
		if name == "" || strings.HasPrefix(name, "Unknown(") {
			t.Errorf("%s (%d) has no name: %q", p.name, p.v, name)
		}
		if want := strings.TrimPrefix(p.name, "Msg"); name != want && !strings.Contains(name, want) {
			t.Errorf("%s is named %q", p.name, name)
		}
	}
	// A reserved type keeps saying so: a log that shows one points at a peer
	// from another build.
	if got := MessageTypeName(MsgListPTYs); got != "Reserved(ListPTYs)" {
		t.Errorf("a reserved type is named %q", got)
	}
	if got := MessageTypeName(0); got != "Unknown(0)" {
		t.Errorf("type 0 is named %q", got)
	}
	if got := MessageTypeName(MsgPaneMark + 1); got != "Unknown(71)" {
		t.Errorf("a type past the last is named %q", got)
	}
}

// The numbers the protocol's invariants are built on. Each one is on the wire
// or is the size of something on it.
func TestWireConstantsArePinned(t *testing.T) {
	if CodecGob != 0 {
		t.Errorf("the gob codec byte is %d, and every frame already written says 0", CodecGob)
	}
	if CodecGobTagged != 2 {
		t.Errorf("the tagged gob codec byte is %d, and 1 stays reserved for the JSON that was", CodecGobTagged)
	}
	if ReqIDLen != 8 {
		t.Errorf("the request id is %d bytes, and a tagged frame's header is 14", ReqIDLen)
	}
	if CodecName != "gob" {
		t.Errorf("the welcome reports codec %q", CodecName)
	}
	if MaxFrameBytes != 16*1024*1024 {
		t.Errorf("the frame ceiling is %d bytes, and every peer refuses past 16 MiB", MaxFrameBytes)
	}
	if MaxGobNesting != 1024 || MaxGobTypes != 1024 {
		t.Errorf("the gob scan's bounds are %d levels and %d types", MaxGobNesting, MaxGobTypes)
	}
	if LargeFrame != 64<<10 || FirstFrameChunk != 4<<10 || FrameGrowStep != 4<<20 {
		t.Errorf("the charging sizes are %d, %d and %d", LargeFrame, FirstFrameChunk, FrameGrowStep)
	}
	if FrameCopies != 2 {
		t.Errorf("a large frame is charged %d times its size", FrameCopies)
	}
	if PersonBudgetBytes != 96<<20 || PeerBudgetBytes != 48<<20 {
		t.Errorf("the read budgets are %d and %d bytes", PersonBudgetBytes, PeerBudgetBytes)
	}
	if ReadBudgetWait != 2*time.Second || FrameBodyDeadline != 30*time.Second {
		t.Errorf("the read waits are %v and %v", ReadBudgetWait, FrameBodyDeadline)
	}
}
