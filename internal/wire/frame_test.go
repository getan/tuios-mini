package wire

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

// The frame layout, byte for byte. A peer writes these bytes and reads them,
// so the test reads the frame it writes as a slice rather than through this
// package's own reader: a change to the layout shows up here and not as a
// connection that hangs.
func TestFrameBytesAreUnchanged(t *testing.T) {
	t.Run("an untagged frame", func(t *testing.T) {
		var buf bytes.Buffer
		msg := &Message{Type: MsgList, Payload: []byte("hi")}
		if err := WriteMessage(&buf, msg); err != nil {
			t.Fatal(err)
		}
		got := buf.Bytes()
		want := []byte{
			0, 0, 0, 4, // length: type, codec and two payload bytes
			byte(MsgList), CodecGob,
			'h', 'i',
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("an untagged frame is % x, want % x", got, want)
		}

		back, err := ReadMessage(bytes.NewReader(got))
		if err != nil {
			t.Fatal(err)
		}
		if back.Type != MsgList || string(back.Payload) != "hi" || back.ReqID != 0 {
			t.Fatalf("the frame read back as %+v", back)
		}
	})

	t.Run("a tagged frame carries the id in the header", func(t *testing.T) {
		var buf bytes.Buffer
		if err := WriteMessage(&buf, &Message{Type: MsgList, Payload: []byte("hi"), ReqID: 42}); err != nil {
			t.Fatal(err)
		}
		got := buf.Bytes()
		want := []byte{
			0, 0, 0, 12, // length: type, codec, eight id bytes and two payload bytes
			byte(MsgList), CodecGobTagged,
			0, 0, 0, 0, 0, 0, 0, 42,
			'h', 'i',
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("a tagged frame is % x, want % x", got, want)
		}

		back, err := ReadMessage(bytes.NewReader(got))
		if err != nil {
			t.Fatal(err)
		}
		if back.ReqID != 42 || string(back.Payload) != "hi" || back.Type != MsgList {
			t.Fatalf("the frame read back as %+v", back)
		}
	})

	t.Run("an empty payload is the header alone", func(t *testing.T) {
		var buf bytes.Buffer
		if err := WriteMessage(&buf, &Message{Type: MsgPing}); err != nil {
			t.Fatal(err)
		}
		want := []byte{0, 0, 0, 2, byte(MsgPing), CodecGob}
		if !bytes.Equal(buf.Bytes(), want) {
			t.Fatalf("a frame of no payload is % x, want % x", buf.Bytes(), want)
		}
		back, err := ReadMessage(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		if back.Type != MsgPing || len(back.Payload) != 0 {
			t.Fatalf("the frame read back as %+v", back)
		}
	})
}

func TestMalformedHeadersAreRefused(t *testing.T) {
	header := func(totalLen uint32, body ...byte) []byte {
		b := make([]byte, 4, 4+len(body))
		binary.BigEndian.PutUint32(b, totalLen)
		return append(b, body...)
	}
	cases := []struct {
		name  string
		frame []byte
		want  string
	}{
		{"a length past 16 MiB", header(MaxFrameBytes + 1), "message too large"},
		{"a length of one byte", header(1, byte(MsgList)), "message too small"},
		{"a tag too short for its id", header(2+ReqIDLen-1, byte(MsgList), CodecGobTagged, 1, 2, 3, 4, 5, 6, 7), "tagged message too small"},
		{"a tagged frame with id zero", header(2+ReqIDLen, byte(MsgList), CodecGobTagged, 0, 0, 0, 0, 0, 0, 0, 0), "tagged message with request id 0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ReadMessage(bytes.NewReader(c.frame))
			if err == nil {
				t.Fatalf("%s was read", c.name)
			}
			if !bytes.Contains([]byte(err.Error()), []byte(c.want)) {
				t.Fatalf("%s read as %v, want a mention of %q", c.name, err, c.want)
			}
		})
	}

	// The other half of the last case: a frame with no id goes out untagged,
	// so a tagged zero is a frame no sender writes.
	var buf bytes.Buffer
	if err := WriteMessage(&buf, &Message{Type: MsgList, ReqID: 0}); err != nil {
		t.Fatal(err)
	}
	if buf.Bytes()[5] != CodecGob {
		t.Fatalf("a frame without a request id went out with codec byte %d", buf.Bytes()[5])
	}
}

// A frame over its type's limit is skipped unread and the stream is still in
// step: what a daemon does with a hello that announces 16 MiB, and the reason
// the limit is checked from the header.
func TestTypeLimitSkipsTheBodyAndKeepsTheStream(t *testing.T) {
	const limit = 64
	limitFor := func(MessageType) uint32 { return limit }

	var buf bytes.Buffer
	if err := WriteMessage(&buf, &Message{Type: MsgHello, Payload: make([]byte, limit)}); err != nil {
		t.Fatal(err)
	}
	refused := buf.Len()
	if err := WriteMessage(&buf, &Message{Type: MsgHello, Payload: []byte("next")}); err != nil {
		t.Fatal(err)
	}

	r := bytes.NewReader(buf.Bytes())
	var totalLen uint32
	if err := binary.Read(r, binary.BigEndian, &totalLen); err != nil {
		t.Fatal(err)
	}
	_, err := ReadMessageBody(r, totalLen, limitFor)
	var tooLarge *FrameTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("a frame over its type's limit read as %v", err)
	}
	if tooLarge.Type != MsgHello || tooLarge.Size != limit+2 || tooLarge.Limit != limit {
		t.Fatalf("the refusal says %+v", tooLarge)
	}
	if !bytes.Contains([]byte(tooLarge.Error()), []byte("Hello message of")) {
		t.Fatalf("the refusal reads %q, and its name comes from MessageTypeName", tooLarge.Error())
	}
	if refused == 0 {
		t.Fatal("the refused frame was empty")
	}

	// The next frame, which is under the limit, still reads: the refused
	// body was skipped.
	if err := binary.Read(r, binary.BigEndian, &totalLen); err != nil {
		t.Fatal(err)
	}
	msg, err := ReadMessageBody(r, totalLen, limitFor)
	if err != nil {
		t.Fatalf("the frame after a refused one did not read: %v", err)
	}
	if string(msg.Payload) != "next" {
		t.Fatalf("the frame after a refused one read as %q", msg.Payload)
	}

	// And the same frame over the ceiling of any frame is refused before its
	// head is even read.
	if _, err := ReadFrameHeader(bytes.NewReader(make([]byte, 8)), MaxFrameBytes+1); err == nil {
		t.Fatal("a frame past 16 MiB was accepted")
	}
}

// The payload grows with what arrives rather than from the header, so a sender
// that announces 16 MiB and sends little costs little. The allocation count is
// the allocator's, which sees a make whether or not it is touched.
func TestPayloadGrowsWithWhatArrives(t *testing.T) {
	const sent = 1 << 10
	var buf bytes.Buffer
	if err := WriteMessage(&buf, &Message{Type: MsgInput, Payload: make([]byte, sent)}); err != nil {
		t.Fatal(err)
	}
	frame := buf.Bytes()
	binary.BigEndian.PutUint32(frame[:4], MaxFrameBytes) // announce the ceiling

	allocs := testing.AllocsPerRun(20, func() {
		if _, err := ReadFramePayload(bytes.NewReader(frame[6:6+sent]), MaxFrameBytes-2); !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("a cut frame read as %v", err)
		}
	})
	if allocs > 8 {
		t.Fatalf("reading %d bytes of a frame that announced 16 MiB took %v allocations", sent, allocs)
	}
}

// The client's read path, with the deadline split between the wait for a frame
// and the arrival of its body.
func TestReadMessageBuffered(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	go func() {
		var buf bytes.Buffer
		if err := WriteMessage(&buf, &Message{Type: MsgPong, Payload: []byte("ok"), ReqID: 7}); err != nil {
			panic(err)
		}
		_, _ = client.Write(buf.Bytes())
	}()

	msg, err := ReadMessageBuffered(server, server, time.Second, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Type != MsgPong || msg.ReqID != 7 || string(msg.Payload) != "ok" {
		t.Fatalf("the frame read as %+v", msg)
	}
}
