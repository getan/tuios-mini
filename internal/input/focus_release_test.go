package input

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestKeyReleaseReturnsToItsPressOwnerAfterFocusMoves(t *testing.T) {
	for _, dialog := range []bool{false, true} {
		o, first, second := twoInputPanes(t)
		o.KeyboardFlags = hostReportsEvents
		o.Windows[0].Terminal.Write([]byte(pushEventTypes))
		o.Windows[1].Terminal.Write([]byte(pushEventTypes))
		HandleInput(tea.KeyPressMsg{Code: 'a', Text: "a"}, o)
		first.got = nil
		o.FocusWindow(1)
		o.ShowHelp = dialog
		HandleInput(tea.KeyReleaseMsg{Code: 'a'}, o)
		if string(first.got) != "\x1b[97;1:3u" || len(second.got) != 0 {
			t.Fatalf("release changed owner: first=%q second=%q", first.got, second.got)
		}
		first.got = nil
		HandleInput(tea.KeyReleaseMsg{Code: 'a'}, o)
		if len(first.got) != 0 || len(second.got) != 0 {
			t.Fatal("an unmatched release reached a pane")
		}
	}
}
