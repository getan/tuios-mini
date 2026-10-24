package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeProbeManifest puts a manifest with a recognisable id in dir. The detect
// rule matches a process name nothing runs, so the manifest can only be seen
// through the registry: a test can ask whether it was loaded without pretending
// to be the agent it names.
func writeProbeManifest(t *testing.T, dir, id string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("make manifest dir: %v", err)
	}
	path := filepath.Join(dir, id+".toml")
	body := strings.Join([]string{
		"schema_version = 1",
		`id             = "` + id + `"`,
		`display_name   = "` + id + `"`,
		"priority       = 90",
		"",
		"[detect]",
		`comm = ["` + id + `-process"]`,
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return path
}

// TestAgentMatcherReadsOnlyTheConfiguredHarnessSource is the harness-dir half of
// the embedding boundary. A daemon that embeds another program must not read the
// harness rules a standalone tuios on the same machine was configured with:
// TUIOS_HARNESS_DIR and XDG_CONFIG_HOME/tuios/harnesses describe another
// program's panes. The standalone daemon keeps reading them.
func TestAgentMatcherReadsOnlyTheConfiguredHarnessSource(t *testing.T) {
	userDir := t.TempDir()
	ownDir := t.TempDir()
	writeProbeManifest(t, userDir, "planted-user")
	writeProbeManifest(t, ownDir, "planted-own")

	// Both ways a user names the directory, so a matcher that read the user
	// directory by any route is caught, not only by TUIOS_HARNESS_DIR.
	t.Setenv("TUIOS_HARNESS_DIR", userDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(userDir, "xdg"))

	cases := []struct {
		name    string
		source  harnessSource
		dir     string
		want    []string
		notWant []string
	}{
		{
			name:   "standalone reads the user directory",
			source: harnessSourceUser,
			want:   []string{"planted-user", "claude-code"},
		},
		{
			name:    "bundled only ignores every user path",
			source:  harnessSourceBundled,
			want:    []string{"claude-code", "codex"},
			notWant: []string{"planted-user", "planted-own"},
		},
		{
			name:    "one named directory replaces the user directory",
			source:  harnessSourceDir,
			dir:     ownDir,
			want:    []string{"planted-own", "claude-code"},
			notWant: []string{"planted-user"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newAgentMatcherFrom(tc.source, tc.dir, nil, func(string, ...any) {})
			if m.registry == nil {
				t.Fatal("the matcher has no registry")
			}
			for _, id := range tc.want {
				if m.registry.Lookup(id) == nil {
					t.Errorf("manifest %q was not loaded; loaded: %v", id, m.registry.IDs())
				}
			}
			for _, id := range tc.notWant {
				if m.registry.Lookup(id) != nil {
					t.Errorf("manifest %q was loaded from a directory this daemon must not read; loaded: %v", id, m.registry.IDs())
				}
			}
		})
	}

	// The standalone shim the rest of this package calls is still the user
	// directory, so the compatibility the flag is meant to preserve is real.
	if newAgentMatcher(nil).registry.Lookup("planted-user") == nil {
		t.Error("newAgentMatcher no longer reads the user harness directory, so standalone harnesses stopped loading")
	}
}

// TestDaemonHarnessConfigPicksTheSource is the same rule one level up, on the
// daemon configuration an embedding caller fills in.
func TestDaemonHarnessConfigPicksTheSource(t *testing.T) {
	userDir := t.TempDir()
	ownDir := t.TempDir()
	writeProbeManifest(t, userDir, "planted-user")
	writeProbeManifest(t, ownDir, "planted-own")
	t.Setenv("TUIOS_HARNESS_DIR", userDir)

	cases := []struct {
		name string
		cfg  DaemonConfig
		want string
		bad  string
	}{
		{
			name: "the zero value is still the standalone daemon",
			cfg:  DaemonConfig{},
			want: "planted-user",
			bad:  "planted-own",
		},
		{
			name: "bundled only",
			cfg:  DaemonConfig{BundledHarnessesOnly: true},
			want: "claude-code",
			bad:  "planted-user",
		},
		{
			name: "a directory of its own",
			cfg:  DaemonConfig{HarnessDir: ownDir},
			want: "planted-own",
			bad:  "planted-user",
		},
		{
			name: "a named directory wins over bundled only",
			cfg:  DaemonConfig{HarnessDir: ownDir, BundledHarnessesOnly: true},
			want: "planted-own",
			bad:  "planted-user",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfg
			d := NewDaemon(&cfg)
			// NewDaemon starts the transcript watcher; a test that never runs
			// the daemon has to close it or the process keeps an inotify
			// descriptor for the rest of the run.
			t.Cleanup(func() { _ = d.transcriptWatcher.Close() })
			if d.agentMatcher.registry.Lookup(tc.want) == nil {
				t.Errorf("manifest %q was not loaded; loaded: %v", tc.want, d.agentMatcher.registry.IDs())
			}
			if d.agentMatcher.registry.Lookup(tc.bad) != nil {
				t.Errorf("manifest %q was loaded, and this daemon must not read that directory; loaded: %v", tc.bad, d.agentMatcher.registry.IDs())
			}
		})
	}
}
