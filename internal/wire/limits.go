package wire

import "fmt"

// MaxFrameBytes is the limit on any frame, whatever its type.
const MaxFrameBytes = 16 * 1024 * 1024

// FrameTooLargeError is a frame over the limit for its message type. The
// reader has skipped its body, so the stream is still in step and the
// connection can go on.
type FrameTooLargeError struct {
	Type  MessageType
	Size  uint32
	Limit uint32
	// ReqID is the request id the frame carried, so the refusal can be sent
	// as the answer to it. Zero for an untagged frame.
	ReqID uint64
}

func (e *FrameTooLargeError) Error() string {
	return fmt.Sprintf("%s message of %d bytes is over the %d byte limit for that type and was not read",
		MessageTypeName(e.Type), e.Size, e.Limit)
}

// FrameBusyError is a frame the daemon had no memory for: other large frames
// held the read budget for longer than it waits, or the pane still has a
// large input it has not read. The reader has skipped the body, so the stream
// is still in step, and the sender may send it again.
type FrameBusyError struct {
	Type MessageType
	Size uint32
	// ReqID is the request id the frame carried, so the refusal can be sent
	// as the answer to it. Zero for an untagged frame.
	ReqID uint64
}

func (e *FrameBusyError) Error() string {
	return fmt.Sprintf("%s message of %d bytes was not read because the daemon is busy with other large messages",
		MessageTypeName(e.Type), e.Size)
}
