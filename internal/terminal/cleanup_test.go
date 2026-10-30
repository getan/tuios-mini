package terminal

import (
	"strings"
	"testing"
)

func TestResetSequenceClearsKeyboardProtocols(t *testing.T) {
	for _, want := range []string{"\x1b[=0;1u", "\x1b[>4m", "\x1b[?47l"} {
		if !strings.Contains(ResetSequence, want) {
			t.Errorf("ResetSequence does not contain %q", want)
		}
	}
}
