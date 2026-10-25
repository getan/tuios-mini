//go:build !darwin && !dragonfly && !freebsd && !linux && !solaris && !aix

package workspace

import "os"

// withoutHardTabs is a no-op where bubbletea does not read TABDLY. See the
// unix version.
func withoutHardTabs(*os.File) func() { return func() {} }
