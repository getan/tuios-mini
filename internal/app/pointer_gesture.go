package app

// A frame gesture owns the pointer until release. It is independent of pane
// keyboard focus: moving/resizing a pane never leaves the user in a new mode.

// BeginPointerGesture claims the pointer for a frame drag/resize. It does not
// change keyboard modes; ending the grab simply returns input to its target.
func (m *OS) BeginPointerGesture() {
	m.pointerGestureActive = true
}

// EndPointerGesture relinquishes the grab. Every completion/abort calls it;
// the next key derives its destination from focus without any mode restore.
func (m *OS) EndPointerGesture() {
	m.pointerGestureActive = false
}
