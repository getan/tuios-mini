// Package mini embeds the tuios daemon core golder needs.
//
// It is the M1 slice of the tuios-mini fork: sessions, PTYs, windows and the
// control verbs golder speaks, without the TUI, the plugin host or the rest of
// the standalone product. Start returns only after the daemon is listening and
// the requested session exists, so a caller can dial immediately.
package mini

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/Gaurav-Gosain/tuios/internal/session"
)

const (
	DefaultSession = "mini"
	DefaultWidth   = 80
	DefaultHeight  = 24
	DefaultVersion = "mini-dev"
)

// Config is everything the embedded daemon may read from its host. There is no
// implicit user config: the caller owns the socket, the shell and the session.
type Config struct {
	SocketPath string
	Session    string
	Shell      string
	Cwd        string
	Width      int
	Height     int
	Version    string
	LogFile    string
}

// Server owns one daemon and the session Start created.
type Server struct {
	daemon  *session.Daemon
	socket  string
	session string

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
	return c
}

// Start runs a daemon on cfg.SocketPath and creates cfg.Session with one
// window. The daemon is stopped when ctx is done, or by Stop.
func Start(ctx context.Context, cfg Config) (*Server, error) {
	cfg = cfg.withDefaults()
	if cfg.SocketPath == "" {
		return nil, errors.New("mini: SocketPath is required")
	}
	if err := os.MkdirAll(filepath.Dir(cfg.SocketPath), 0o700); err != nil {
		return nil, fmt.Errorf("mini: create socket directory: %w", err)
	}

	dc := session.DaemonConfigFromUser(nil)
	dc.Version = cfg.Version
	dc.SocketPath = cfg.SocketPath
	dc.LogFile = cfg.LogFile
	dc.DisableAutoRestore = true
	dc.DisableSignalHandling = true
	dc.PreferredShell = cfg.Shell

	daemon := session.NewDaemon(dc)
	if err := daemon.Start(); err != nil {
		return nil, fmt.Errorf("mini: start daemon: %w", err)
	}
	s := &Server{daemon: daemon, socket: cfg.SocketPath, session: cfg.Session, done: make(chan struct{})}
	if err := s.createSession(ctx, cfg); err != nil {
		s.Stop()
		return nil, err
	}
	go func() {
		<-ctx.Done()
		s.Stop()
	}()
	return s, nil
}

func (s *Server) createSession(ctx context.Context, cfg Config) error {
	c, err := dialRetry(ctx, s.socket)
	if err != nil {
		return err
	}
	defer c.Close()
	params := map[string]any{
		"name": cfg.Session, "width": cfg.Width, "height": cfg.Height, "window": true,
	}
	if cfg.Cwd != "" {
		params["cwd"] = cfg.Cwd
	}
	if _, err := c.Call(ctx, "new-session", params); err != nil {
		var de *Error
		if errors.As(err, &de) && de.Code == "session_exists" {
			return nil
		}
		return fmt.Errorf("mini: create session %q: %w", cfg.Session, err)
	}
	return nil
}

// Socket is the control socket the daemon listens on.
func (s *Server) Socket() string { return s.socket }

// Session is the session Start created.
func (s *Server) Session() string { return s.session }

// Done is closed after the daemon has stopped.
func (s *Server) Done() <-chan struct{} { return s.done }

// Stop stops the daemon and closes Done. It is safe to call more than once, and
// it does not return until the daemon has shut down.
func (s *Server) Stop() {
	if s == nil {
		return
	}
	s.once.Do(func() {
		s.daemon.Stop()
		close(s.done)
	})
}
