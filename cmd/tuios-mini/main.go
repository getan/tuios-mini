// Command tuios-mini runs the embeddable tuios core as a foreground daemon.
//
// `daemon` is the M1 harness: one socket, one session, no TUI.
// `workspace` is the M3 entry point: it detects the terminal host, refuses a
// nested workspace unless asked, and uses golder's own paths. It still has no
// TUI; the retained TUI moves in during M4.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/Gaurav-Gosain/tuios/mini"
)

var version = "mini-dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "tuios-mini:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	mode := "daemon"
	if len(args) > 0 && (args[0] == "daemon" || args[0] == "workspace") {
		mode, args = args[0], args[1:]
	}

	fs := flag.NewFlagSet("tuios-mini", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	socket := fs.String("socket", "", "control socket path (default: mode-specific)")
	session := fs.String("session", mini.DefaultSession, "session to create")
	shell := fs.String("shell", "", "shell for the session's panes (default $SHELL)")
	cwd := fs.String("cwd", "", "working directory for the session")
	width := fs.Int("width", mini.DefaultWidth, "nominal columns")
	height := fs.Int("height", mini.DefaultHeight, "nominal rows")
	logFile := fs.String("log", "", "daemon log file")
	nested := fs.Bool("nested", false, "allow a workspace inside tuios/tmux/zellij")
	showVersion := fs.Bool("version", false, "print the version and exit")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *showVersion {
		fmt.Println(version)
		return nil
	}
	if fs.NArg() > 0 {
		if fs.NArg() == 1 && fs.Arg(0) == "version" {
			fmt.Println(version)
			return nil
		}
		return fmt.Errorf("usage: tuios-mini [daemon|workspace] [flags]")
	}

	host := mini.DetectHost(os.Getenv)
	if mode == "workspace" {
		if host.Nested() && !*nested {
			return fmt.Errorf("a golder workspace inside %s would nest two multiplexers; run `tuios-mini` there, or pass --nested", host)
		}
		if *socket == "" {
			*socket = mini.DefaultNamespace().SocketPath
		}
		if *logFile == "" {
			*logFile = mini.DefaultNamespace().LogFile
		}
		// Mark the panes this workspace starts as a golder host, not a user's
		// tuios daemon. The tuios-style socket/session/pane variables still
		// come from the daemon, as they do in any host.
		_ = os.Setenv("GOLDER_WORKSPACE", "1")
	} else if *socket == "" {
		*socket = defaultDaemonSocket()
	}

	ctx, stop := signal.NotifyContext(context.Background(), shutdownSignals()...)
	defer stop()

	srv, err := mini.Start(ctx, mini.Config{
		SocketPath: *socket,
		Session:    *session,
		Shell:      *shell,
		Cwd:        *cwd,
		Width:      *width,
		Height:     *height,
		Version:    version,
		LogFile:    *logFile,
	})
	if err != nil {
		return err
	}
	defer srv.Stop()

	// One JSON line on stdout is the readiness signal scripts wait for.
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
		"socket": srv.Socket(), "session": srv.Session(), "pid": os.Getpid(),
		"version": version, "mode": mode, "host": string(host),
	})
	<-ctx.Done()
	return nil
}

func defaultDaemonSocket() string {
	if path := os.Getenv("TUIOS_MINI_SOCKET"); path != "" {
		return path
	}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "tuios-mini.sock")
	}
	if dir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(dir, "tuios-mini", "daemon.sock")
	}
	return filepath.Join(os.TempDir(), "tuios-mini", "daemon.sock")
}
