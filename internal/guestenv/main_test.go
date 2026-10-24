package guestenv

import (
	"os"
	"testing"

	"github.com/getan/tuios-mini/internal/testutil"
)

// TestMain isolates the whole test binary from the developer's own XDG
// directories. See testutil.RunIsolated.
func TestMain(m *testing.M) {
	os.Exit(testutil.RunIsolated(m))
}
