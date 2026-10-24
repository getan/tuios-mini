package mini

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
//
// The two boolean markers, GOLDER_WORKSPACE and TUIOS_ENV, are read as
// booleans, not as "set at all": golder and tuios set them to 1, so anything
// else (a 0, a leftover "false", an empty export) is not a marker. A pane that
// carries GOLDER_WORKSPACE=0 is not in a workspace, which matters because the
// nesting check turns this answer into a refusal. TMUX, ZELLIJ and STY carry
// values of their own (a socket path, a session name), so they are markers by
// presence.
func DetectHost(getenv func(string) string) Host {
	if getenv == nil {
		getenv = os.Getenv
	}
	switch {
	case isMarked(getenv("GOLDER_WORKSPACE")):
		return HostGolderWorkspace
	case isMarked(getenv("TUIOS_ENV")) || getenv("TUIOS_PANE_ID") != "":
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

// isMarked reads a boolean marker variable. golder and tuios export 1; "true"
// is accepted as the same thing by hand. Everything else, including 0, is not a
// marker.
func isMarked(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true":
		return true
	}
	return false
}

// Nested reports whether a workspace started here would be a pane inside
// another multiplexer, and so whether starting one without asking would nest
// two of them.
//
// A golder workspace counts: a workspace started inside a golder workspace is
// one multiplexer inside another, and the outer one is what the person is
// already in. It used to be exempt as "not nested in itself", which was true
// only while there was one workspace per machine. With per-project workspaces,
// running a project's workspace from a pane of the machine's workspace is the
// normal case, and the inner one must say so rather than quietly building a
// second layer.
func (h Host) Nested() bool {
	switch h {
	case HostGolderWorkspace, HostTuios, HostTmux, HostZellij, HostScreen:
		return true
	}
	return false
}

// Namespace is the set of paths a golder workspace owns. None of them is
// tuios's: both can run on one machine without sharing a socket, a pid file,
// a config tree or a log. Two namespaces of two projects share nothing either.
//
// The paths fall into two kinds, and they are kept apart on purpose. The state
// and config trees live under the user's XDG state and config homes, because a
// workspace's sessions, history and settings must survive a reboot. The socket
// and its log live under a runtime folder, because a socket is bound once and
// its path has to fit a sockaddr_un (104 bytes on macOS, 108 on Linux,
// terminator included) including the names the daemon puts beside it.
type Namespace struct {
	ConfigDir  string
	StateDir   string
	RuntimeDir string
	SocketPath string
	LogFile    string
}

// maxSocketPath is the most a unix socket path may hold, terminator included.
// Darwin's sockaddr_un.sun_path is 104 bytes and Linux's is 108; the smaller
// of the two is the one to fit, and bind answers a longer one with EINVAL,
// which names neither the path nor its length.
const maxSocketPath = 104

// socketNameSlack is the longest name the daemon puts beside its own socket:
// ".link-human". A socket path plus this has to fit too, since the daemon binds
// all of them.
const socketNameSlack = len(".link-human")

// maxScopeSlug bounds the folder name one project gets, so the socket inside
// it still fits. See scopeSlug.
const maxScopeSlug = 23

// DefaultNamespace is the namespace of the machine's own workspace: the one a
// person starts in, with no project scope. It is NamespaceFor("").
func DefaultNamespace() Namespace {
	ns, err := NamespaceFor("")
	if err != nil {
		// NamespaceFor only fails on a scope it cannot spell, and "" always
		// spells. Rather than panic in a helper, fall back to the scoped name
		// of a scope that is legal by construction.
		ns, _ = NamespaceFor("workspace")
	}
	return ns
}

// NamespaceFor derives the paths of one workspace. scope names the project the
// workspace belongs to, so several projects of one user get several workspaces
// that cannot see each other's daemon or files. An empty scope is the machine's
// own workspace, which is what a person starts in and what M3 shipped.
//
// The scope becomes a folder name, so it is folded to letters, digits and '-'
// and shortened, and a short digest of the original is appended: two projects
// whose names fold to the same folder still get different folders, and the
// folder stays inside the socket path budget. The empty scope does not fold.
func NamespaceFor(scope string) (Namespace, error) {
	configHome := configBaseDir()
	stateHome := stateBaseDir()

	ns := Namespace{
		ConfigDir: filepath.Join(configHome, "golder", "workspace"),
		StateDir:  filepath.Join(stateHome, "golder", "workspace"),
	}
	runtimeRoot := RuntimeBase()
	if scope == "" {
		ns.RuntimeDir = filepath.Join(runtimeRoot, "golder", "workspace")
		ns.SocketPath = filepath.Join(ns.RuntimeDir, "workspace.sock")
		ns.LogFile = filepath.Join(ns.RuntimeDir, "workspace.log")
		return ns, nil
	}

	slug, err := scopeSlug(scope)
	if err != nil {
		return Namespace{}, err
	}
	ns.ConfigDir = filepath.Join(ns.ConfigDir, slug)
	ns.StateDir = filepath.Join(ns.StateDir, slug, "state")
	// The runtime folder is chosen by length, shortest layout only when the
	// readable one does not fit: a runtime root that is itself long (a macOS
	// per-user temporary folder, a test's own tree) leaves little of the socket
	// budget for the folders below it. "ws" rather than "workspace" for the
	// same reason.
	var lastErr error
	for _, dir := range []string{
		filepath.Join(runtimeRoot, "golder", "ws", slug),
		filepath.Join(runtimeRoot, "g-"+slug),
	} {
		socket := filepath.Join(dir, "d.sock")
		if lastErr = checkSocketPath(socket); lastErr != nil {
			continue
		}
		ns.RuntimeDir = dir
		ns.SocketPath = socket
		ns.LogFile = filepath.Join(dir, "workspace.log")
		return ns, nil
	}
	return Namespace{}, lastErr
}

// RuntimeBase is the folder below which runtime paths are kept: short, private
// to this user, and gone at logout where the system offers such a folder.
//
// $XDG_RUNTIME_DIR is used when it names a folder that is really this user's
// and closed to everyone else, which is what a desktop login and a systemd
// session provide. Anything else (unset, a folder someone else could write to,
// a symbolic link) is not safe enough for a socket whose connections are
// trusted by the daemon, so the fallback is <tmp>/golder-<uid>: private to the
// user, and short on both platforms that have unix sockets.
func RuntimeBase() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" && privateDir(dir) {
		return dir
	}
	if runtime.GOOS == "windows" {
		return os.TempDir()
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("golder-%d", os.Getuid()))
}

// privateDir reports whether dir is a real folder, not a link, that this user
// owns and nobody else can write to. Whoever controls the folder of a socket can
// put their own socket there, so a folder this program did not make is used only
// when this holds.
func privateDir(dir string) bool {
	st, err := os.Lstat(dir)
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return false
	}
	if st.Mode().Perm()&0o077 != 0 {
		return false
	}
	// The ownership check needs a stat with an owner; on the platforms whose
	// os.Stat has none, the mode check above is what there is.
	if owner, ok := fileOwner(dir); ok && owner != currentUID() {
		return false
	}
	return true
}

// checkSocketPath refuses a socket path the kernel cannot bind, with a message
// that says what to do about it. bind answers this case with EINVAL and names
// neither the path nor the length, so the daemon has to say it first.
func checkSocketPath(path string) error {
	// The daemon binds its own socket and up to three beside it, the longest
	// ".link-human" longer, and keeps a pid file. Every one of them has to fit.
	if len(path)+socketNameSlack < maxSocketPath {
		return nil
	}
	return fmt.Errorf("mini: the control socket path is %d bytes (%s), and with the sockets and pid file the daemon opens beside it (%d bytes more) that does not fit the %d byte limit a unix socket path has. "+
		"Use a shorter --socket or a shorter StateDir, or set XDG_RUNTIME_DIR to a short private directory such as a fresh directory under /tmp",
		len(path), path, socketNameSlack, maxSocketPath)
}

// scopeSlug is the folder name one project's workspace gets: its name folded to
// lower-case letters, digits and '-', shortened, and a six-character digest of
// the original appended so two folding names never collide.
func scopeSlug(scope string) (string, error) {
	if scope == "" {
		return "", fmt.Errorf("mini: a workspace scope cannot be empty; use the machine's own namespace, which is NamespaceFor(\"\")")
	}
	var b strings.Builder
	lastDash := true
	for _, r := range strings.ToLower(scope) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteByte('-')
			lastDash = true
		}
		if b.Len() >= maxScopeSlug-7 {
			break
		}
	}
	name := strings.Trim(b.String(), "-")
	sum := sha256.Sum256([]byte(scope))
	slug := name + "-" + hex.EncodeToString(sum[:3])
	if name == "" {
		slug = hex.EncodeToString(sum[:8])
	}
	if len(slug) > maxScopeSlug {
		slug = slug[:maxScopeSlug]
	}
	return slug, nil
}

// configBaseDir is the XDG config home, spelled out rather than read from a
// package that resolves it once at startup: a test that moves XDG_CONFIG_HOME
// for itself has to be obeyed, and the standalone daemon's config tree is not
// this workspace's to change.
func configBaseDir() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return dir
	}
	if dir, err := os.UserConfigDir(); err == nil {
		return dir
	}
	return os.TempDir()
}

// stateBaseDir is the XDG state home, with the same reasoning as configBaseDir:
// a workspace's saved sessions belong under the state home, not the cache
// directory, because a cache may be swept at any time and the state is what the
// next daemon reads.
func stateBaseDir() string {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return dir
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if runtime.GOOS == "darwin" {
			return filepath.Join(home, "Library", "Application Support")
		}
		return filepath.Join(home, ".local", "state")
	}
	return os.TempDir()
}
