// Command tuios-mini runs the embeddable tuios core as a foreground daemon.
//
// It is the M1 harness for the mini package: one socket, one session, no TUI.
// Golder dials the socket with TUIOS_SOCKET / TUIOS_SESSION, exactly as it does
// against a full tuios daemon.
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
	fs := flag.NewFlagSet("tuios-mini", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	socket := fs.String("socket", defaultSocket(), "control socket path")
	session := fs.String("session", mini.DefaultSession, "session to create")
	shell := fs.String("shell", "", "shell for the session's panes (default $SHELL)")
	cwd := fs.String("cwd", "", "working directory for the session")
	width := fs.Int("width", mini.DefaultWidth, "nominal columns")
	height := fs.Int("height", mini.DefaultHeight, "nominal rows")
	logFile := fs.String("log", "", "daemon log file")
	showVersion := fs.Bool("version", false, "print the version and exit")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *showVersion {
		fmt.Println(version)
		return nil
	}
	if fs.NArg() == 1 && fs.Arg(0) == "version" {
		fmt.Println(version)
		return nil
	}
	if fs.NArg() > 1 || (fs.NArg() == 1 && fs.Arg(0) != "daemon") {
		return fmt.Errorf("usage: tuios-mini [daemon] [flags]")
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
		"socket": srv.Socket(), "session": srv.Session(), "pid": os.Getpid(), "version": version,
	})
	<-ctx.Done()
	return nil
}

func defaultSocket() string {
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
