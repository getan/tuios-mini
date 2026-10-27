//go:build !darwin

package app

import (
	"fmt"
	"runtime"
)

func nativeGitWatch(_ string, _ func()) (func(), bool, error) {
	switch runtime.GOOS {
	case "freebsd", "openbsd", "netbsd", "dragonfly":
		// fsnotify's kqueue backend opens every child file of watched
		// directories. Do not consume the client's entire descriptor limit.
		return nil, true, fmt.Errorf("recursive Git watching unavailable on %s; using timed reconciliation", runtime.GOOS)
	}
	return nil, false, nil
}
