package wire

import (
	"bytes"
	"encoding/gob"
	"reflect"
	"strings"
	"testing"
)

// testPayload is what a caller's own payload looks like from here: this
// package never registers a type, and a payload it did not register cannot
// travel in an interface.
type testPayload struct {
	Name  string
	Count int
}

func TestPayloadRoundTrip(t *testing.T) {
	data, err := EncodePayload(&testPayload{Name: "pane", Count: 3})
	if err != nil {
		t.Fatal(err)
	}
	var got testPayload
	if err := DecodePayload(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "pane" || got.Count != 3 {
		t.Fatalf("the payload read back as %+v", got)
	}

	// An empty payload leaves the value untouched: a message of no payload is
	// a message, not an error.
	got = testPayload{Name: "left alone", Count: 9}
	if err := DecodePayload(nil, &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "left alone" || got.Count != 9 {
		t.Fatalf("an empty payload wrote %+v", got)
	}

	// NewMessage and ParsePayload are the same two halves.
	msg, err := NewMessage(MsgTerminalState, &testPayload{Name: "x", Count: 1})
	if err != nil {
		t.Fatal(err)
	}
	if msg.Type != MsgTerminalState {
		t.Fatalf("the message holds type %d", msg.Type)
	}
	var back testPayload
	if err := msg.ParsePayload(&back); err != nil {
		t.Fatal(err)
	}
	if back.Name != "x" || back.Count != 1 {
		t.Fatalf("the message payload read back as %+v", back)
	}

	// A nil payload gives an empty message rather than an error.
	empty, err := NewMessage(MsgPing, nil)
	if err != nil || empty.Payload != nil {
		t.Fatalf("a message with no payload is %+v, %v", empty, err)
	}
}

// The nesting scan runs before encoding/gob sees the bytes, because a payload
// deep enough to overflow the stack during gob's decode is fatal, not a panic
// the reader can recover from.
func TestDeepPayloadIsRefusedBeforeGob(t *testing.T) {
	// Registering the map is the caller's half of gob: a value held in an
	// interface has to be registered, and the session package registers its
	// own in its init.
	gob.Register(map[string]any{})

	// A chain of nested maps: every level is one more than the last, and each
	// is a value gob would decode by recursing.
	deep := func(levels int) []byte {
		v := any(map[string]any{})
		for range levels {
			v = map[string]any{"n": v}
		}
		data, err := EncodePayload(v)
		if err != nil {
			t.Fatalf("%d levels did not encode: %v", levels, err)
		}
		return data
	}

	// Positive control: a payload that nests less than the bound decodes.
	ok := deep(32)
	var sink map[string]any
	if err := DecodePayload(ok, &sink); err != nil {
		t.Fatalf("a payload of 32 levels did not decode: %v", err)
	}

	// Negative control: one past the bound is refused, and refused with the
	// scan's own error rather than gob's.
	tooDeep := deep(MaxGobNesting + 1)
	err := DecodePayload(tooDeep, &sink)
	if err == nil {
		t.Fatal("a payload deeper than the bound decoded")
	}
	if !strings.Contains(err.Error(), ErrGobNested.Error()) {
		t.Fatalf("a deep payload read as %v, want %v", err, ErrGobNested)
	}
	if !strings.HasPrefix(err.Error(), "gob decode: ") {
		t.Fatalf("the refusal reads %q, and every decode error is prefixed", err)
	}
}

func TestMalformedPayloadIsRefused(t *testing.T) {
	for _, data := range [][]byte{
		{0xff, 0xff, 0xff}, // a length no message holds
		{0x01},             // a type id with nothing behind it
		{0x7f, 0x00, 0x00}, // a value of a type the stream never defined
		append(make([]byte, 20), 0xff),
	} {
		var sink any
		if err := DecodePayload(data, &sink); err == nil {
			t.Fatalf("% x decoded", data)
		}
	}
	// A payload that is not gob at all says so through the same prefix.
	var sink testPayload
	err := DecodePayload([]byte("not gob"), &sink)
	if err == nil || !strings.HasPrefix(err.Error(), "gob decode: ") {
		t.Fatalf("a payload that is not gob read as %v", err)
	}
}

// A type defined too many times is refused as well: gob compiles a decoder for
// each type a stream defines, by recursion over the type's elements, so a
// chain of definitions in a field the receiver does not have is work it does
// before any value arrives.
func TestTypeChainIsRefused(t *testing.T) {
	// A slice of a slice of ... n deep is n type definitions, and a field the
	// receiver need not have. Built by reflection because the type is the
	// point: it exists only in this test.
	chain := func(n int) ([]byte, reflect.Type) {
		typ := reflect.TypeFor[int]()
		for range n {
			typ = reflect.SliceOf(typ)
		}
		rt := reflect.StructOf([]reflect.StructField{{Name: "Chain", Type: typ}})
		data, err := EncodePayload(reflect.New(rt).Interface())
		if err != nil {
			t.Fatalf("%d levels did not encode: %v", n, err)
		}
		return data, rt
	}

	// Positive control: a short chain is a payload like any other.
	data, rt := chain(8)
	if err := DecodePayload(data, reflect.New(rt).Interface()); err != nil {
		t.Fatalf("a short chain did not decode: %v", err)
	}

	// Negative control: past the bound the definitions alone are refused.
	data, rt = chain(MaxGobTypes + 2)
	err := DecodePayload(data, reflect.New(rt).Interface())
	if err == nil {
		t.Fatal("a chain past the type bound decoded")
	}
	if !strings.Contains(err.Error(), ErrGobTypes.Error()) {
		t.Fatalf("a chain past the type bound read as %v, want %v", err, ErrGobTypes)
	}
}

// The scan walks what it is given and refuses what it cannot follow, rather
// than passing it to gob to find out.
func TestNestingScanRefusesWhatItCannotFollow(t *testing.T) {
	if err := CheckGobNesting(nil); err == nil {
		t.Fatal("an empty payload passed the scan")
	}
	var sink any
	if err := DecodePayload([]byte{0x00}, &sink); err == nil {
		t.Fatal("a payload of one zero byte decoded")
	}
	// A gob value this package's own encoder never writes: a message length
	// past the end of the data.
	bad := append([]byte{0x7f, 0xff}, bytes.Repeat([]byte{0}, 4)...)
	if err := CheckGobNesting(bad); err == nil {
		t.Fatalf("% x passed the scan", bad)
	}
}

func TestGobRegistrationIsTheCallersJob(t *testing.T) {
	// This package registers no type: the payload DTOs and their registration
	// stay with the package that owns them, so no name on the wire is decided
	// here. A value held in an interface has to be registered by its caller,
	// and until it is, gob's refusal comes back wrapped like any other encode
	// error.
	type local struct{ A int }
	type carrier struct{ V any }
	if _, err := EncodePayload(carrier{V: local{A: 1}}); err == nil {
		t.Fatal("gob carried a value in an interface field of a type nobody registered")
	} else if !strings.HasPrefix(err.Error(), "gob encode: ") {
		t.Fatalf("the refusal reads %q", err)
	}

	gob.Register(local{})
	data, err := EncodePayload(carrier{V: local{A: 1}})
	if err != nil {
		t.Fatalf("a registered value did not encode: %v", err)
	}
	var got carrier
	if err := DecodePayload(data, &got); err != nil {
		t.Fatalf("a registered value did not decode: %v", err)
	}
	if v, ok := got.V.(local); !ok || v.A != 1 {
		t.Fatalf("the value read back as %#v", got.V)
	}
}
