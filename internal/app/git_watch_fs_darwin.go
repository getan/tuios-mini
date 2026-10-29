//go:build darwin

package app

import "golang.org/x/sys/unix"

// gitWatchNetworkFS reports whether root is on a non-local mount. FSEvents on
// a network mount is as unreliable as inotify there; the timed reconciliation
// is the better fallback.
func gitWatchNetworkFS(root string) bool {
	var st unix.Statfs_t
	if err := unix.Statfs(root, &st); err != nil {
		return false
	}
	return st.Flags&unix.MNT_LOCAL == 0
}
