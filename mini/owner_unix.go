//go:build unix

package mini

import "syscall"

// currentUID is this process's user id.
func currentUID() int { return syscall.Getuid() }

// fileOwner is the user id that owns path, and whether the platform reports
// one. It reads the stat the caller just took: the folder is private only if it
// is this user's, and a mode alone does not say who set it.
func fileOwner(path string) (int, bool) {
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		return 0, false
	}
	return int(st.Uid), true
}
