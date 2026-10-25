// Package workspace runs the retained TUIOS client against a daemon socket
// the caller owns.
//
// It is the same client `tuios attach` runs, without the command: the model,
// the input handler, the keybinds, the terminal writers and the cleanup are
// the ones the standalone binary uses, and the only thing that differs is who
// decided the socket, the session, the config file and the terminal.
//
// A host that embeds this package starts the daemon itself (see package mini)
// and calls Run with the socket it started. Run never execs a binary, never
// starts a daemon, and never looks for one: an OSError of "no daemon at this
// socket" is the caller's to answer. It also never calls os.Exit. The process
// is the host's, so a signal, a cancellation and an exhausted context are
// reported back to the caller, which decides what the process does next.
//
// One client runs at a time. The client is built on process-wide state — the
// appearance globals, the config file the settings page saves, the theme, the
// log stream — and two of them in one process would each read the other's.
// Run therefore refuses a second concurrent call rather than interleaving two
// sessions in one terminal; see ErrAlreadyRunning.
//
// That state is not only the config file. The client reads and writes the
// person's sidebar, their launcher's history, their custom themes and glyph
// sets, their saved layouts and tapes, and it logs through the process's
// logger. Every one of those is scoped to a directory the host owns for the
// duration of a run — Options.StateDir — and put back when Run returns, so a
// workspace neither reads nor writes the person's tree and one project cannot
// inherit another's custom assets. See Options.open.
package workspace

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"

	"github.com/getan/tuios-mini/internal/app"
	"github.com/getan/tuios-mini/internal/config"
	"github.com/getan/tuios-mini/internal/input"
	"github.com/getan/tuios-mini/internal/session"
	"github.com/getan/tuios-mini/internal/terminal"
)

// ErrAlreadyRunning is what Run returns while another call to Run is in
// progress in this process. The client is not reentrant: see the package
// comment.
var ErrAlreadyRunning = errors.New("workspace: a client is already running in this process")

// ExitError reports that the client stopped for a reason that is not the
// person leaving: the session was killed, the daemon or the host link went
// away, or the daemon took this client off its session because the session
// was being shown inside itself.
//
// A quit and a detach are the two normal ends and return nil. A cancellation
// returns the context's error.
type ExitError struct {
	// Session is the session that was attached when the client stopped. It is
	// the name the client is in now, which is not the one asked for if the
	// client switched sessions.
	Session string
	// Reason is one of "session-killed", "daemon-lost", "host-lost" or
	// "nested-refused".
	Reason string
}

func (e *ExitError) Error() string {
	switch e.Reason {
	case "session-killed":
		return fmt.Sprintf("workspace: session %q was terminated while this client was attached", e.Session)
	case "daemon-lost":
		return fmt.Sprintf("workspace: the daemon connection was lost while attached to session %q", e.Session)
	case "host-lost":
		return fmt.Sprintf("workspace: the link to the host running session %q closed and did not come back", e.Session)
	case "nested-refused":
		return fmt.Sprintf("workspace: the daemon took this client off session %q because its output reached a pane of that session", e.Session)
	default:
		return fmt.Sprintf("workspace: the client stopped on session %q (%s)", e.Session, e.Reason)
	}
}

// Options is what a host supplies. Every field is a plain value: nothing here
// is a tuios internal type, so a caller can build it without importing
// anything under internal/.
type Options struct {
	// Socket is the daemon's unix socket. Required. It is dialled as given;
	// the environment is not consulted, and TUIOS_SOCKET is not set for the
	// life of the process.
	Socket string

	// Session is the session to attach to. Required, unless CreateSession is
	// false and the daemon is to pick: an empty name asks the daemon for its
	// own choice, which is what `tuios attach` does with no argument.
	Session string

	// CreateSession creates the session when it does not exist. The start
	// directory is Cwd.
	CreateSession bool

	// Version is what the handshake reports as this client's version. Empty is
	// allowed; the daemon records it against this client.
	Version string

	// ConfigPath is the config file this client reads, and the file the
	// settings page writes, saves and reloads. Required, and never the
	// person's own: a host owns its config tree, and a client that read the
	// user's file would write the user's file.
	//
	// The file is created on first use, with the drop-ins and includes beside
	// it, exactly as a standalone client's first run creates its own.
	ConfigPath string

	// StateDir is the directory this client keeps the rest of its own files
	// under: the sidebar's preferences, the launcher's history, its custom
	// themes and glyph sets, a saved layout, a tape, a crash report and its
	// debug log. Every one of them is the client's rather than the person's,
	// and none is read from or written to the user's own tuios directories.
	//
	// A host normally passes a directory inside its own tree — golder passes
	// <daemon StateDir>/client — so that two projects, and a workspace beside
	// the person's own tuios, share nothing. Empty derives "state" beside
	// ConfigPath, which keeps the tree private for a host that named one path;
	// it never falls back to the user's.
	//
	// The directory is created when it is missing.
	StateDir string

	// Cwd is the start directory of a session this client creates. It is sent
	// with the attach; the process's working directory is not changed.
	Cwd string

	// Width and Height are the size this client reports to the daemon before
	// the first frame. Zero asks the terminal for its size, and 80x24 is the
	// last resort, as in the standalone client.
	//
	// The session's size is the minimum over its attached clients, so a
	// placeholder here shrinks everyone until the first resize arrives. That
	// is why zero means "ask", not "80x24".
	Width, Height int

	// Debug turns on the diagnostic logs for this run, written under StateDir,
	// with the client's log lines diverted into them so they cannot land on the
	// frame. With it off the client's log lines are discarded for the same
	// reason: the standard library logger is the process's, and an in-process
	// daemon logging through it while a frame is drawn puts a line of text in
	// the middle of an escape sequence.
	//
	// TUIOS_DEBUG_INTERNAL is neither read nor set here. The switch is the
	// run's, not the process's: a run must not turn logging on for the host that
	// embeds it, or leave a workspace's lines in /tmp/tuios-debug.log for the
	// next workspace to find. The log stream is put back as it was when Run
	// returns.
	Debug bool

	// StartInTerminalMode starts the session in terminal mode on the focused
	// pane instead of window-management mode.
	StartInTerminalMode bool

	// DisableSignalHandling leaves SIGINT and SIGTERM to the host. Run's own
	// handling turns them into a quit, which ends the client and puts the
	// terminal back; it never exits the process.
	DisableSignalHandling bool

	// Output is the terminal the client draws to. Nil means os.Stdout.
	Output *os.File

	// Input is the terminal the client reads typed input from. Nil means
	// os.Stdin.
	Input *os.File

	// The appearance below is what this client draws, layered over the config
	// file the way the command line's interface flags are. These are the
	// [appearance] settings a host has reason to decide for itself, not the
	// whole file: anything not here comes from it.
	//
	// Each says what the tag of the same name says. Zero and empty mean "not
	// chosen", so the file's value stands.

	// ShowKeys turns the press-keys overlay on.
	ShowKeys bool
	// ASCIIOnly draws the chrome with ASCII instead of Nerd Font icons.
	ASCIIOnly bool
	// Theme is a theme id ("dracula", "nord", ...).
	Theme string
	// BorderStyle is rounded, normal, thick, double, hidden, block, ascii,
	// outer-half-block, inner-half-block or glyphs.
	BorderStyle string
	// DockbarPosition is top, bottom or hidden.
	DockbarPosition string
	// WindowTitlePosition is top, bottom or hidden.
	WindowTitlePosition string
	// HideWindowButtons hides the minimize/maximize/close controls.
	HideWindowButtons bool
	// HideScrollbar hides the scrollbar thumb on the border.
	HideScrollbar bool
	// NoAnimations turns off transitions.
	NoAnimations bool
	// SharedBorders shares borders between adjacent tiled windows.
	SharedBorders bool
}

// overrides is the appearance as the config overrides the command line's flags
// become. Same list, same meaning: ApplyOverrides layers these over the file
// and under nothing else.
func (o Options) overrides() config.Overrides {
	return config.Overrides{
		ASCIIOnly:           o.ASCIIOnly,
		BorderStyle:         o.BorderStyle,
		DockbarPosition:     o.DockbarPosition,
		HideWindowButtons:   o.HideWindowButtons,
		WindowTitlePosition: o.WindowTitlePosition,
		HideScrollbar:       o.HideScrollbar,
		SharedBorders:       o.SharedBorders,
		NoAnimations:        o.NoAnimations,
		ThemeName:           o.Theme,
	}
}

// running guards the process-wide state the client is built on. See the
// package comment.
var running atomic.Bool

// check refuses an options value that cannot run, before anything is opened or
// touched. Everything here is a mistake in the caller, so each message says
// which field.
func (o Options) check() error {
	if o.Socket == "" {
		return errors.New("workspace: Options.Socket is required; this client will not look for a daemon")
	}
	if o.ConfigPath == "" {
		return errors.New("workspace: Options.ConfigPath is required; this client will not read or write the user's own config")
	}
	if o.CreateSession && o.Session == "" {
		return errors.New("workspace: Options.CreateSession needs Options.Session to name the session to create")
	}
	if o.Width < 0 || o.Height < 0 {
		return fmt.Errorf("workspace: Options.Width and Height cannot be negative (%dx%d)", o.Width, o.Height)
	}
	return nil
}

// stdout and stdin are the terminal this client draws to and reads from.
func (o Options) stdout() *os.File {
	if o.Output != nil {
		return o.Output
	}
	return os.Stdout
}

func (o Options) stdin() *os.File {
	if o.Input != nil {
		return o.Input
	}
	return os.Stdin
}

// Run attaches to the daemon at opts.Socket, runs the client until it is asked
// to stop, and puts everything it touched back.
//
// It returns nil when the client was quit or detached from its session, an
// *ExitError when the session was killed, the daemon went away or the daemon
// took this client off the session, ctx.Err() when ctx ended the run, and a
// plain error when the client could not be prepared or the program failed.
// Every one of those paths runs the same cleanup: the session's state is
// synced back for a detach, the panes this client owns are closed, the
// terminal is reset, and the process-wide state Run set is restored.
//
// Cancelling ctx quits the client. So does SIGINT or SIGTERM, unless
// opts.DisableSignalHandling left those to the host. Neither exits the
// process: that decision is the host's.
func Run(ctx context.Context, opts Options) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := opts.check(); err != nil {
		return err
	}

	if !running.CompareAndSwap(false, true) {
		return ErrAlreadyRunning
	}
	defer running.Store(false)

	in, out := opts.stdin(), opts.stdout()
	// A screen that cannot host the client is far harder to diagnose once the
	// client has taken it, and a pipe would produce a running client nobody can
	// see or type into. Both are refused here, before anything is opened.
	if err := checkTerminal(in, out); err != nil {
		return err
	}

	// Everything this client reads and writes apart from the socket: its config
	// file, its state directory, its custom themes and glyph sets, and the log
	// stream. Opened here, before anything is loaded, and put back on every way
	// out of this function.
	runScope, err := opts.open()
	if err != nil {
		return err
	}
	defer runScope.close()

	// Written before the attach, so it has reached any pane it is going to
	// reach by the time the attach arrives. A workspace drawn inside a pane of
	// the very session it shows is a loop, and this is how the daemon knows:
	// see internal/session/nest_probe.go. A terminal that would print the
	// sequence instead gets none (NestProbeSafe).
	var probe session.NestProbe
	if session.NestProbeSafe(os.Getenv("TERM")) {
		probe = session.WriteNestProbe(out)
	}

	// The appearance globals are the process's, and this client overwrites
	// them. A host that runs one client and then goes back to its own screens
	// gets the ones it had.
	previousSettings := config.Global
	defer func() { config.Global = previousSettings }()

	userConfig := loadAndApply(opts)

	app.SetInputHandler(input.HandleInput)
	keybindRegistry := config.NewKeybindRegistry(userConfig)

	width, height := opts.terminalSize(in, out)

	client := session.NewTUIClient()
	// The person is where this process is: their agent socket, their display
	// and their terminal's answer to TERM all go with the attach, as they do
	// for `tuios attach`. A host that wants none of that says so by clearing
	// the environment before it calls Run.
	client.SSHAuthSock = os.Getenv("SSH_AUTH_SOCK")
	client.Display = session.ClientDisplayFromEnv()
	client.UseCallerTerm()
	client.SetNestProbe(probe)
	if opts.CreateSession {
		client.StartDir = opts.Cwd
	}

	clientCaps := app.ClientCapabilitiesOf(app.GetHostCapabilities())
	if err := client.ConnectSocket(opts.Socket, opts.Version, width, height, clientCaps); err != nil {
		return fmt.Errorf("workspace: %w", err)
	}

	state, err := client.AttachSession(opts.Session, opts.CreateSession, width, height)
	if err != nil {
		_ = client.Close()
		return fmt.Errorf("workspace: attach to session %q: %w", opts.Session, err)
	}
	client.StartReadLoop()

	prw := app.NewPostRenderWriter(out)
	initialOS := app.NewOS(app.OSOptions{
		Client:          app.ClientLocal,
		KeybindRegistry: keybindRegistry,
		UserConfig:      userConfig,
		ShowKeys:        opts.ShowKeys,
		// The size this client told the daemon, so the restored windows are
		// tiled into a real box rather than a zero one the first resize then
		// has to undo.
		Width:           width,
		Height:          height,
		IsDaemonSession: true,
		DaemonClient:    client,
		// The TUI's verbs must reach the daemon this host started, not the
		// default socket a standalone tuios would use.
		LocalSocket: opts.Socket,
		SessionName: client.SessionName(),
		// Only a local attach starts in terminal mode, and only when asked.
		StartInTerminalMode: opts.StartInTerminalMode,
		// One writer for the terminal: frames, kitty and sixel sequences all
		// serialize on it. Left nil, the passthroughs open their own /dev/tty
		// and nothing can order their writes against a frame.
		GraphicsOutput: prw,
	})
	initialOS.PostRenderWriter = prw
	// Sixel images follow the frame they sit on, in the same write.
	initialOS.ConnectFrameWriter(prw)

	// Everything the daemon sends an attached client, queued for Update.
	initialOS.WireDaemonClient(client)
	initialOS.RestoreAttachedSession(state)

	// The shared list, then the one option that is this transport's: the
	// writer every frame and every graphics sequence serializes on. A client
	// handed its own terminal also supplies the input typed on it.
	programOptions := append(app.ProgramOptions(), tea.WithOutput(prw))
	if opts.Input != nil {
		programOptions = append(programOptions, tea.WithInput(in))
	}
	p := tea.NewProgram(initialOS, programOptions...)
	initialOS.BindProgram(p)

	stopSignals := func() {}
	if !opts.DisableSignalHandling {
		stopSignals = quitOnSignals(p)
	}
	defer stopSignals()

	// A cancelled context is somebody outside asking this client to stop. The
	// same quit a Ctrl+C asks for, and the run reports the cancellation when
	// it returns.
	var cancelled atomic.Bool
	runDone := make(chan struct{})
	defer close(runDone)
	go func() {
		select {
		case <-ctx.Done():
			cancelled.Store(true)
			p.Send(tea.QuitMsg{})
		case <-runDone:
		}
	}()

	// See withoutHardTabs: tmux drops the background under a tab.
	restoreTabs := withoutHardTabs(in)
	finalModel, runErr := p.Run()
	restoreTabs()

	reason := app.ExitNormal
	// The client may have switched sessions since it attached, so the name to
	// report is the one it is in now.
	exitSession := client.SessionName()
	if exitSession == "" {
		exitSession = opts.Session
	}
	killed := false
	if finalOS, ok := finalModel.(*app.OS); ok {
		reason = finalOS.ExitReason
		if name := finalOS.SessionName; name != "" {
			exitSession = name
		}
		killed = finalOS.QuitRequested
		// Syncing state back is meaningful only for a detach, while the
		// session still exists. A quit already killed it, a kill from
		// elsewhere left no session to receive the state, and a lost
		// connection cannot carry the write, so skip it rather than block on a
		// dead socket on the way out.
		if reason == app.ExitNormal && !killed {
			finalOS.SyncStateToDaemon()
		}
		finalOS.DumpTickStats()
		// Cleanup closes the daemon client with everything else this session
		// owns.
		finalOS.Cleanup()
		// The OSC 7501 records this client left on the terminal.
		_, _ = out.WriteString(finalOS.HostProgramStatusClear())
	}
	// Idempotent, and the one close that happens when the model never came
	// back as an *app.OS.
	_ = client.Close()

	// The terminal this client drew to, not this process's stdout: a host that
	// handed over a pty must not find escape sequences on its own output.
	_ = terminal.WriteReset(out)

	if runErr != nil {
		return fmt.Errorf("workspace: program error: %w", runErr)
	}
	if cancelled.Load() {
		return ctx.Err()
	}
	return exitResult(exitSession, reason)
}

// exitResult is the reason the client stopped, as an error for the ones that
// are not a normal end.
func exitResult(sessionName string, reason app.ExitReason) error {
	var why string
	switch reason {
	case app.ExitNormal, app.ExitDetached:
		// A quit killed the session; a detach left it running. Both are the
		// client doing what it was asked to do.
		return nil
	case app.ExitSessionKilled:
		why = "session-killed"
	case app.ExitDaemonLost:
		why = "daemon-lost"
	case app.ExitHostLost:
		why = "host-lost"
	case app.ExitNestedRefused:
		why = "nested-refused"
	default:
		why = fmt.Sprintf("exit-reason-%d", int(reason))
	}
	return &ExitError{Session: sessionName, Reason: why}
}

// loadAndApply loads the config file this process was scoped to, applies the
// appearance seed and then the caller's appearance over it.
//
// It is the same sequence every entry point bootstraps through (see
// cmd/tuios/run.go): the file is the baseline and what the host asked for
// wins. It runs exactly once per client, because the appearance globals are
// process-wide.
func loadAndApply(opts Options) *config.UserConfig {
	userConfig, err := config.LoadUserConfig()
	if err != nil {
		log.Printf("Warning: Failed to load config, using defaults: %v", err)
		userConfig = config.DefaultConfig()
		// The log line above is wiped by the first frame. LoadWarnings reach
		// the TUI, so the person learns why none of the file applies.
		userConfig.LoadWarnings = append(userConfig.LoadWarnings,
			fmt.Sprintf("tuios cannot load config.toml and uses the default settings: %v", err))
	}

	// What this terminal can draw, from its locale and TERM. It only decides
	// the glyphs when the config chose no set and ASCII was not asked for.
	env, why := config.DetectGlyphEnv(os.Getenv)
	config.Global.GlyphEnv = env
	if why != "" {
		log.Printf("glyphs: %s, so the chrome is drawn with %s glyphs unless a glyph set or ASCII is chosen", why, env)
	}

	// Appearance globals are the baseline; the host's choices win.
	// LoadUserConfig does not apply globals itself, so this must run before
	// ApplyOverrides.
	config.ApplyAppearanceConfig(userConfig, &config.Global)
	config.ApplyOverrides(opts.overrides(), &config.Global)

	return userConfig
}
