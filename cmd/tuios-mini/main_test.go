package main

import (
	"strings"
	"testing"
)

func TestWorkspaceRefusesNestingByDefault(t *testing.T) {
	t.Setenv("TUIOS_ENV", "1")
	err := run([]string{"workspace"})
	if err == nil || !strings.Contains(err.Error(), "nest") {
		t.Fatalf("run(workspace inside tuios) = %v, want a nesting refusal", err)
	}
}
