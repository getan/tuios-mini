package control

import (
	"os"
	"testing"

	"github.com/getan/tuios-mini/internal/testutil"
)

// TestMain isolates the whole test binary from the developer's own XDG
// directories, as every package with tests in this tree does. The client itself
// reads none of them, but a test here starts a socket, and a run that reached a
// person's real state tree would be wrong even if it passed.
func TestMain(m *testing.M) {
	os.Exit(testutil.RunIsolated(m))
}
