package wire

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

// A PTY frame is a frame with the pane's id in a fixed 36-byte field, written
// as one buffer. The bytes are what a pane's output and the person's keystrokes
// travel in, so they are pinned here rather than read back through the parser.
func TestPTYFrameBytesAreUnchanged(t *testing.T) {
	const id = "0123456789abcdef"
	var buf bytes.Buffer
	if err := WritePTYOutput(&buf, id, []byte("hi")); err != nil {
		t.Fatal(err)
	}
	got := buf.Bytes()
	if want := PTYFrameHeaderLen; len(got) != want+2 {
		t.Fatalf("a two byte frame is %d bytes, want %d", len(got), want+2)
	}
	if n := binary.BigEndian.Uint32(got[:4]); n != uint32(2+36+2) {
		t.Fatalf("the frame announces %d bytes, want %d", n, 2+36+2)
	}
	if got[4] != byte(MsgPTYOutput) || got[5] != CodecGob {
		t.Fatalf("the header is type %d codec %d", got[4], got[5])
	}
	if string(got[6:6+len(id)]) != id {
		t.Fatalf("the pane id is %q", got[6:6+len(id)])
	}
	for i := 6 + len(id); i < PTYFrameHeaderLen; i++ {
		if got[i] != 0 {
			t.Fatalf("the id field is not padded from byte %d: % x", i, got[6:PTYFrameHeaderLen])
		}
	}
	if string(got[PTYFrameHeaderLen:]) != "hi" {
		t.Fatalf("the data is %q", got[PTYFrameHeaderLen:])
	}

	// Input goes out the same way, with its own type.
	buf.Reset()
	if err := WritePTYInput(&buf, id, []byte("k")); err != nil {
		t.Fatal(err)
	}
	if buf.Bytes()[4] != byte(MsgInput) {
		t.Fatalf("an input frame holds type %d", buf.Bytes()[4])
	}

	// An id longer than the field is cut to it, and a shorter one is padded:
	// both are what copying into a 36-byte slice did.
	buf.Reset()
	if err := WritePTYInput(&buf, strings.Repeat("x", 40), nil); err != nil {
		t.Fatal(err)
	}
	if string(buf.Bytes()[6:PTYFrameHeaderLen]) != strings.Repeat("x", 36) {
		t.Fatalf("a long id was not cut: %q", buf.Bytes()[6:PTYFrameHeaderLen])
	}
}

func TestParseBinaryPTYMessage(t *testing.T) {
	var buf bytes.Buffer
	if err := WritePTYOutput(&buf, "pane-1", []byte("data")); err != nil {
		t.Fatal(err)
	}
	frame := buf.Bytes()
	id, data, err := ParseBinaryPTYMessage(frame[6:])
	if err != nil {
		t.Fatal(err)
	}
	if id != "pane-1" || string(data) != "data" {
		t.Fatalf("the frame parsed as %q and %q", id, data)
	}

	// A payload of nothing but padding is an empty id, not 36 NUL bytes.
	if id, _, err := ParseBinaryPTYMessage(make([]byte, 40)); err != nil || id != "" {
		t.Fatalf("an all-padding id parsed as %q, %v", id, err)
	}
	// A payload too short to hold an id is refused.
	if _, _, err := ParseBinaryPTYMessage(make([]byte, 35)); err == nil {
		t.Fatal("a 35 byte payload parsed as a PTY message")
	}
}
