package input

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/getan/tuios-mini/internal/app"
	"github.com/getan/tuios-mini/internal/config"
	"github.com/getan/tuios-mini/internal/terminal"
)

func withClickToType(t *testing.T, mode string) {
	t.Helper()
	prev := config.Global.ClickToType
	config.Global.ClickToType = mode
	t.Cleanup(func() { config.Global.ClickToType = prev })
}

func clickPane(o *app.OS, x, y int) {
	HandleInput(tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: y}, o)
	HandleInput(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: x, Y: y}, o)
}

func mouseModePane(t *testing.T, mode app.Mode) (*app.OS, *terminal.Window, *capturePty) {
	t.Helper()
	o, pty := osWithFocusedPane(t, config.DefaultConfig(), mode)
	win := o.Windows[0]
	win.DaemonMode = true
	win.DaemonWriteFunc = func(b []byte) error { _, err := pty.Write(b); return err }
	win.Terminal.Write([]byte("\x1b[?1002h\x1b[?1006h"))
	return o, win, pty
}

func TestOneContentClickFocusesAndTypesUnderEveryOldClickPolicy(t *testing.T) {
	for _, policy := range config.ClickToTypeModes {
		t.Run(policy, func(t *testing.T) {
			o, first, second := twoInputPanes(t)
			o.Settings.ClickToType = policy
			clickPane(o, 60, 10)
			HandleInput(press("b"), o)
			if string(second.got) != "b" || len(first.got) != 0 {
				t.Fatalf("click then typing: first=%q second=%q", first.got, second.got)
			}
		})
	}
}

func TestFirstContentClickReachesAMouseTrackingProgram(t *testing.T) {
	for _, policy := range config.ClickToTypeModes {
		t.Run(policy, func(t *testing.T) {
			o, win, pty := mouseModePane(t, app.WindowManagementMode)
			o.Settings.ClickToType = policy
			cx, cy := contentCell(win)
			clickPane(o, cx, cy)
			if len(pty.got) == 0 || o.PointerGestureActive() {
				t.Fatalf("first click did not reach the program: %q", pty.got)
			}
		})
	}
}

func TestContentDragSelectsRatherThanMovingThePane(t *testing.T) {
	o, _ := selectPane(t, "alpha bravo charlie")
	w := o.GetFocusedWindow()
	x, y := w.X, w.Y
	pressAt(o, 0, 0)
	dragTo(o, 10, 0)
	release(o, 10, 0)
	if w.X != x || w.Y != y || !w.HasSelection() || w.IsBeingManipulated {
		t.Fatal("content drag moved the frame or failed to select text")
	}
}

func TestTitleDragDoesNotLeakToAMouseTrackingProgram(t *testing.T) {
	o, win, pty := mouseModePane(t, app.WindowManagementMode)
	HandleInput(tea.MouseClickMsg{Button: tea.MouseLeft, X: win.X + 10, Y: win.Y}, o)
	if !o.PointerGestureActive() || !o.Dragging {
		t.Fatal("title did not grab the frame")
	}
	HandleInput(tea.MouseMotionMsg{Button: tea.MouseLeft, X: win.X + 15, Y: win.Y + 3}, o)
	HandleInput(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: win.X + 15, Y: win.Y + 3}, o)
	if len(pty.got) != 0 || o.PointerGestureActive() {
		t.Fatalf("frame drag leaked or stayed grabbed: %q", pty.got)
	}
	HandleInput(press("a"), o)
	if string(pty.got) != "a" {
		t.Fatalf("typing after title drag = %q", pty.got)
	}
}
