package app

import (
	"strings"
	"testing"

	"github.com/getan/tuios-mini/internal/config"
)

func TestGolderPaneDefaultTitleDoesNotOverrideCustomName(t *testing.T) {
	w := newTestWindow(t, "12345678-1234", 80, 20)
	w.AgentHarness, w.Cwd = "golder", "/work/project"
	s := config.DefaultSettings()
	if got := windowTitleText(w, "", 1, 80, &s); !strings.Contains(got, "project") {
		t.Fatalf("default title = %q", got)
	}
	w.CustomName = "my name"
	if got := windowTitleText(w, "", 1, 80, &s); !strings.Contains(got, "my name") || strings.Contains(got, "project") {
		t.Fatalf("explicit title = %q", got)
	}
}
