//go:build linux

package app

import "golang.org/x/sys/unix"

// gitWatchNetworkFS reports whether root is on a filesystem whose events are
// expensive or unreliable. fsnotify on NFS, CIFS/SMB and FUSE often misses
// remote changes, and every inotify add is a round trip; the timed
// reconciliation is the better fallback.
func gitWatchNetworkFS(root string) bool {
	var st unix.Statfs_t
	if err := unix.Statfs(root, &st); err != nil {
		return false
	}
	switch st.Type {
	case unix.NFS_SUPER_MAGIC, unix.CIFS_SUPER_MAGIC, unix.SMB2_SUPER_MAGIC, unix.FUSE_SUPER_MAGIC:
		return true
	}
	return false
}
