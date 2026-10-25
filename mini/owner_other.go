//go:build !unix

package mini

// currentUID is unknown on a platform with no unix user ids.
func currentUID() int { return -1 }

// fileOwner reports no owner, so the mode check alone decides whether a folder
// is private.
func fileOwner(string) (int, bool) { return 0, false }
