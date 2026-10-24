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
