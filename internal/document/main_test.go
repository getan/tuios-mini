package document

import (
	"os"
	"testing"

	"github.com/getan/tuios-mini/internal/testutil"
)

// TestMain keeps every test away from the developer's XDG config and state.
func TestMain(m *testing.M) { os.Exit(testutil.RunIsolated(m)) }
