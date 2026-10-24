package mini

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectHost(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want Host
	}{
		{"plain", nil, HostPlain},
		{"tuios env", map[string]string{"TUIOS_ENV": "1"}, HostTuios},
		{"tuios pane", map[string]string{"TUIOS_PANE_ID": "w1"}, HostTuios},
		{"golder workspace wins", map[string]string{"TUIOS_ENV": "1", "GOLDER_WORKSPACE": "1"}, HostGolderWorkspace},
		{"tmux", map[string]string{"TMUX": "/tmp/tmux"}, HostTmux},
		{"zellij", map[string]string{"ZELLIJ": "0"}, HostZellij},
		{"screen", map[string]string{"STY": "123.pts"}, HostScreen},
		// The boolean markers are read as booleans. A pane that carries
		// GOLDER_WORKSPACE=0, or a TUIOS_ENV left at "false", is not in a
		// multiplexer, and must not be refused as if it were.
		{"golder workspace off", map[string]string{"GOLDER_WORKSPACE": "0"}, HostPlain},
		{"golder workspace empty", map[string]string{"GOLDER_WORKSPACE": ""}, HostPlain},
		{"tuios env off", map[string]string{"TUIOS_ENV": "0"}, HostPlain},
		{"tuios env false", map[string]string{"TUIOS_ENV": "false"}, HostPlain},
		{"golder workspace spelled true", map[string]string{"GOLDER_WORKSPACE": "true"}, HostGolderWorkspace},
		{"golder workspace beats a tuios pane", map[string]string{"GOLDER_WORKSPACE": "1", "TUIOS_PANE_ID": "w1"}, HostGolderWorkspace},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectHost(func(key string) string { return tc.env[key] })
			if got != tc.want {
				t.Fatalf("DetectHost = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNestedHosts(t *testing.T) {
	for host, want := range map[Host]bool{
		HostPlain: false,
		// A workspace inside a workspace nests two multiplexers, and the
		// outer one is where the person already is.
		HostGolderWorkspace: true,
		HostTuios:           true, HostTmux: true, HostZellij: true, HostScreen: true,
	} {
		if got := host.Nested(); got != want {
			t.Errorf("%s.Nested() = %v, want %v", host, got, want)
		}
	}
}

func TestDefaultNamespaceIsNotTuios(t *testing.T) {
	ns := DefaultNamespace()
	if ns.ConfigDir == "" || ns.StateDir == "" || ns.SocketPath == "" || ns.LogFile == "" {
		t.Fatalf("namespace has an empty path: %+v", ns)
	}
	for _, path := range []string{ns.ConfigDir, ns.StateDir, ns.RuntimeDir, ns.SocketPath, ns.LogFile} {
		if !strings.Contains(path, filepath.Join("golder", "workspace")) {
			t.Errorf("workspace path %q is not under golder/workspace", path)
		}
	}
}

// TestNamespaceForScopesEachProject proves two projects get two workspaces that
// share no path: not the socket, not the state, not the config, not the log.
func TestNamespaceForScopesEachProject(t *testing.T) {
	// A real XDG_RUNTIME_DIR (/run/user/<uid> on Linux, unset on macOS) is
	// short. The isolated test tree is not, and a socket has to fit a
	// sockaddr_un, so this test brings its own.
	t.Setenv("XDG_RUNTIME_DIR", shortRuntimeDir(t))
	a, err := NamespaceFor("golder")
	if err != nil {
		t.Fatalf("NamespaceFor(golder): %v", err)
	}
	b, err := NamespaceFor("harness")
	if err != nil {
		t.Fatalf("NamespaceFor(harness): %v", err)
	}
	for name, pair := range map[string][2]string{
		"config":  {a.ConfigDir, b.ConfigDir},
		"state":   {a.StateDir, b.StateDir},
		"runtime": {a.RuntimeDir, b.RuntimeDir},
		"socket":  {a.SocketPath, b.SocketPath},
		"log":     {a.LogFile, b.LogFile},
	} {
		if pair[0] == "" || pair[1] == "" {
			t.Errorf("%s is empty: %q %q", name, pair[0], pair[1])
		}
		if pair[0] == pair[1] {
			t.Errorf("two projects share a %s: %q", name, pair[0])
		}
		if strings.HasPrefix(pair[0], pair[1]) || strings.HasPrefix(pair[1], pair[0]) {
			t.Errorf("%s of one project is inside the other's: %q %q", name, pair[0], pair[1])
		}
	}
	if err := checkSocketPath(a.SocketPath); err != nil {
		t.Errorf("a scoped socket does not fit: %v", err)
	}
	// The state is persistent and belongs under the state home, not a cache.
	if !strings.Contains(a.StateDir, filepath.Join("golder", "workspace")) {
		t.Errorf("scoped state %q is not under golder/workspace", a.StateDir)
	}
}

// TestScopeSlugFoldsAndStaysDistinct proves a project name that is hostile to
// a path becomes a short folder, and two names that fold alike still differ.
func TestScopeSlugFoldsAndStaysDistinct(t *testing.T) {
	cases := map[string]string{
		"../../etc/passwd":       "",
		"My Project!":            "my-project-",
		"a":                      "a-",
		"":                       "",
		strings.Repeat("x", 400): "xxxxxxxx",
	}
	for scope, prefix := range cases {
		if scope == "" {
			if _, err := scopeSlug(scope); err == nil {
				t.Errorf("scopeSlug(%q) accepted an empty scope", scope)
			}
			continue
		}
		slug, err := scopeSlug(scope)
		if err != nil {
			t.Fatalf("scopeSlug(%q): %v", scope, err)
		}
		if len(slug) > maxScopeSlug {
			t.Errorf("scopeSlug(%q) = %q, %d bytes over %d", scope, slug, len(slug), maxScopeSlug)
		}
		for _, r := range slug {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				t.Errorf("scopeSlug(%q) = %q, which holds %q", scope, slug, r)
			}
		}
		if strings.Contains(slug, "..") || strings.ContainsAny(slug, `/\`) {
			t.Errorf("scopeSlug(%q) = %q, which can leave its folder", scope, slug)
		}
		if prefix != "" && !strings.HasPrefix(slug, prefix) {
			t.Errorf("scopeSlug(%q) = %q, want a prefix of %q", scope, slug, prefix)
		}
	}
	if a, _ := scopeSlug("My Project"); a == mustScopeSlug(t, "my/project") {
		t.Errorf("two projects that fold alike share the folder %q", a)
	}
}

// shortRuntimeDir makes a short private runtime folder, as a desktop login
// would, and removes it when the test ends.
func shortRuntimeDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "gmrt-")
	if err != nil {
		t.Fatalf("make a runtime dir: %v", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func mustScopeSlug(t *testing.T, scope string) string {
	t.Helper()
	slug, err := scopeSlug(scope)
	if err != nil {
		t.Fatalf("scopeSlug(%q): %v", scope, err)
	}
	return slug
}

// TestCheckSocketPathIsActionable proves a socket path over the kernel's limit
// is refused with a message that says the length, the limit and what to change.
func TestCheckSocketPathIsActionable(t *testing.T) {
	if err := checkSocketPath("/tmp/d.sock"); err != nil {
		t.Fatalf("a short socket path was refused: %v", err)
	}
	long := "/tmp/" + strings.Repeat("n", maxSocketPath) + "/d.sock"
	err := checkSocketPath(long)
	if err == nil {
		t.Fatal("a socket path over the limit was accepted")
	}
	for _, want := range []string{long, "limit", "shorter", "XDG_RUNTIME_DIR"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
}

// TestRuntimeBaseHonoursXDGOnlyWhenSafe proves the runtime folder is used when
// it is this user's and private, and that a world-writable one is not.
func TestRuntimeBaseHonoursXDGOnlyWhenSafe(t *testing.T) {
	private := t.TempDir()
	if err := os.Chmod(private, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", private)
	if got := RuntimeBase(); got != private {
		t.Errorf("RuntimeBase with a private XDG_RUNTIME_DIR = %q, want %q", got, private)
	}

	open := t.TempDir()
	if err := os.Chmod(open, 0o777); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", open)
	got := RuntimeBase()
	if got == open {
		t.Errorf("RuntimeBase used a world-writable runtime dir %q", open)
	}
	if strings.Contains(got, filepath.Base(open)) && got == open {
		t.Errorf("RuntimeBase = %q, want a private fallback", got)
	}
}
