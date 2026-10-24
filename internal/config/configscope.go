package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/adrg/xdg"
)

// The config file a process reads and writes is the user's, at
// ~/.config/tuios/config.toml (XDG's, on every platform). A host that embeds
// the client and owns its own tree is not that user: golder's workspace keeps
// its config under its own namespace, and a client that read the user's file
// while writing the settings page's saves back into it would edit a file the
// host was told to leave alone.
//
// So the path is a scope, not a constant. ScopeConfigPath points every default
// lookup in this package at a path the host owns:
//
//   - GetConfigPath, which the config watcher and the settings page's links
//     resolve;
//   - LoadUserConfig, including the first-run file it writes when there is
//     none;
//   - RenderUserConfig and SaveUserConfig, which every settings row saves
//     through;
//   - EffectiveDefault, which reads the file to answer what an option defaults
//     to.
//
// Nothing else is redirected: the state, data and runtime directories, the
// themes and glyphs asset directories and the socket are the process's, and a
// host that needs them elsewhere arranges that itself. A scope covers the
// config file and the config.d drop-ins and includes beside it, because those
// are resolved relative to it.
//
// The scope is process-wide because its callers are: the settings page writes
// through a package function, not through a handle it was given. That is why
// ScopeConfigPath refuses a second scope while one is open, and why it returns
// the function that ends it: a host must put the globals back before it hands
// the process to somebody else, or a later standalone client would find its
// lookups pointed at the embedded tree.
//
// The config file is the first of several such scopes and not the only one. A
// client also reads and writes a state directory (the sidebar, the launcher's
// history, its tapes and saved layouts — app.ScopeClientState), the custom
// theme and glyph set directories with the registry and selections that go
// with them (theme.ScopeAssets), and the process's log stream
// (debuglog.Scope). A host that scopes one and not the others gets a client
// that reads the person's config and writes the person's sidebar, which is
// worse than one that reads both. workspace.Options.open opens them together
// and is what an embedding host should copy.

// errNoScopedPath is returned when a scope was asked for without a path. It is
// a programming mistake, not a user-visible failure, so it is refused here
// rather than at the file system.
var errNoScopedPath = errors.New("config: no config path; a scope needs the file the host owns")

// errScopeOpen is returned when a scope is asked for while another is open.
type errScopeOpen struct{ path string }

func (e *errScopeOpen) Error() string {
	return fmt.Sprintf("config: %s is already in scope; end that scope before starting another", e.path)
}

// configScope is the path a host scoped this process to, when there is one.
var configScope struct {
	mu   sync.Mutex
	path string
	open bool
}

// ScopeConfigPath makes this package read and write the config file at path
// instead of the user's, and returns the function that puts the user's back.
//
// path is the file, not the directory, and it is made absolute here so a
// caller that changes directory cannot move the config out from under the
// client. The directory is not created until something writes there. Restoring
// is safe to call more than once and from a defer.
//
// A scope that is already open is an error rather than a nesting: this is
// process-wide state, and two visible clients in one process would otherwise
// each read the other's file.
func ScopeConfigPath(path string) (restore func(), err error) {
	if path == "" {
		return nil, errNoScopedPath
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("config: resolve %s: %w", path, err)
	}

	configScope.mu.Lock()
	defer configScope.mu.Unlock()
	if configScope.open {
		return nil, &errScopeOpen{path: configScope.path}
	}
	configScope.path, configScope.open = abs, true

	var once sync.Once
	return func() {
		once.Do(func() {
			configScope.mu.Lock()
			defer configScope.mu.Unlock()
			configScope.path, configScope.open = "", false
		})
	}, nil
}

// ScopedConfigPath reports the config file a host scoped this process to, and
// whether there is one. An embedding host uses it to answer "which file is
// this client actually reading" without loading the config to find out.
func ScopedConfigPath() (string, bool) {
	configScope.mu.Lock()
	defer configScope.mu.Unlock()
	return configScope.path, configScope.open
}

// configFileToRead is the config file this process reads, and whether it is
// there. A caller that needs somewhere to write when it is not there asks
// configFileToWrite.
//
// A scoped path is read as it stands and nothing else is looked at: the user's
// file is not a fallback for a host that was given its own. An unscoped
// process gets the file XDG finds, which is what every standalone client does.
//
// Nothing is created here. This is also the path a plain "what does this
// option default to" question takes, and that must not make a directory the
// person never asked for.
func configFileToRead() (path string, found bool, err error) {
	if scoped, ok := ScopedConfigPath(); ok {
		switch _, statErr := os.Stat(scoped); {
		case statErr == nil:
			return scoped, true, nil
		case errors.Is(statErr, fs.ErrNotExist):
			return scoped, false, nil
		default:
			// There is a file and it cannot be looked at. Reporting it missing
			// would write a first-run config over it, so the error stands.
			return scoped, false, fmt.Errorf("config: %w", statErr)
		}
	}
	path, err = xdg.SearchConfigFile("tuios/config.toml")
	if err != nil {
		return "", false, nil
	}
	return path, true, nil
}

// configFileToWrite is the config file this process saves to, with the
// directory it lives in made ready. A save, and the first-run file, go here.
func configFileToWrite() (string, error) {
	if scoped, ok := ScopedConfigPath(); ok {
		// The host named the file and may not have made the folder yet.
		// xdg.ConfigFile does this for the user's path, so a scoped path does
		// it here and a first save on a fresh machine works the same way.
		if dir := filepath.Dir(scoped); dir != "" {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return "", fmt.Errorf("config: create %s: %w", dir, err)
			}
		}
		return scoped, nil
	}
	return xdg.ConfigFile("tuios/config.toml")
}
