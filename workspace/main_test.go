package workspace

import (
	"os"
	"testing"

	"github.com/Gaurav-Gosain/tuios/internal/testutil"
)

// TestMain isolates the whole test binary from the developer's own XDG
// directories. That matters more here than anywhere: the point of this package
// is that a client can be told which config tree is its own, and a test that
// got that wrong would otherwise edit the real ~/.config/tuios/config.toml.
// See testutil.RunIsolated.
func TestMain(m *testing.M) { os.Exit(testutil.RunIsolated(m)) }
