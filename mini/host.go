package mini

import (
	"os"
	"path/filepath"
)

// Host is the terminal environment a golder process is running in.
type Host string

const (
	HostPlain           Host = "plain"
	HostTuios           Host = "tuios"
	HostGolderWorkspace Host = "golder-workspace"
	HostTmux            Host = "tmux"
	HostZellij          Host = "zellij"
	HostScreen          Host = "screen"
)

// DetectHost reports the environment from the variables a pane receives.
// GOLDER_WORKSPACE wins over TUIOS_ENV because a pane inside a golder
// workspace also carries the tuios-style host contract.
func DetectHost(getenv func(string) string) Host {
	if getenv == nil {
		getenv = os.Getenv
	}
	switch {
	case getenv("GOLDER_WORKSPACE") != "":
		return HostGolderWorkspace
	case getenv("TUIOS_ENV") != "" || getenv("TUIOS_PANE_ID") != "":
		return HostTuios
	case getenv("TMUX") != "":
		return HostTmux
	case getenv("ZELLIJ") != "":
		return HostZellij
	case getenv("STY") != "":
		return HostScreen
	default:
		return HostPlain
	}
}

// Nested reports whether a workspace started here would be a pane inside
// another terminal multiplexer. A golder workspace is not nested in itself.
func (h Host) Nested() bool {
	switch h {
	case HostTuios, HostTmux, HostZellij, HostScreen:
		return true
	}
	return false
}

// Namespace is the set of paths a golder workspace owns. None of them is
// tuios's: both can run on one machine without sharing a socket, a pid file,
// a config tree or a log.
type Namespace struct {
	ConfigDir  string
	StateDir   string
	RuntimeDir string
	SocketPath string
	LogFile    string
}

// DefaultNamespace derives the workspace paths from the user's XDG roots.
func DefaultNamespace() Namespace {
	cache, _ := os.UserCacheDir()
	config, _ := os.UserConfigDir()
	if cache == "" {
		cache = os.TempDir()
	}
	if config == "" {
		config = cache
	}
	ns := Namespace{
		ConfigDir:  filepath.Join(config, "golder", "workspace"),
		StateDir:   filepath.Join(cache, "golder", "workspace", "state"),
		RuntimeDir: filepath.Join(cache, "golder", "workspace", "run"),
		LogFile:    filepath.Join(cache, "golder", "workspace", "workspace.log"),
	}
	ns.SocketPath = filepath.Join(ns.RuntimeDir, "workspace.sock")
	return ns
}
