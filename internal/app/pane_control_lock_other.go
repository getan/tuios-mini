//go:build !darwin && !linux && !freebsd && !openbsd && !netbsd

package app

import "os"

func lockPaneControl(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	return func() { _ = f.Close() }, nil
}
