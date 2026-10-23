//go:build unix

package main

import (
	"os"
	"syscall"
)

// shutdownSignals includes SIGHUP: a pane or terminal closing must still let
// the daemon remove its socket.
func shutdownSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}
}
