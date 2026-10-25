package wire

import (
	"os"
	"testing"

	"github.com/getan/tuios-mini/internal/testutil"
)

func TestMain(m *testing.M) {
	os.Exit(testutil.RunIsolated(m))
}
