package wire

import (
	"bytes"
	"encoding/gob"
	"fmt"
)

// CodecGob is the value of the codec byte in every frame header. gob is the
// only payload encoding. The byte stays on the wire so frames keep their
// layout: senders write 0 and readers ignore it. The value 1 once meant JSON
// and stays reserved.
const CodecGob byte = 0

// CodecGobTagged marks a frame whose gob payload is preceded by an 8-byte
// big-endian request id: the id a client put on a request, and the id the
// daemon puts on its answer to it. See Message.ReqID. The id is never 0: a
// message with no id goes out untagged, and a reader refuses a tagged 0.
//
// A reader from before the value existed ignores the codec byte and would take
// the id for the start of the payload, so neither side writes one to a peer
// that has not said it reads them. A client writes tagged requests only to a
// daemon whose welcome set RequestIDs, and the daemon tags only the answer to
// a request that was itself tagged.
const CodecGobTagged byte = 2

// ReqIDLen is the size of the request id in a tagged frame.
const ReqIDLen = 8

// CodecName is what the welcome reports in WelcomePayload.Codec. Older peers
// inside the same protocol version still read that field.
const CodecName = "gob"

// Message is the base protocol message structure.
// Wire format (v2): [4 bytes length][1 byte type][1 byte codec][payload]
// The codec byte is 0 (gob) or 2 (gob after an 8-byte request id, see
// CodecGobTagged). The value 1 once meant JSON and stays reserved.
type Message struct {
	Type    MessageType
	Payload []byte
	// ReqID ties a reply to the request it answers. A client sets it on a
	// request it waits for, and the daemon copies it onto every message it
	// sends in answer, MsgError included. Zero is a message that answers
	// nothing in particular: a broadcast, a push, or anything to or from a
	// peer that does not read request ids. It travels in the frame header
	// (CodecGobTagged), outside the payload, so a reply is matched to its
	// request without decoding it.
	ReqID uint64
}

// EncodePayload serializes a message payload with gob.
func EncodePayload(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("gob encode: %w", err)
	}
	return buf.Bytes(), nil
}

// DecodePayload deserializes a gob payload into v. An empty payload leaves v
// untouched. A payload that nests too deeply for gob to decode safely is
// refused before gob sees it: see gobscan.go.
func DecodePayload(data []byte, v any) error {
	if len(data) == 0 {
		return nil
	}
	if err := CheckGobNesting(data); err != nil {
		return fmt.Errorf("gob decode: %w", err)
	}
	dec := gob.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("gob decode: %w", err)
	}
	return nil
}

// NewMessage creates a message with a gob-encoded payload. A nil payload
// gives an empty one.
func NewMessage(msgType MessageType, payload any) (*Message, error) {
	var data []byte
	var err error

	if payload != nil {
		data, err = EncodePayload(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to encode payload: %w", err)
		}
	}

	return &Message{
		Type:    msgType,
		Payload: data,
	}, nil
}

// ParsePayload decodes the message's gob payload into v. An empty payload
// leaves v untouched.
func (m *Message) ParsePayload(v any) error {
	return DecodePayload(m.Payload, v)
}
