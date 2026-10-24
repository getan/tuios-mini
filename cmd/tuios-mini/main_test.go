package main

import (
	"os"
	"strings"
	"testing"

	"github.com/Gaurav-Gosain/tuios/internal/testutil"
)

func TestMain(m *testing.M) {
	os.Exit(testutil.RunIsolated(m))
}

func TestWorkspaceRefusesNestingByDefault(t *testing.T) {
	t.Setenv("TUIOS_ENV", "1")
	err := run([]string{"workspace"})
	if err == nil || !strings.Contains(err.Error(), "nest") {
		t.Fatalf("run(workspace inside tuios) = %v, want a nesting refusal", err)
	}
}

// TestHelpIsNotAFailure proves `tuios-mini --help` prints the usage and exits
// zero, which every script that probes a binary for its flags depends on.
func TestHelpIsNotAFailure(t *testing.T) {
	if err := run([]string{"--help"}); err != nil {
		t.Fatalf("run(--help) = %v, want nil", err)
	}
	t.Setenv("TUIOS_ENV", "")
	if err := run([]string{"workspace", "--help"}); err != nil {
		t.Fatalf("run(workspace --help) = %v, want nil", err)
	}
}

// TestWorkspaceRefusesGolderNesting proves a workspace started inside a golder
// workspace is refused too: the outer workspace is the multiplexer the person
// is already in.
func TestWorkspaceRefusesGolderNesting(t *testing.T) {
	clearHostMarkers(t)
	t.Setenv("GOLDER_WORKSPACE", "1")
	err := run([]string{"workspace"})
	if err == nil || !strings.Contains(err.Error(), "nest") {
		t.Fatalf("run(workspace inside a workspace) = %v, want a nesting refusal", err)
	}
	// The marker is a boolean: a leftover zero does not nest, so the workspace
	// gets as far as trying to start. The socket here is deliberately
	// impossible, which stops it before a daemon exists and proves the nesting
	// check was not what refused it.
	t.Setenv("GOLDER_WORKSPACE", "0")
	err = run([]string{"workspace", "--socket", "/tmp/" + strings.Repeat("n", 120) + "/d.sock"})
	if err == nil || strings.Contains(err.Error(), "nest") {
		t.Fatalf("run(workspace with GOLDER_WORKSPACE=0) = %v, want a start attempt that is not a nesting refusal", err)
	}
	if !strings.Contains(err.Error(), "limit") {
		t.Errorf("the refusal does not explain the socket path: %v", err)
	}
}

// clearHostMarkers removes the variables that say this test is running inside a
// pane of another multiplexer, which is exactly where a developer runs the
// suite.
func clearHostMarkers(t *testing.T) {
	t.Helper()
	for _, name := range []string{"GOLDER_WORKSPACE", "TUIOS_ENV", "TUIOS_PANE_ID", "TMUX", "ZELLIJ", "STY"} {
		t.Setenv(name, "")
	}
}

// TestUnknownFlagIsAnError proves a mistyped flag is reported rather than read
// as the first pane's argv.
func TestUnknownFlagIsAnError(t *testing.T) {
	if err := run([]string{"--no-such-flag"}); err == nil {
		t.Fatal("run(--no-such-flag) accepted an unknown flag")
	}
}
