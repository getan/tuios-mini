package theme

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/adrg/xdg"
	tint "github.com/lrstanley/bubbletint/v2"
)

// These are the checks behind ScopeAssets: a workspace's custom themes and
// glyph sets come from the tree its host gave it, the person's directories are
// neither read nor made, and the process — the next workspace, and the host's
// own screens — is exactly as it was when the scope ends.

// userConfigHome points the user's config tree at a fresh directory and returns
// it. xdg resolves its base directories once at init, so the reload is what
// makes the redirect real.
func userConfigHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Cleanup(xdg.Reload)
	t.Setenv("XDG_CONFIG_HOME", dir)
	xdg.Reload()
	return dir
}

// writeAsset writes one JSON file of a theme or glyph set into dir.
func writeAsset(t *testing.T, dir, name string, body map[string]any) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestScopedAssetsReadTheHostsTreeAndNotTheUsers is the whole point of the
// scope: the person's themes and glyph sets are invisible inside it, the host's
// are in force, and the user's directories are not created on the way past.
func TestScopedAssetsReadTheHostsTreeAndNotTheUsers(t *testing.T) {
	configHome := userConfigHome(t)
	userThemes := filepath.Join(configHome, "tuios", "themes")
	userGlyphs := filepath.Join(configHome, "tuios", "glyphs")
	writeAsset(t, userThemes, "theirs.json", map[string]any{"id": "theirs", "fg": "#ffffff", "bg": "#000000"})
	writeAsset(t, userGlyphs, "theirset.json", map[string]any{"bullet": "T"})

	if !Exists("theirs") {
		t.Fatal("the unscoped process cannot see the user's own theme; the fixture is wrong")
	}

	root := t.TempDir()
	themesDir, glyphsDir := filepath.Join(root, "themes"), filepath.Join(root, "glyphs")
	writeAsset(t, themesDir, "ours.json", map[string]any{"id": "ours", "fg": "#101010", "bg": "#f0f0f0"})
	writeAsset(t, glyphsDir, "ourset.json", map[string]any{"bullet": "O"})

	restore, err := ScopeAssets(themesDir, glyphsDir)
	if err != nil {
		t.Fatalf("ScopeAssets: %v", err)
	}
	defer restore()

	gotThemes, gotGlyphs, ok := ScopedAssetDirs()
	if !ok || gotThemes != themesDir || gotGlyphs != glyphsDir {
		t.Fatalf("ScopedAssetDirs = %q, %q, %v; want %q, %q", gotThemes, gotGlyphs, ok, themesDir, glyphsDir)
	}
	if dir, err := GetThemesDir(); err != nil || dir != themesDir {
		t.Errorf("GetThemesDir = %q, %v; want the host's %q", dir, err, themesDir)
	}
	if dir, err := GetGlyphsDir(); err != nil || dir != glyphsDir {
		t.Errorf("GetGlyphsDir = %q, %v; want the host's %q", dir, err, glyphsDir)
	}

	// The user's files are not readable through this process any more.
	if Exists("theirs") {
		t.Error("a theme of the user's is visible inside a scoped workspace")
	}
	for _, id := range AvailableGlyphSets() {
		if id == "theirset" {
			t.Error("a glyph set of the user's is visible inside a scoped workspace")
		}
	}

	// The host's own are.
	if !Exists("ours") {
		t.Error("the workspace cannot see the theme file in its own tree")
	}
	SetActiveGlyphs("ourset")
	if got := ActiveGlyphSetID(); got != "ourset" {
		t.Errorf("ActiveGlyphSetID = %q, want ourset", got)
	}
	if got := Glyphs().Bullet; got != "O" {
		t.Errorf("the drawn bullet is %q, want the workspace's O", got)
	}

	// Nothing in the person's tree was read or made. The themes and glyphs
	// directories this scope does not use are still the only files under it,
	// and no tuios directory appeared beside them.
	entries, err := os.ReadDir(filepath.Join(configHome, "tuios"))
	if err != nil {
		t.Fatalf("the user's config tree: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 2 || names[0] != "glyphs" || names[1] != "themes" {
		t.Errorf("the scoped run wrote into the user's config tree: %v", names)
	}
}

// TestScopedAssetsAreGivenBack is the restore: the theme in force, the glyph
// set in force and the registry itself are the ones the process had, so the
// host's own screens and the next workspace draw what they drew.
func TestScopedAssetsAreGivenBack(t *testing.T) {
	configHome := userConfigHome(t)
	userThemes := filepath.Join(configHome, "tuios", "themes")
	userGlyphs := filepath.Join(configHome, "tuios", "glyphs")
	writeAsset(t, userThemes, "theirs.json", map[string]any{"id": "theirs", "fg": "#ffffff", "bg": "#000000"})
	writeAsset(t, userGlyphs, "theirset.json", map[string]any{"bullet": "T"})

	// What the process is drawing before the workspace runs.
	if err := Initialize("theirs"); err != nil {
		t.Fatalf("Initialize(theirs): %v", err)
	}
	SetActiveGlyphs("theirset")
	t.Cleanup(func() { SetActiveGlyphs(GlyphSetNone) })
	beforeID, beforeGlyphs := CurrentThemeID(), ActiveGlyphSetID()
	beforeRegistry := tint.DefaultRegistry

	root := t.TempDir()
	themesDir, glyphsDir := filepath.Join(root, "themes"), filepath.Join(root, "glyphs")
	writeAsset(t, themesDir, "ours.json", map[string]any{"id": "ours", "fg": "#101010", "bg": "#f0f0f0"})
	writeAsset(t, glyphsDir, "ourset.json", map[string]any{"bullet": "O"})

	restore, err := ScopeAssets(themesDir, glyphsDir)
	if err != nil {
		t.Fatalf("ScopeAssets: %v", err)
	}
	if err := Initialize("ours"); err != nil {
		t.Fatalf("Initialize(ours): %v", err)
	}
	SetActiveGlyphs("ourset")
	if CurrentThemeID() != "ours" || ActiveGlyphSetID() != "ourset" {
		t.Fatalf("inside the scope the theme is %q and the glyph set %q; want ours and ourset",
			CurrentThemeID(), ActiveGlyphSetID())
	}

	restore()
	// The second call is the defer's, and is a no-op: a scope closed twice must
	// not put back a state it no longer holds.
	restore()

	if got := CurrentThemeID(); got != beforeID {
		t.Errorf("after the scope the theme is %q, want %q", got, beforeID)
	}
	if got := ActiveGlyphSetID(); got != beforeGlyphs {
		t.Errorf("after the scope the glyph set is %q, want %q", got, beforeGlyphs)
	}
	if got := Glyphs().Bullet; got != "T" {
		t.Errorf("after the scope the drawn bullet is %q, want the user's T", got)
	}
	if tint.DefaultRegistry != beforeRegistry {
		t.Error("the tint registry in force after the scope is not the process's own object")
	}
	if Exists("ours") {
		t.Error("the workspace's own theme outlived its scope")
	}
	if !Exists("theirs") {
		t.Error("the user's theme is gone after the scope ended")
	}
	if dir, err := GetThemesDir(); err != nil || dir != userThemes {
		t.Errorf("after the scope GetThemesDir = %q, %v; want the user's %q", dir, err, userThemes)
	}
	if _, _, ok := ScopedAssetDirs(); ok {
		t.Error("the asset scope is still open after it was restored")
	}
}

// TestTwoWorkspacesShareNoCustomAssets is the sequential case, which is what a
// host that runs one project and then another does: neither project's custom
// theme or glyph set may be in force in the other.
func TestTwoWorkspacesShareNoCustomAssets(t *testing.T) {
	userConfigHome(t)

	first, second := t.TempDir(), t.TempDir()
	writeAsset(t, filepath.Join(first, "themes"), "first.json", map[string]any{"id": "first", "fg": "#010101", "bg": "#fefefe"})
	writeAsset(t, filepath.Join(first, "glyphs"), "firstset.json", map[string]any{"bullet": "1"})
	writeAsset(t, filepath.Join(second, "themes"), "second.json", map[string]any{"id": "second", "fg": "#020202", "bg": "#fdfdfd"})

	restoreFirst, err := ScopeAssets(filepath.Join(first, "themes"), filepath.Join(first, "glyphs"))
	if err != nil {
		t.Fatalf("the first scope: %v", err)
	}
	if !Exists("first") {
		t.Error("the first workspace cannot see its own theme")
	}
	SetActiveGlyphs("firstset")
	if got := Glyphs().Bullet; got != "1" {
		t.Errorf("the first workspace's bullet is %q, want 1", got)
	}
	restoreFirst()

	restoreSecond, err := ScopeAssets(filepath.Join(second, "themes"), filepath.Join(second, "glyphs"))
	if err != nil {
		t.Fatalf("the second scope: %v", err)
	}
	defer restoreSecond()

	if Exists("first") {
		t.Error("the second workspace inherited the first's custom theme")
	}
	if got := ActiveGlyphSetID(); got != GlyphSetNone {
		t.Errorf("the second workspace started on glyph set %q, want the built-in default", got)
	}
	for _, id := range AvailableGlyphSets() {
		if id == "firstset" {
			t.Error("the second workspace inherited the first's glyph set")
		}
	}
	if !Exists("second") {
		t.Error("the second workspace cannot see its own theme")
	}
}

// TestAssetScopesAreExclusive: two scopes at once would each read the other's
// directories, so the second is refused — and the refusal leaves the first
// working and the process restorable.
func TestAssetScopesAreExclusive(t *testing.T) {
	userConfigHome(t)
	root := t.TempDir()
	restore, err := ScopeAssets(filepath.Join(root, "themes"), filepath.Join(root, "glyphs"))
	if err != nil {
		t.Fatalf("the first scope: %v", err)
	}

	other := t.TempDir()
	if _, err := ScopeAssets(filepath.Join(other, "themes"), filepath.Join(other, "glyphs")); err == nil {
		t.Fatal("a second asset scope was accepted while one was open")
	}
	if dir, _, ok := ScopedAssetDirs(); !ok || dir != filepath.Join(root, "themes") {
		t.Errorf("the refused scope disturbed the open one: %q, %v", dir, ok)
	}

	restore()
	if _, _, ok := ScopedAssetDirs(); ok {
		t.Error("the asset scope is still open after it was restored")
	}
	restoreOther, err := ScopeAssets(filepath.Join(other, "themes"), filepath.Join(other, "glyphs"))
	if err != nil {
		t.Fatalf("a scope after a restored one was refused: %v", err)
	}
	restoreOther()
}

// TestAssetScopeRefusesADirectoryItWasNotGiven: a scope with no directory would
// leave that lookup pointed at the user's tree, which is the one thing it must
// not do.
func TestAssetScopeRefusesADirectoryItWasNotGiven(t *testing.T) {
	if _, err := ScopeAssets("", t.TempDir()); err == nil {
		t.Error("ScopeAssets accepted an empty themes directory")
	}
	if _, err := ScopeAssets(t.TempDir(), ""); err == nil {
		t.Error("ScopeAssets accepted an empty glyphs directory")
	}
	if _, _, ok := ScopedAssetDirs(); ok {
		t.Error("a refused scope left the asset directories scoped")
	}
}
