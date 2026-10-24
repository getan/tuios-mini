package session

import (
	"bytes"
	"io"
	"testing"

	"github.com/getan/tuios-mini/internal/wire"
)

// The names this package re-exports are the wire's own, not copies of them.
// The compiler holds the first half of that: these assignments only build if
// Message is wire.Message and WriteMessage takes one. A quiet second copy in
// this package — the shape the extraction exists to remove — could not pass
// them.
var (
	_ = func(m *Message) *wire.Message { return m }
	_ = func(w io.Writer, m *wire.Message) error { return WriteMessage(w, m) }
	_ = func(t MessageType) wire.MessageType { return t }
)

// And the bytes, which the compiler says nothing about. A frame this package
// writes is the frame internal/wire reads, and the header is the layout every
// peer in the field already speaks.
func TestSessionFramesAreWireFrames(t *testing.T) {
	if MsgList != 5 || MsgPaneMark != 70 {
		t.Fatalf("the message numbering moved: List is %d and PaneMark is %d", MsgList, MsgPaneMark)
	}
	if wireCodecGob != 0 || wireCodecGobTagged != 2 || reqIDLen != 8 {
		t.Fatalf("the codec bytes moved: %d, %d, and an id of %d", wireCodecGob, wireCodecGobTagged, reqIDLen)
	}
	if maxFrameBytes != 16*1024*1024 {
		t.Fatalf("the frame ceiling is %d", maxFrameBytes)
	}

	var buf bytes.Buffer
	if err := WriteMessage(&buf, &Message{Type: MsgList, Payload: []byte("hi"), ReqID: 3}); err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0, 0, 0, 12, // type, codec, the eight id bytes and two payload bytes
		byte(MsgList), wireCodecGobTagged,
		0, 0, 0, 0, 0, 0, 0, 3,
		'h', 'i',
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("a tagged session frame is % x, want % x", buf.Bytes(), want)
	}

	// Read by the package that owns the framing, the frame is the same
	// message: one implementation, two names.
	back, err := wire.ReadMessage(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if back.Type != MsgList || back.ReqID != 3 || string(back.Payload) != "hi" {
		t.Fatalf("the frame read back as %+v", back)
	}
	if got := MessageTypeName(back.Type); got != wire.MessageTypeName(MsgList) || got != "List" {
		t.Fatalf("the type is named %q", got)
	}
}

// The protocol log is a seam as well: internal/wire writes and reads the
// frames, so if nothing installs this package's logger, every frame a session
// sends goes unlogged and no test of the log would notice.
func TestWireFrameLogIsTheProtocolLog(t *testing.T) {
	if wire.LogFrame == nil {
		t.Fatal("no frame logger is installed: internal/wire writes frames with the protocol log unwired")
	}

	var got []string
	previous := wire.LogFrame
	wire.LogFrame = func(direction string, msg *wire.Message) {
		got = append(got, direction+":"+MessageTypeName(msg.Type))
	}
	defer func() { wire.LogFrame = previous }()

	var buf bytes.Buffer
	if err := WriteMessage(&buf, &Message{Type: MsgPing}); err != nil {
		t.Fatal(err)
	}
	if _, err := wire.ReadMessage(&buf); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "SEND:Ping" || got[1] != "RECV:Ping" {
		t.Fatalf("the frame log saw %v", got)
	}
}
