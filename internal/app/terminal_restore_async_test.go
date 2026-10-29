package app

import "testing"

// TestTerminalRestoreCanBeFetchedAndAppliedOutsideUpdate covers the split
// attach restore: the daemon round trip is a command, and applying the answer
// clears the pending flag on the Update goroutine. It deliberately uses a real
// daemon/client rig rather than a fake, because GetTerminalState is the wait
// this change moved off the event loop.
func TestTerminalRestoreCanBeFetchedAndAppliedOutsideUpdate(t *testing.T) {
	r := newRig(t, 1)
	m := r.m
	m.terminalRestoreGen++
	m.terminalRestorePending = true

	cmd := m.restoreTerminalStatesCmd()
	if cmd == nil {
		t.Fatal("restoreTerminalStatesCmd returned no command")
	}
	msg, ok := cmd().(terminalStatesMsg)
	if !ok {
		t.Fatalf("restore command returned %T", msg)
	}
	if len(msg.snapshots) == 0 {
		t.Fatal("the restore command returned no window snapshots")
	}
	m.applyTerminalStates(msg)
	if m.terminalRestorePending {
		t.Fatal("applying the snapshots left the restore pending")
	}
}
