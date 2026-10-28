//go:build unix

package app

import (
	"os"

	"golang.org/x/sys/unix"
)

// Open nonblocking before inspecting the descriptor: even if a regular-file
// precheck races replacement by a FIFO, opening it must not park a worker.
func openPresetConfig(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|unix.O_NONBLOCK, 0)
}
