package input

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Protocol conformance: associated text on an IME confirmation is the commit,
// not a request to type the physical Space/Enter key. Both PTY transports and
// legacy, modifyOtherKeys and Kitty panes must keep it. Control characters and
// real modified shortcuts must retain their ordinary meanings.
func TestIMEConfirmationKeepsAssociatedText(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  tea.KeyPressMsg
		all  string
	}{
		{"space", tea.KeyPressMsg{Code: tea.KeySpace, Text: "你好"}, "\x1b[32;1;20320:22909u"},
		{"enter", tea.KeyPressMsg{Code: tea.KeyEnter, Text: "你好"}, "\x1b[13;1;20320:22909u"},
		{"shift enter", tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift, Text: "你好"}, "\x1b[13;2;20320:22909u"},
		{"emoji sequence", tea.KeyPressMsg{Code: tea.KeySpace, Text: "👩‍💻"}, "\x1b[32;1;128105:8205:128187u"},
	} {
		for _, pane := range []struct{ name, flags string }{
			{"legacy", ""},
			{"modifyOtherKeys", "\x1b[>4;2m"},
			{"kitty disambiguate", "\x1b[=1;1u"},
			{"kitty all with text", "\x1b[=31;1u"},
		} {
			t.Run(tc.name+"/"+pane.name, func(t *testing.T) {
				want := tc.msg.Text
				if pane.name == "kitty all with text" {
					want = tc.all
				}
				if got := forwardNative(t, pane.flags, tc.msg); got != want {
					t.Errorf("native forwarded %q, want %q", got, want)
				}
				if got := forwardDaemon(t, pane.flags, tc.msg); got != want {
					t.Errorf("daemon forwarded %q, want %q", got, want)
				}
			})
		}
	}
}

func TestIMETextPriorityDoesNotChangeControlKeys(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  tea.KeyPressMsg
		want string
	}{
		{"space", tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}, " "},
		{"enter with control text", tea.KeyPressMsg{Code: tea.KeyEnter, Text: "\r"}, "\r"},
		{"enter", tea.KeyPressMsg{Code: tea.KeyEnter}, "\r"},
		{"shift enter", tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift}, "\x1b[13;2u"},
		{"ctrl space", tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl}, "\x1b[32;5u"},
		{"ctrl a", tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}, "\x1b[97;5u"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := forwardDaemon(t, "\x1b[=1;1u", tc.msg); got != tc.want {
				t.Fatalf("forwarded %q, want %q", got, tc.want)
			}
		})
	}
}
