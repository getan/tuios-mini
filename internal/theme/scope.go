package theme

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sync"

	"github.com/adrg/xdg"
	tint "github.com/lrstanley/bubbletint/v2"
	"tuios.dev/swatch"

	"github.com/getan/tuios-mini/internal/overlay"
)

// The themes and glyph sets a client draws with are two directories and a pile
// of process-wide tables: the tint registry, the chrome that hangs off each
// theme, the bases a file resolved against, the glyph sets read from disk and
// the one in force.
//
// A standalone client reads the user's own directories and the tables are its
// own. A host that embeds the client and owns a tree of its own is not that
// user, and one project is not another: a workspace must not read the person's
// ~/.config/tuios/themes, must not create the directories GetThemesDir and
// GetGlyphsDir make on the way past, and must not leave its custom theme or its
// glyph set in the process for the next workspace to inherit.
//
// So the directories and the tables are a scope, not constants. ScopeAssets
// points the two directory lookups at paths the host owns and gives the process
// back, on the way out, every table it had. Nothing else is redirected: the
// built-in themes, the Omarchy source and the config file are where they were,
// and an unscoped process — every standalone client — behaves exactly as it did
// before this existed.

// errNoAssetDir is returned when a scope was asked for without a directory.
// Both are required: a scope that named none would leave that lookup pointed at
// the user's tree, which is the one thing this must not do.
var errNoAssetDir = errors.New("theme: a scope needs the themes directory and the glyphs directory the host owns")

// errAssetScopeOpen is returned when a scope is asked for while another is
// open.
type errAssetScopeOpen struct{ dir string }

func (e *errAssetScopeOpen) Error() string {
	return fmt.Sprintf("theme: %s is already in scope; end that scope before starting another", e.dir)
}

// assetScope is the directories a host scoped this process to, when there is
// one. There is at most one because the tables above are process-wide: two
// clients scoping two directories at once would each read the other's.
var assetScope struct {
	mu        sync.Mutex
	themesDir string
	glyphsDir string
	open      bool
}

// ScopedAssetDirs reports the themes and glyphs directories a host scoped this
// process to, and whether there is a scope. It is how an embedding host answers
// "which files is this client actually reading" without asking the client.
func ScopedAssetDirs() (themesDir, glyphsDir string, ok bool) {
	assetScope.mu.Lock()
	defer assetScope.mu.Unlock()
	return assetScope.themesDir, assetScope.glyphsDir, assetScope.open
}

// assetDir is the directory one class of asset is read from: the host's when
// this process is scoped, the user's own otherwise.
//
// name is "themes" or "glyphs" and only ever those two: it becomes a folder in
// the user's config tree. The scoped branch makes the directory rather than
// leaving that to xdg.ConfigFile, because a host's tree is its own to make and
// nothing else creates it.
func assetDir(name string) (string, error) {
	if dir, ok := scopedAssetDir(name); ok {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", fmt.Errorf("theme: create %s: %w", dir, err)
		}
		return dir, nil
	}
	// Use xdg.ConfigFile to get the path and ensure parent dirs exist
	keepFile, err := xdg.ConfigFile("tuios/" + name + "/.keep")
	if err != nil {
		return "", fmt.Errorf("failed to get %s directory: %w", name, err)
	}
	return filepath.Dir(keepFile), nil
}

// scopedAssetDir is assetDir's answer when this process is scoped.
func scopedAssetDir(name string) (string, bool) {
	assetScope.mu.Lock()
	defer assetScope.mu.Unlock()
	if !assetScope.open {
		return "", false
	}
	if name == "glyphs" {
		return assetScope.glyphsDir, true
	}
	return assetScope.themesDir, true
}

// ScopeAssets makes this package read themes from themesDir and glyph sets from
// glyphsDir instead of the user's directories, and gives the process back the
// theme and glyph state it had. It returns the function that ends the scope.
//
// The tables this saves are the whole of what a client can change: the tint
// registry, whether themes are on, which theme is in force, the chrome and the
// bases behind it, the Omarchy palette, the border overrides, the glyph sets
// read from disk and the set in force. Inside the scope the registry is rebuilt
// from the host's directories, so a custom theme or a glyph set of another
// project cannot be inherited. On restore the process is put back exactly as it
// was, which is what lets a host go back to its own screens — and what lets the
// next workspace start from nothing rather than from this one.
//
// Restoring is safe to call more than once and from a defer. A second scope
// while one is open is an error rather than a nesting: this is process-wide
// state. The directories are made when something reads them, not here, so a
// run that never draws anything creates nothing.
func ScopeAssets(themesDir, glyphsDir string) (restore func(), err error) {
	if themesDir == "" || glyphsDir == "" {
		return nil, errNoAssetDir
	}
	themesAbs, err := filepath.Abs(themesDir)
	if err != nil {
		return nil, fmt.Errorf("theme: resolve %s: %w", themesDir, err)
	}
	glyphsAbs, err := filepath.Abs(glyphsDir)
	if err != nil {
		return nil, fmt.Errorf("theme: resolve %s: %w", glyphsDir, err)
	}

	assetScope.mu.Lock()
	if assetScope.open {
		open := assetScope.themesDir
		assetScope.mu.Unlock()
		return nil, &errAssetScopeOpen{dir: open}
	}
	previous := takeState()
	assetScope.themesDir, assetScope.glyphsDir, assetScope.open = themesAbs, glyphsAbs, true
	assetScope.mu.Unlock()

	// Inside the scope from here on. The tables are emptied and the registry is
	// built again from the host's directories, so nothing of the outer process
	// — a user's custom theme, another project's glyph set — is in force while
	// this client runs.
	clearState()
	EnsureRegistry()
	refreshActiveGlyphs()

	var once sync.Once
	return func() {
		once.Do(func() {
			assetScope.mu.Lock()
			assetScope.themesDir, assetScope.glyphsDir, assetScope.open = "", "", false
			assetScope.mu.Unlock()
			putState(previous)
		})
	}, nil
}

// state is everything this package keeps in the process that a client can
// change. It is a value, and every map in it is a copy, so putting it back
// cannot be undone by whatever the scope did to the tables in the meantime.
type state struct {
	// registry is the tint registry itself. bubbletint keeps it behind one
	// package variable, so saving the pointer is saving the registry: the
	// scope's is a new one and the outer process's is untouched.
	registry *tint.Registry
	// built records whether EnsureRegistry had run. The registry is built once
	// per process, and a scope that rebuilt it has to put that back too or the
	// process would build a second one over its own.
	built bool

	chrome   map[string]*Chrome
	builtins map[string]resolvedTheme
	customs  map[string]resolvedTheme

	enabled    bool
	matchHost  bool
	generation uint64
	omarchy    swatch.Palette
	omarchyOK  bool
	host       *hostGround

	borderFocused   ColorSpec
	borderUnfocused ColorSpec

	glyphSets        map[string]*GlyphSet
	glyphProblems    []string
	glyphProblemsSet map[string][]string
	activeGlyphID    string
}

// takeState copies the process's theme and glyph tables.
func takeState() state {
	s := state{
		registry:   tint.DefaultRegistry,
		built:      ensureRegistry.done(),
		enabled:    enabled,
		matchHost:  matchHost.Load(),
		generation: generation.Load(),
		host:       host.Load(),
	}

	chromeRegistry.RLock()
	s.chrome = maps.Clone(chromeRegistry.byID)
	chromeRegistry.RUnlock()

	themeBases.RLock()
	s.builtins, s.customs = maps.Clone(themeBases.builtins), maps.Clone(themeBases.customs)
	themeBases.RUnlock()

	omarchyState.Lock()
	s.omarchy, s.omarchyOK = omarchyState.palette, omarchyState.valid
	omarchyState.Unlock()

	borderMu.RLock()
	s.borderFocused, s.borderUnfocused = borderFocusedOverride, borderUnfocusedOverride
	borderMu.RUnlock()

	glyphMu.RLock()
	s.glyphSets = maps.Clone(userGlyphSets)
	s.glyphProblems = append([]string(nil), glyphProblems...)
	s.glyphProblemsSet = make(map[string][]string, len(glyphProblemsBySet))
	for id, lines := range glyphProblemsBySet {
		s.glyphProblemsSet[id] = append([]string(nil), lines...)
	}
	s.activeGlyphID = activeGlyphID
	glyphMu.RUnlock()

	return s
}

// putState puts the process's tables back.
func putState(s state) {
	// The registry first: everything below either reads it or is read against
	// it, and the active theme's colours come from it.
	tint.DefaultRegistry = s.registry
	ensureRegistry.set(s.built)
	enabled = s.enabled
	matchHost.Store(s.matchHost)
	generation.Store(s.generation)
	host.Store(s.host)

	chromeRegistry.Lock()
	chromeRegistry.byID = s.chrome
	chromeRegistry.Unlock()

	themeBases.Lock()
	themeBases.builtins, themeBases.customs = s.builtins, s.customs
	themeBases.Unlock()

	omarchyState.Lock()
	omarchyState.palette, omarchyState.valid = s.omarchy, s.omarchyOK
	omarchyState.Unlock()

	borderMu.Lock()
	borderFocusedOverride, borderUnfocusedOverride = s.borderFocused, s.borderUnfocused
	borderMu.Unlock()

	glyphMu.Lock()
	userGlyphSets, glyphProblems, glyphProblemsBySet = s.glyphSets, s.glyphProblems, s.glyphProblemsSet
	activeGlyphID = s.activeGlyphID
	glyphMu.Unlock()
	// Pushes the restored set to the overlay family, which is the one consumer
	// that reads the glyphs out of band. Without it the chrome would keep
	// drawing the scoped set's ellipsis over the outer process's theme.
	refreshActiveGlyphs()
}

// clearState empties the tables a scope starts from. The registry is left
// alone: it is replaced by EnsureRegistry's own, which happens before anything
// can read an empty one — and bubbletint's Current panics on a registry with no
// tints at all.
func clearState() {
	ensureRegistry.set(false)

	enabled = false
	matchHost.Store(false)
	host.Store(nil)

	chromeRegistry.Lock()
	chromeRegistry.byID = nil
	chromeRegistry.Unlock()

	themeBases.Lock()
	themeBases.builtins, themeBases.customs = nil, nil
	themeBases.Unlock()

	borderMu.Lock()
	borderFocusedOverride, borderUnfocusedOverride = ColorSpec{}, ColorSpec{}
	borderMu.Unlock()

	glyphMu.Lock()
	userGlyphSets, glyphProblems, glyphProblemsBySet = nil, nil, nil
	activeGlyphID = GlyphSetNone
	glyphMu.Unlock()

	// The overlay family's glyphs are its own copy, so they are blanked here
	// rather than waiting for the refresh that follows the registry being
	// built: a set of one project's arrows must not outlive its scope even for
	// the length of a config load.
	overlay.SetChrome(&overlay.Chrome{})
	activeGlyphs.Store(nil)
}
