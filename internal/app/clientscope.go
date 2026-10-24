package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/adrg/xdg"

	"github.com/getan/tuios-mini/pkg/applist"
)

// A client keeps more than its config file. The sidebar's order, the rail's
// width and its per-window accents, the launcher's history, a crash report, a
// tape, a saved layout, a render trace: each of them is a small file this
// package derives on its own from an XDG base directory.
//
// That is right for a machine where tuios is the only client. It is wrong for a
// host that embeds the client inside a tree of its own, and it is wrong twice
// over when one host runs two projects: a workspace would read the person's
// sidebar, write the person's launcher history, and leave a crash report in the
// person's state directory — and two projects would share one sidebar file,
// keyed as it is by nothing but the state directory.
//
// So the directory those files come from is a scope, not a constant.
// ScopeClientState points every one of them at a directory the host owns and
// returns the function that puts the user's back. Nothing else is redirected:
// the desktop entries the launcher lists and the icons it draws them with are
// still the machine's, because a workspace runs the same programs the person's
// desktop does, and the files it deletes still go to the real trash.
//
// The scope is process-wide because its callers are: the sidebar saves through
// a package function, not through a handle it was given.

// errNoStateDir is returned when a scope was asked for without a directory. It
// is a mistake in the caller, so it is refused here rather than at the file
// system.
var errNoStateDir = errors.New("app: no state directory; a client scope needs the directory the host owns")

// clientState is the directory a host scoped this process to, when there is
// one.
var clientState struct {
	mu  sync.RWMutex
	dir string
}

// ScopeClientState points every per-client file this package derives at dir,
// and returns the function that puts the user's directories back.
//
// dir is made here, because every one of these files is written at some point
// in a client's life and a save that cannot create its directory is a save that
// silently does nothing. It is made absolute so a caller that changes directory
// cannot move the client's state out from under it.
//
// Restoring is safe to call more than once and from a defer. A second scope
// while one is open is an error rather than a nesting: this is process-wide
// state, and two clients would each read the other's sidebar.
func ScopeClientState(dir string) (restore func(), err error) {
	if dir == "" {
		return nil, errNoStateDir
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("app: resolve %s: %w", dir, err)
	}

	clientState.mu.Lock()
	if clientState.dir != "" {
		open := clientState.dir
		clientState.mu.Unlock()
		return nil, fmt.Errorf("app: %s is already in scope; end that scope before starting another", open)
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		clientState.mu.Unlock()
		return nil, fmt.Errorf("app: create %s: %w", abs, err)
	}
	clientState.dir = abs
	clientState.mu.Unlock()

	// The launcher's history is the one file of these that lives in another
	// package, so it is scoped there. Its own path is the same shape as the
	// rest: one small JSON file under the directory.
	restoreLauncher := applist.ScopeDefaultPath(filepath.Join(abs, "launcher.json"))

	var once sync.Once
	return func() {
		once.Do(func() {
			restoreLauncher()
			clientState.mu.Lock()
			clientState.dir = ""
			clientState.mu.Unlock()
		})
	}, nil
}

// ScopedClientState reports the directory a host scoped this package's
// per-client files to, and whether there is one.
func ScopedClientState() (string, bool) {
	clientState.mu.RLock()
	defer clientState.mu.RUnlock()
	return clientState.dir, clientState.dir != ""
}

// uiStateDir is the directory per-client UI state files live in: the host's
// when this process is scoped, the user's XDG state directory otherwise.
//
// It is the one place the fallback is written down. Every file below goes
// through it or through uiStatePath, so a host that scoped the process cannot
// reach the user's tree through a path this package forgot to route.
func uiStateDir() string {
	if dir, ok := ScopedClientState(); ok {
		return dir
	}
	return filepath.Join(xdg.StateHome, "tuios")
}

// uiStatePath is one file inside uiStateDir.
func uiStatePath(name string) string {
	return filepath.Join(uiStateDir(), name)
}

// uiStateSubdir is a directory inside the scoped tree, made ready, for the
// files a client groups in one (its tapes, its saved layouts). ok is false when
// the process is not scoped, where each of those has an XDG base of its own and
// the caller keeps using it.
func uiStateSubdir(name string) (string, bool, error) {
	dir, ok := ScopedClientState()
	if !ok {
		return "", false, nil
	}
	full := filepath.Join(dir, name)
	if err := os.MkdirAll(full, 0o700); err != nil {
		return "", true, fmt.Errorf("app: create %s: %w", full, err)
	}
	return full, true, nil
}
