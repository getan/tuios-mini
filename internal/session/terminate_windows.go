//go:build !unix

package session

import "os"

// Windows has no SIGTERM for an ordinary process; Close's bounded wait still
// applies, and Kill is the only portable request.
func terminateProcess(p *os.Process) error {
	return p.Kill()
}
