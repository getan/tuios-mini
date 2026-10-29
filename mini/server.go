// Package mini embeds the tuios daemon core golder needs.
//
// It is the retained core of the tuios-mini fork: sessions, PTYs, windows and
// the control verbs golder speaks. The daemon it starts runs the local-only
// profile (session.ProfileLocalOnly): one socket, no federation links, no herdr
// protocol socket, no ssh-agent or display following, and none of the watchers
// and followers behind them. Start returns only after the daemon is listening
// and the requested session exists, so a caller can dial immediately.
//
// The standalone product's own packages are still linked — the boundary is what
// the daemon starts, not what the linker includes. docs/M4.md and
// mini/imports_test.go state which packages are on each side.
package mini

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/getan/tuios-mini/internal/config"
	"github.com/getan/tuios-mini/internal/guestenv"
	"github.com/getan/tuios-mini/internal/session"
	"github.com/getan/tuios-mini/mini/control"
)

const (
	DefaultSession = "mini"
	DefaultWidth   = 80
	DefaultHeight  = 24
	DefaultVersion = "mini-dev"
)

// Config is everything the embedded daemon may read from its host. There is no
// implicit user config: the caller owns the socket, the state, the shell, the
// session and the policy. Every field is a plain value, so a caller can build a
// Config without linking any of the daemon's types.
//
// Two daemons started from two Configs share nothing. Their own files live in
// their own StateDir, their log goes to their own LogFile, and their panes get
// their own environment, so two workspaces can both have a session named "work"
// without either seeing the other's state, history or log.
type Config struct {
	SocketPath string
	Session    string
	Shell      string
	Cwd        string
	Width      int
	Height     int
	Version    string
	// LogFile is this daemon's log file, opened by this Server and owned by it:
	// the file gets this daemon's own lines and is closed by Stop. Empty means
	// no file, and nothing is written anywhere: the embedded daemon never takes
	// over the process's standard logger, and never writes into the standalone
	// daemon's log.
	LogFile string

	// HarnessDir is the one directory this workspace's daemon reads extra
	// harness manifests from, in place of the machine's tuios configuration.
	// Empty, the default, means the manifests compiled into the binary and
	// nothing else: this daemon never reads TUIOS_HARNESS_DIR,
	// XDG_CONFIG_HOME/tuios/harnesses or any other path a standalone tuios on
	// the same machine was configured with. A caller that wants its own harness
	// rules points this at a folder it owns, such as one under StateDir.
	HarnessDir string

	// StateDir is the directory this daemon saves everything it keeps across a
	// restart under: session state, pane history, the Inbox, the outbox, review
	// notes, the applied-grants record, push registrations and the transfer
	// journal. Empty means a private directory beside the socket
	// (<socket path>.state), so an embedded daemon never shares the standalone
	// tuios state, and two embedded daemons never share each other's.
	//
	// Set it explicitly to put the state where a later daemon can find it after
	// /tmp is cleared, which is what a workspace that survives a reboot needs.
	StateDir string
	// PaneEnv is the environment every pane of this daemon is started with, on
	// top of the daemon's own environment and under the variables the daemon
	// stamps itself. It is applied per pane, never to this process: the caller's
	// own environment and any other daemon in the process are untouched.
	//
	// A variable the daemon owns (TUIOS_ENV, TUIOS_SOCKET, TUIOS_SERVER,
	// TUIOS_SESSION, TUIOS_HOST, TUIOS_WINDOW_ID, TUIOS_PANE_ID,
	// TUIOS_PANE_TOKEN, TUIOS_PANE_GRANTS, TUIOS_RESTORED, TUIOS_PANE_TTY) is
	// refused at Start with an error: a caller cannot hand a pane an identity or
	// a token it does not have. HERDR_* variables are dropped, because this
	// daemon speaks no herdr protocol.
	PaneEnv []string
	// Command is the argv of the session's first pane: the program the pane
	// runs instead of a shell. Empty runs Shell. The daemon starts it, so Cwd
	// and PaneEnv still apply.
	Command []string
	// PermissionsMode is what a pane started with no grants of its own may do
	// through the control socket: config.PaneModeOpen ("open", the default) or
	// config.PaneModeStrict ("strict"). Open gives such a pane admin, which is
	// everything a pane could do before grants existed. Strict gives it exactly
	// Grants. Any other value is refused rather than guessed at.
	PermissionsMode string
	// Grants is what a pane holds under strict mode when it was started with no
	// grants of its own. Empty means config.DefaultStrictGrants (read, write and
	// fan), which covers the verbs golder drives a pane with inside its own
	// session. A name outside config.PaneGrantNames is refused.
	Grants []string
	// AgentCheckpoints turns on the checkpoint of a pane's git work tree taken
	// when its agent finishes a turn. Off by default: a workspace saves no
	// checkpoints unless its caller asks, and nothing is written to the work
	// tree or to git behind the caller's back.
	AgentCheckpoints bool
	// AgentDetection turns on the foreground-process agent detector, which
	// marks a pane as running an agent by looking at what it runs. Off by
	// default: an embedded daemon reports only what its panes tell it.
	AgentDetection bool
	// StopWhenEmpty ends this server when its last session is explicitly
	// deleted. A UI detach does not delete a session and leaves panes alive.
	StopWhenEmpty bool
}

// Server owns one daemon and the session Start created.
type Server struct {
	daemon  *session.Daemon
	socket  string
	session string
	state   string
	log     *instanceLog

	once sync.Once
	done chan struct{}
}

func (c Config) withDefaults() Config {
	if c.Session == "" {
		c.Session = DefaultSession
	}
	if c.Version == "" {
		c.Version = DefaultVersion
	}
	if c.Width <= 0 {
		c.Width = DefaultWidth
	}
	if c.Height <= 0 {
		c.Height = DefaultHeight
	}
	if c.Shell == "" {
		c.Shell = os.Getenv("SHELL")
	}
	if c.Shell == "" {
		c.Shell = "sh"
	}
	if c.Cwd == "" {
		if wd, err := os.Getwd(); err == nil {
			c.Cwd = wd
		}
	}
	// The state a daemon keeps across a restart defaults to a directory beside
	// its socket: private to this daemon, and short, since /tmp and the runtime
	// folder are where an unconfigured caller ends up.
	if c.StateDir == "" && c.SocketPath != "" {
		c.StateDir = filepath.Join(filepath.Dir(c.SocketPath), filepath.Base(c.SocketPath)+".state")
	}
	c.PermissionsMode = strings.ToLower(strings.TrimSpace(c.PermissionsMode))
	if c.PermissionsMode == "" {
		c.PermissionsMode = config.PaneModeOpen
	}
	return c
}

// validate refuses a Config the daemon would have to guess at, before anything
// is created. Every refusal names the field, the value and what to do instead:
// a caller that gets one of these back has a bug, not a mystery.
func (c Config) validate() error {
	if c.SocketPath == "" {
		return errors.New("mini: SocketPath is required")
	}
	if err := checkSocketPath(c.SocketPath); err != nil {
		return err
	}
	if !slices.Contains(config.PaneModes, c.PermissionsMode) {
		return fmt.Errorf("mini: PermissionsMode is %q, which is not a mode: use %q (every pane holds admin) or %q (a pane holds exactly Grants)",
			c.PermissionsMode, config.PaneModeOpen, config.PaneModeStrict)
	}
	if _, unknown := config.CanonicalPaneGrants(c.Grants); len(unknown) > 0 {
		return fmt.Errorf("mini: Grants names %s, which is not a grant: use any of %s",
			strings.Join(unknown, ", "), strings.Join(config.PaneGrantNames, ", "))
	}
	// A caller cannot describe a pane it is not: the identity and token
	// variables are the daemon's to set. The herdr variables are dropped
	// instead of refused, since asking for them is a mistake about a protocol
	// rather than an attempt to widen what a pane is.
	var identity []string
	_, refused := guestenv.FilterPaneEnv(c.PaneEnv)
	for _, name := range refused {
		if guestenv.IsPaneIdentityVar(name) {
			identity = append(identity, name)
		}
	}
	if len(identity) > 0 {
		return fmt.Errorf("mini: PaneEnv cannot set %s: those are variables the daemon stamps on a pane itself, and a caller cannot give a pane an identity, a token or a grant it does not have. Remove them from PaneEnv",
			strings.Join(identity, ", "))
	}
	if len(c.Command) > 0 && c.Command[0] == "" {
		return errors.New("mini: Command[0] is the program the first pane runs and cannot be empty")
	}
	return nil
}

// permissions is the daemon's [agents.permissions] as a plain value.
func (c Config) permissions() config.ResolvedPermissions {
	return config.PermissionsConfig{Mode: c.PermissionsMode, Grants: c.Grants}.Resolve()
}

// Start runs a daemon on cfg.SocketPath and creates cfg.Session with one
// window (or cfg.Command, when it names one).
//
// The daemon is stopped when ctx is done, or by Stop. Start returns an error
// and leaves nothing running when ctx is already done, when the daemon refuses
// its socket, or when the session cannot be created; a half-started daemon is
// never handed back.
func Start(ctx context.Context, cfg Config) (*Server, error) {
	cfg = cfg.withDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("mini: start cancelled before the daemon started: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfg.SocketPath), 0o700); err != nil {
		return nil, fmt.Errorf("mini: create socket directory: %w", err)
	}
	if err := os.MkdirAll(cfg.StateDir, 0o700); err != nil {
		return nil, fmt.Errorf("mini: create state directory %s: %w", cfg.StateDir, err)
	}

	// The log file is this instance's, opened here and closed by Stop. It is
	// not the process's log: the standard library logger is left exactly as the
	// embedding program set it, and two daemons in one process write two files.
	var log *instanceLog
	if cfg.LogFile != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.LogFile), 0o700); err != nil {
			return nil, fmt.Errorf("mini: create log directory for %s: %w", cfg.LogFile, err)
		}
		var err error
		if log, err = openInstanceLog(cfg.LogFile, cfg.Version, cfg.SocketPath); err != nil {
			return nil, err
		}
	}

	s := &Server{socket: cfg.SocketPath, session: cfg.Session, state: cfg.StateDir, log: log, done: make(chan struct{})}

	dc := session.DaemonConfigFromUser(nil)
	dc.Version = cfg.Version
	dc.SocketPath = cfg.SocketPath
	dc.StateDir = cfg.StateDir
	// A workspace is panes, sessions and the control socket. The profile is what
	// keeps the standalone product out of the embedding process: no .link or
	// .link-human socket beside this one, no herdr socket, no ssh-agent or
	// display following, and none of the watchers and followers behind them.
	dc.Profile = session.ProfileLocalOnly
	// Harness rules are the same kind of thing. The daemon reads the bundled
	// manifests, and a folder this caller named, but never the user harness
	// directory a standalone tuios on the machine was configured with: those
	// rules are about another program's panes.
	if cfg.HarnessDir != "" {
		dc.HarnessDir = cfg.HarnessDir
	} else {
		dc.BundledHarnessesOnly = true
	}
	dc.PaneEnv = cfg.PaneEnv
	// The embedded daemon owns its log file rather than taking over the
	// process's: nothing here calls log.SetOutput.
	dc.LogFile = ""
	if log != nil {
		dc.LogSink = log.write
	}
	dc.DisableAutoRestore = true
	// A workspace reached over SSH starts its cleanup after the first frame.
	// The sweeps walk the state root, which may be a network home; none of them
	// is needed to create the session or draw it.
	dc.DeferLocalMaintenance = sshTerminalEnvironment()
	dc.DisableSignalHandling = true
	dc.PreferredShell = cfg.Shell
	if cfg.StopWhenEmpty {
		dc.OnEmpty = func() { go s.Stop() }
	}
	// A daemon of the agent-driven product by default; a workspace of panes by
	// default here. Checkpoints write to the caller's work tree and git, the
	// detector guesses, and the herdr protocol socket is a file beside the
	// daemon's that nothing asked for. All three stay off unless asked for.
	dc.Permissions = cfg.permissions()
	dc.Checkpoints = config.CheckpointsConfig{Enabled: ptr(cfg.AgentCheckpoints)}
	dc.AgentAutoDetect = ptr(cfg.AgentDetection)
	dc.ResumeAgents = "off"
	dc.HerdrProtocol = config.HerdrProtocolOff

	daemon := session.NewDaemon(dc)
	startupAt := time.Now()
	if err := daemon.Start(); err != nil {
		s.closeLog()
		return nil, fmt.Errorf("mini: start daemon: %w", err)
	}
	s.daemon = daemon
	if log != nil {
		log.write("info", fmt.Sprintf("startup: daemon.Start %s", time.Since(startupAt)))
		log.write("info", fmt.Sprintf("session %q requested on %s", cfg.Session, cfg.SocketPath))
	}
	createAt := time.Now()
	if err := s.createSession(ctx, cfg); err != nil {
		s.Stop()
		return nil, err
	}
	if log != nil {
		log.write("info", fmt.Sprintf("startup: createSession %s (total %s)", time.Since(createAt), time.Since(startupAt)))
	}
	// A cancel before the daemon finished starting still stops it: the select
	// takes ctx.Done or the server's own Done, so a Stop from a caller returns
	// this goroutine instead of leaving it parked on a context nobody cancels.
	go func() {
		select {
		case <-ctx.Done():
			s.Stop()
		case <-s.done:
		}
	}()
	return s, nil
}

// ptr returns a pointer to v, for the daemon options that distinguish "unset"
// from false.
func ptr[T any](v T) *T { return &v }

func sshTerminalEnvironment() bool {
	return os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != ""
}

func (s *Server) createSession(ctx context.Context, cfg Config) error {
	dialAt := time.Now()
	c, err := control.DialRetry(ctx, s.socket)
	if err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return fmt.Errorf("mini: connect to the daemon: %w", cerr)
		}
		return fmt.Errorf("mini: connect to the daemon on %s: %w", s.socket, err)
	}
	defer c.Close()
	if s.log != nil {
		s.log.write("info", fmt.Sprintf("startup: control dial %s", time.Since(dialAt)))
	}
	params := map[string]any{
		"name": cfg.Session, "width": cfg.Width, "height": cfg.Height, "window": true,
	}
	if cfg.Cwd != "" {
		params["cwd"] = cfg.Cwd
	}
	if len(cfg.Command) > 0 {
		params["command"] = cfg.Command
	}
	callAt := time.Now()
	if _, err := c.Call(ctx, "new-session", params); err != nil {
		var de *control.Error
		if errors.As(err, &de) && de.Code == "session_exists" {
			return nil
		}
		return fmt.Errorf("mini: create session %q: %w", cfg.Session, err)
	}
	if s.log != nil {
		s.log.write("info", fmt.Sprintf("startup: new-session verb %s", time.Since(callAt)))
	}
	return nil
}

// Socket is the control socket the daemon listens on.
func (s *Server) Socket() string { return s.socket }

// Session is the session Start created.
func (s *Server) Session() string { return s.session }

// StateDir is the directory the daemon saves its state under: the Config's
// StateDir, or the private directory beside the socket that Start derived.
func (s *Server) StateDir() string { return s.state }

// LogFile is the file this daemon appends its own log to, empty when the
// Config named none.
func (s *Server) LogFile() string {
	if s == nil || s.log == nil {
		return ""
	}
	return s.log.path
}

// Done is closed after the daemon has stopped.
func (s *Server) Done() <-chan struct{} { return s.done }

// Stop stops the daemon and closes Done. It is safe to call more than once, and
// it does not return until the daemon has shut down.
func (s *Server) Stop() {
	if s == nil {
		return
	}
	s.once.Do(func() {
		if s.log != nil {
			s.log.write("info", "stopping the daemon")
		}
		if s.daemon != nil {
			s.daemon.Stop()
		}
		if s.log != nil {
			s.log.write("info", "the daemon stopped")
			s.closeLog()
		}
		close(s.done)
	})
}

// closeLog releases the log file, if this server opened one.
func (s *Server) closeLog() {
	if s == nil || s.log == nil {
		return
	}
	_ = s.log.close()
}
