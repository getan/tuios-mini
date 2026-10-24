//go:build darwin || dragonfly || freebsd || linux || solaris || aix

package workspace

import (
	"os"

	"github.com/charmbracelet/x/term"
	"golang.org/x/sys/unix"
)

// withoutHardTabs stops bubbletea from moving the cursor with HT, and returns
// the function that puts the terminal back the way it was.
//
// This is the standalone client's function (see cmd/tuios/hardtabs_unix.go),
// over the input this client was handed rather than over this process's
// stdin. bubbletea moves the cursor across unchanged cells with a hard tab
// whenever the tty's TABDLY is TAB0, which is the default everywhere, and tmux
// 3.6 and later record a tab that crosses only blank cells as a tab cell,
// written without the background the blanks had: a panel drawn on a background
// shows a hole of terminal ground where the tab crossed it.
//
// bubbletea reads the setting from the tty state it saves before raw mode, and
// has no option for it, so the state it saves says TAB3. Raw mode turns output
// processing off, so TAB3 expands nothing while the program runs; the original
// state comes back when it exits.
func withoutHardTabs(in *os.File) func() {
	if in == nil {
		return func() {}
	}
	fd := in.Fd()
	if !term.IsTerminal(fd) {
		return func() {}
	}
	orig, err := term.GetState(fd)
	if err != nil || orig.Oflag&unix.TABDLY != unix.TAB0 {
		return func() {}
	}
	changed := *orig
	changed.Oflag = changed.Oflag&^unix.TABDLY | unix.TAB3
	if err := term.SetState(fd, &changed); err != nil {
		return func() {}
	}
	return func() { _ = term.SetState(fd, orig) }
}
