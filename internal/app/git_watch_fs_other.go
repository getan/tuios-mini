//go:build !linux && !darwin

package app

// gitWatchNetworkFS has no portable filesystem-type check on this platform;
// the existing watcher and the timed reconciliation stay in charge.
func gitWatchNetworkFS(string) bool { return false }
