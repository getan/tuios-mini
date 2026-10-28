//go:build !unix

package app

import (
	"fmt"
	"os"
)

func openPresetConfig(path string) (*os.File, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("preset config is not a regular file")
	}
	return os.Open(path)
}
