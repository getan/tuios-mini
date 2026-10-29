package vtcontract_test

import (
	"os"
	"testing"

	"github.com/getan/tuios-mini/internal/testutil"
)

// Even a pure protocol target follows the root module's isolation contract;
// future parser changes must never read or write the developer's XDG tree.
func TestMain(m *testing.M) { os.Exit(testutil.RunIsolated(m)) }
