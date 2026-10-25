//go:build unix

package session

import (
	"testing"
	"time"
)

// TestSessionStopPaysTheExitGraceOnce is the shutdown-cost bound. A pane whose
// process ignores SIGTERM costs ptyCloseGrace to close, and closing a session's
// panes one after another made that cost add up: ten such panes held the daemon
// for a second. Stop now asks every pane to leave before waiting on any, so the
// same panes share one grace.

// The panes in this test ignore SIGTERM on purpose, so every one of them would
// burn the full grace if the closes were serialised. Five of them would take
// 500ms that way; sharing the grace keeps the whole Stop near 100ms. The bounds
// are wide enough for a loaded machine and still far below the serial cost.
func TestSessionStopPaysTheExitGraceOnce(t *testing.T) {
	d, _ := startTestDaemonWith(t, &DaemonConfig{Profile: ProfileLocalOnly})

	sess, err := d.manager.CreateSession("bulk", &SessionConfig{}, 80, 24)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	const panes = 5
	stubborn := []string{"/bin/sh", "-c", `trap "" TERM; while :; do sleep 0.05; done`}
	for i := 0; i < panes; i++ {
		if _, err := sess.AddDaemonWindowWith(NewWindowOptions{Command: stubborn}, nil); err != nil {
			t.Fatalf("add pane %d: %v", i, err)
		}
	}

	// DeleteSession is the daemon's own teardown path: it stops the session and
	// takes it out of the manager, so the daemon's cleanup below does not stop it
	// a second time.
	start := time.Now()
	if err := d.manager.DeleteSession("bulk"); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	elapsed := time.Since(start)

	// It waited, so the panes really did ignore the signal and the measurement
	// is of the wait rather than of an instant exit.
	if elapsed < ptyCloseGrace/2 {
		t.Fatalf("Stop returned in %v, so the panes did not hold out for the grace and this test measured nothing", elapsed)
	}
	if limit := ptyCloseGrace + ptyCloseGrace/2; elapsed > limit {
		t.Fatalf("Stop took %v for %d panes that each ignore SIGTERM; one grace is %v, and %d serialised graces would be %v",
			elapsed, panes, ptyCloseGrace, panes, time.Duration(panes)*ptyCloseGrace)
	}
}
