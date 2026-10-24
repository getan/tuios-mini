package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adrg/xdg"
)

// The scope is a security boundary, not a convenience: a host that says "this
// client's config is mine" must not have the person's own config.toml read
// behind its back, and must not have the settings page write there. These
// tests hold each of the four default lookups to the scoped path, and hold the
// unscoped process to the user's.
//
// TestMain (main_test.go) points every XDG directory at a throwaway tree, so
// "the user's file" below is that tree's, never the developer's.

// userConfigPath is where the person's own config.toml is in this test binary.
func userConfigPath(t *testing.T) string {
	t.Helper()
	path, err := xdg.ConfigFile("tuios/config.toml")
	if err != nil {
		t.Fatalf("resolve the user's config path: %v", err)
	}
	return path
}

// writeFile writes data at path, making the directory.
func writeFile(t *testing.T, path string, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestScopeConfigPathRedirectsEveryDefaultLookup is the whole contract at
// once: GetConfigPath, LoadUserConfig, SaveUserConfig and EffectiveDefault all
// answer from the scoped file, and the person's file is neither read nor
// written while the scope is open.
func TestScopeConfigPathRedirectsEveryDefaultLookup(t *testing.T) {
	userPath := userConfigPath(t)
	// The startup table is the part the last check reads: the user's file says
	// the daemon default is on, and the scoped file below says off, so the two
	// answers cannot be confused.
	writeFile(t, userPath, "[appearance]\nborder_style = \"double\"\n\n[startup]\ndaemon = true\n")
	before, err := os.ReadFile(userPath)
	if err != nil {
		t.Fatalf("read the user's config: %v", err)
	}

	scoped := filepath.Join(t.TempDir(), "workspace", "config.toml")
	restore, err := ScopeConfigPath(scoped)
	if err != nil {
		t.Fatalf("scope: %v", err)
	}
	t.Cleanup(restore)

	got, ok := ScopedConfigPath()
	if !ok || got != scoped {
		t.Fatalf("ScopedConfigPath() = %q, %v; want %q, true", got, ok, scoped)
	}

	// Where it would be created, before anything exists.
	path, err := GetConfigPath()
	if err != nil {
		t.Fatalf("GetConfigPath: %v", err)
	}
	if path != scoped {
		t.Fatalf("GetConfigPath() = %q, want the scoped %q", path, scoped)
	}

	// The first load creates the scoped file and reads nothing of the user's:
	// the user's border_style must not reach this config.
	cfg, err := LoadUserConfig()
	if err != nil {
		t.Fatalf("LoadUserConfig: %v", err)
	}
	if _, statErr := os.Stat(scoped); statErr != nil {
		t.Fatalf("the first load did not create %s: %v", scoped, statErr)
	}
	if cfg.Appearance.BorderStyle == "double" {
		t.Fatal("the scoped load read the user's config file")
	}

	// A save goes to the scoped file.
	cfg.Appearance.BorderStyle = "thick"
	if _, err := SaveUserConfig(cfg); err != nil {
		t.Fatalf("SaveUserConfig: %v", err)
	}
	saved, err := os.ReadFile(scoped)
	if err != nil {
		t.Fatalf("read the scoped config: %v", err)
	}
	if !strings.Contains(string(saved), `border_style = "thick"`) {
		t.Errorf("the save did not reach %s:\n%s", scoped, saved)
	}
	after, err := os.ReadFile(userPath)
	if err != nil {
		t.Fatalf("read the user's config: %v", err)
	}
	if string(after) != string(before) {
		t.Errorf("the user's config changed while a scope was open:\nbefore:\n%s\nafter:\n%s", before, after)
	}

	// EffectiveDefault reads the scoped file too: the [startup] table written
	// there is what this process's startup booleans default to, and it is not
	// the user's.
	writeFile(t, scoped, "[startup]\ndaemon = false\n")
	if got := EffectiveDefault(Option{Path: "startup.daemon", Default: "true"}); got != "false" {
		t.Errorf("EffectiveDefault(startup.daemon) = %q, want the scoped file's false", got)
	}
}

// TestScopeConfigPathRestoresTheUsersFile: once the scope ends, every lookup
// is the user's again. That is what makes it safe to run an embedded client
// and then anything else in the same process.
func TestScopeConfigPathRestoresTheUsersFile(t *testing.T) {
	userPath := userConfigPath(t)
	writeFile(t, userPath, "[appearance]\nborder_style = \"double\"\n")

	scoped := filepath.Join(t.TempDir(), "config.toml")
	restore, err := ScopeConfigPath(scoped)
	if err != nil {
		t.Fatalf("scope: %v", err)
	}
	if _, err := LoadUserConfig(); err != nil {
		t.Fatalf("load inside the scope: %v", err)
	}
	restore()
	// A second call is a defer's right, not an error.
	restore()

	if _, ok := ScopedConfigPath(); ok {
		t.Fatal("the scope is still open after restoring it")
	}
	path, err := GetConfigPath()
	if err != nil {
		t.Fatalf("GetConfigPath after restore: %v", err)
	}
	if path != userPath {
		t.Errorf("GetConfigPath() = %q after restore, want the user's %q", path, userPath)
	}
	cfg, err := LoadUserConfig()
	if err != nil {
		t.Fatalf("LoadUserConfig after restore: %v", err)
	}
	if cfg.Appearance.BorderStyle != "double" {
		t.Errorf("the restored load did not read the user's config: border_style = %q", cfg.Appearance.BorderStyle)
	}
}

// TestSecondScopeIsRefused: the scope is process-wide, so two of them cannot
// be open at once. A host that tried would have two clients reading one file,
// and the second one would silently win.
func TestSecondScopeIsRefused(t *testing.T) {
	first := filepath.Join(t.TempDir(), "one.toml")
	restore, err := ScopeConfigPath(first)
	if err != nil {
		t.Fatalf("first scope: %v", err)
	}
	defer restore()

	if _, err := ScopeConfigPath(filepath.Join(t.TempDir(), "two.toml")); err == nil {
		t.Fatal("a second scope was allowed; the two clients would read one file")
	} else if !strings.Contains(err.Error(), first) {
		t.Errorf("the refusal does not name the open scope: %v", err)
	}

	if _, err := ScopeConfigPath(""); err == nil {
		t.Error("a scope with no path was allowed")
	}
}

// TestScopedPathIsAbsolute pins that the scope is resolved when it is taken:
// a load that happens after the host changed directory must still reach the
// file the host named.
func TestScopedPathIsAbsolute(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	restore, err := ScopeConfigPath("config.toml")
	if err != nil {
		t.Fatalf("scope: %v", err)
	}
	defer restore()

	got, ok := ScopedConfigPath()
	if !ok {
		t.Fatal("the scope is not open")
	}
	if !filepath.IsAbs(got) || filepath.Dir(got) != dir {
		t.Errorf("ScopedConfigPath() = %q, want an absolute path in %q", got, dir)
	}
}
