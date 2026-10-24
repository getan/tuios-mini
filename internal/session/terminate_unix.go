//go:build unix

package session

import (
	"os"
	"syscall"
)

// terminateProcess asks a pane's process to leave. Close follows it with a
// bounded wait and SIGKILL, so a program that handles SIGTERM can run its
// cleanup without holding the pane open forever.
func terminateProcess(p *os.Process) error {
	return p.Signal(syscall.SIGTERM)
}
