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
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/getan/tuios-mini/mini"
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
	stateDir := fs.String("state-dir", "", "directory the daemon saves state under (default: beside the socket, or the workspace's own)")
	project := fs.String("project", "", "project this workspace belongs to: its own socket, state, config and log (workspace mode)")
	nested := fs.Bool("nested", false, "allow a workspace inside tuios/tmux/zellij/golder")
	permissions := fs.String("permissions", "open", "what a pane may do through the socket with no grants of its own: open or strict")
	var grants stringList
	fs.Var(&grants, "grant", "a grant a pane holds under --permissions strict (repeatable): read, write, fan, respond, files, admin")
	var paneEnv stringList
	fs.Var(&paneEnv, "pane-env", "KEY=VALUE exported into every pane (repeatable); TUIOS_ identity and token variables are refused")
	checkpoints := fs.Bool("checkpoints", false, "save a checkpoint of a pane's git work tree when its agent finishes a turn")
	agentDetection := fs.Bool("agent-detection", false, "detect agents by looking at what a pane runs")
	showVersion := fs.Bool("version", false, "print the version and exit")

	if err := fs.Parse(args); err != nil {
		// --help is not a failure: the flag package has already printed the
		// usage this build has, and exiting 1 would make `tuios-mini --help`
		// look broken to every script that checks it.
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *showVersion {
		fmt.Println(version)
		return nil
	}
	// Everything after the flags is the session's first pane: `tuios-mini
	// workspace -- /bin/sh`. `version` on its own stays a version request.
	var command []string
	if fs.NArg() > 0 {
		if fs.NArg() == 1 && fs.Arg(0) == "version" {
			fmt.Println(version)
			return nil
		}
		command = fs.Args()
	}

	host := mini.DetectHost(os.Getenv)
	// A workspace's own variables are handed to its panes, never set on this
	// process: os.Setenv here would move the environment of the program that
	// started us, and of any other daemon running beside us.
	var workspaceEnv []string
	if mode == "workspace" {
		if host.Nested() && !*nested {
			return fmt.Errorf("a golder workspace inside %s would nest two multiplexers; use the host's own panes there, or pass --nested", host)
		}
		ns, err := mini.NamespaceFor(*project)
		if err != nil {
			return err
		}
		if *socket == "" {
			*socket = ns.SocketPath
		}
		if *logFile == "" {
			*logFile = ns.LogFile
		}
		if *stateDir == "" {
			*stateDir = ns.StateDir
		}
		// Mark the panes this workspace starts as a golder host, not a user's
		// tuios daemon. The tuios-style socket/session/pane variables still
		// come from the daemon, as they do in any host.
		workspaceEnv = append(workspaceEnv, "GOLDER_WORKSPACE=1")
		if *project != "" {
			workspaceEnv = append(workspaceEnv, "GOLDER_PROJECT="+*project)
		}
	} else if *socket == "" {
		*socket = defaultDaemonSocket()
	}

	ctx, stop := signal.NotifyContext(context.Background(), shutdownSignals()...)
	defer stop()

	srv, err := mini.Start(ctx, mini.Config{
		SocketPath:       *socket,
		Session:          *session,
		Shell:            *shell,
		Cwd:              *cwd,
		Width:            *width,
		Height:           *height,
		Version:          version,
		LogFile:          *logFile,
		StateDir:         *stateDir,
		PaneEnv:          append(append([]string{}, workspaceEnv...), paneEnv...),
		Command:          command,
		PermissionsMode:  *permissions,
		Grants:           grants,
		AgentCheckpoints: *checkpoints,
		AgentDetection:   *agentDetection,
	})
	if err != nil {
		return err
	}
	defer srv.Stop()

	// One JSON line on stdout is the readiness signal scripts wait for.
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
		"socket": srv.Socket(), "session": srv.Session(), "pid": os.Getpid(),
		"version": version, "mode": mode, "host": string(host),
		"state": srv.StateDir(), "log": srv.LogFile(), "project": *project,
	})
	<-ctx.Done()
	// Wait for the daemon itself, not only for the signal: Stop returns once the
	// daemon has shut down and its socket is gone, and Done says the same thing
	// to a caller that did not make the Stop call.
	srv.Stop()
	<-srv.Done()
	return nil
}

// stringList is a repeatable flag. A comma-separated value is split too, since
// both spellings are what people type.
type stringList []string

func (l *stringList) String() string { return strings.Join(*l, ",") }

func (l *stringList) Set(v string) error {
	for _, part := range strings.Split(v, ",") {
		if part = strings.TrimSpace(part); part != "" {
			*l = append(*l, part)
		}
	}
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
