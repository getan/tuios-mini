package app

import (
	"testing"
	"time"

	"github.com/getan/tuios-mini/internal/terminal"
)

func inputTargetOS(t *testing.T) *OS {
	t.Helper()
	w := newTestWindow(t, "input-target", 80, 24)
	w.Workspace = 1
	m := newTestOS(w)
	m.CurrentWorkspace, m.FocusedWindow = 1, 0
	return m
}

func TestInputTargetFollowsFocusAndTemporaryOwners(t *testing.T) {
	m := inputTargetOS(t)
	check := func(want InputTarget) {
		t.Helper()
		if got := m.InputTarget(); got != want {
			t.Fatalf("input target = %s, want %s", got, want)
		}
	}
	check(InputPane)
	m.Mode = WindowManagementMode
	check(InputPane)
	m.SidebarFocused = true
	check(InputSidebar)
	m.ShowHelp = true
	check(InputOverlay)
	m.ShowHelp = false
	check(InputSidebar)
	m.SidebarFocused = false
	m.PrefixActive = true
	check(InputCommands)
	m.CancelPrefix()
	m.BeginPointerGesture()
	check(InputPointer)
	m.EndPointerGesture()
	check(InputPane)
	w := m.GetFocusedWindow()
	w.EnterCopyMode()
	check(InputSelection)
	w.ExitCopyMode()
	w.EnterCopyModeImplicit()
	check(InputPane)
	w.ExitCopyMode()
	m.CurrentWorkspace = 2
	check(InputDesktop)
	if m.KeyboardPane() != nil {
		t.Fatal("an empty workspace can type into the previous workspace")
	}
	m.CurrentWorkspace = 1
	check(InputPane)
	w.Minimized = true
	check(InputDesktop)
}

func TestPendingPaneInputCannotReturnToTheOldShell(t *testing.T) {
	m := inputTargetOS(t)
	m.beginPaneInputRequest()
	m.settlePendingPaneInput() // unrelated/stale sync, same old pane
	if m.InputTarget() != InputPending || m.KeyboardPane() != nil {
		t.Fatal("input reached the old pane during an asynchronous spawn")
	}
	m.beginPaneInputRequest() // two consecutive new-pane requests
	newPane := &terminal.Window{ID: "arrived-one", Workspace: 1}
	m.Windows = append(m.Windows, newPane)
	m.FocusedWindow = 1
	m.settlePendingPaneInput()
	if m.InputTarget() != InputPending {
		t.Fatal("first response released input meant for the second request")
	}
	m.Windows = append(m.Windows, &terminal.Window{ID: "arrived-two", Workspace: 1})
	m.FocusedWindow = 2
	m.settlePendingPaneInput()
	if m.KeyboardPane() != m.Windows[2] {
		t.Fatal("the newly focused pane did not acquire input")
	}
}

func TestPendingPaneInputCanBeCancelledOrTimeoutSafely(t *testing.T) {
	m := inputTargetOS(t)
	m.beginPaneInputRequest()
	m.FocusWindow(0) // deliberate reselect, even though focus did not move
	if m.InputTarget() != InputPane {
		t.Fatal("explicit focus did not cancel the pending destination")
	}
	m.beginPaneInputRequest()
	m.checkPendingPaneInput(m.pendingInputStarted.Add(11 * time.Second))
	if m.InputTarget() != InputDesktop || m.KeyboardPane() != nil || m.FocusedWindow != -1 {
		t.Fatal("spawn timeout silently sent input back to the old shell")
	}
	if len(m.Notifications) == 0 {
		t.Fatal("spawn timeout was not explained")
	}
}
