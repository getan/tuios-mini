package workspace

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/term"
)

// The terminal this client draws to has to be able to host it. The standalone
// client refuses three things before it takes the screen (see checkTerminal in
// cmd/tuios/diagnostics.go): a stream that is not a terminal at all, a
// terminal too small to lay out a session, and a TERM with no capabilities to
// render with. A host embedding this package gets the same refusals, because
// the alternative is a client that runs happily into a pipe and shows nobody
// anything, which reads as "the TUI did not start" with no reason attached.

// The minimum a session can be laid out in. Below this the dock, a border and
// one usable pane do not fit, and the client draws something broken rather
// than something small.
const (
	minTerminalWidth  = 40
	minTerminalHeight = 12
)

// These three are variables so a test can answer for a terminal without
// having one: a test binary's stdout is a pipe, and the check must still be
// testable in both directions. Nothing in this package writes to them.
var (
	isTerminalFile = func(f *os.File) bool { return term.IsTerminal(int(f.Fd())) }
	sizeOfFile     = func(f *os.File) (width, height int, err error) {
		return term.GetSize(int(f.Fd()))
	}
	termVariable = os.Getenv
)

// checkTerminal reports why in and out cannot host the client, before anything
// is read, written or connected.
func checkTerminal(in, out *os.File) error {
	if out == nil {
		return fmt.Errorf("workspace: no output terminal: Options.Output is nil and this process has no standard output")
	}
	if !isTerminalFile(out) {
		return fmt.Errorf("workspace: the output this client draws to is not a terminal, so the interface cannot be displayed. " +
			"Pass the terminal as Options.Output, or run the host from an interactive terminal; " +
			"for scripted use, drive the session with the daemon's verbs instead")
	}
	if in == nil {
		return fmt.Errorf("workspace: no input terminal: Options.Input is nil and this process has no standard input")
	}
	if !isTerminalFile(in) {
		return fmt.Errorf("workspace: the input this client reads from is not a terminal, so typing could never reach it. " +
			"Pass the terminal as Options.Input, or run the host from an interactive terminal")
	}
	// A size that cannot be read is not fatal and neither is zero: a pty
	// opened without a window size is unknown rather than tiny, and the
	// renderer sizes itself from the first resize instead.
	if width, height, err := sizeOfFile(out); err == nil && width > 0 && height > 0 {
		if width < minTerminalWidth || height < minTerminalHeight {
			return fmt.Errorf("workspace: the terminal is %dx%d, too small to render a session (minimum %dx%d); "+
				"resize the terminal or reduce the font size", width, height, minTerminalWidth, minTerminalHeight)
		}
	}
	switch termVariable("TERM") {
	case "":
		return fmt.Errorf("workspace: TERM is not set, so this client cannot tell what the terminal can render; " +
			"set a terminal type, for example TERM=xterm-256color")
	case "dumb":
		return fmt.Errorf("workspace: TERM is \"dumb\", which has no cursor movement or styling, so the interface cannot be drawn; " +
			"set a capable terminal type, for example TERM=xterm-256color")
	}
	return nil
}

// terminalSize is the size this client reports to the daemon: what the caller
// asked for, else the terminal's own, else the conventional 80x24.
//
// The session's size is the minimum over its attached clients, so this number
// is not a private detail: a placeholder here shrinks the session for
// everybody until the first resize arrives. That is why the fallback is last.
func (o Options) terminalSize(in, out *os.File) (width, height int) {
	if o.Width > 0 && o.Height > 0 {
		return o.Width, o.Height
	}
	width, height = 0, 0
	for _, f := range []*os.File{out, in} {
		if f == nil {
			continue
		}
		if w, h, err := sizeOfFile(f); err == nil && w > 0 && h > 0 {
			width, height = w, h
			break
		}
	}
	if o.Width > 0 {
		width = o.Width
	}
	if o.Height > 0 {
		height = o.Height
	}
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	return width, height
}

// quitOnSignals turns SIGINT and SIGTERM into a quit and returns the function
// that stops listening.
//
// It is deliberately less than what the standalone client arms (see
// cmd/tuios/signal.go): that one also ends the process when the event loop
// cannot carry the quit out, because the process is the client. Here the
// process belongs to the host, so the worst this does is ask. A wedged client
// is the host's to end, and it has the pid.
//
// The ask goes out on its own goroutine, as it does there: Program.Send waits
// for the event loop to read, and a loop stuck writing a frame to a terminal
// nobody drains is exactly the case a signal arrives in.
func quitOnSignals(p *tea.Program) func() {
	sigs := make(chan os.Signal, 2)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	stopped := make(chan struct{})
	go func() {
		for {
			select {
			case <-stopped:
				return
			case <-sigs:
				go p.Send(tea.QuitMsg{})
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			signal.Stop(sigs)
			close(stopped)
		})
	}
}
