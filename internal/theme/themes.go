package theme

import (
	"image/color"
	"log"
	"sort"
	"sync"

	"charm.land/lipgloss/v2"
	tint "github.com/lrstanley/bubbletint/v2"
)

// defaultSwatch is the preview palette shown for the "no theme" option (the
// standard xterm-ish bright colors).
var defaultSwatch = []color.Color{
	lipgloss.Color("#ff5555"), lipgloss.Color("#f1fa8c"), lipgloss.Color("#50fa7b"),
	lipgloss.Color("#8be9fd"), lipgloss.Color("#6272a4"), lipgloss.Color("#bd93f9"),
	lipgloss.Color("#f8f8f2"), lipgloss.Color("#282a36"),
}

// ThemeSwatch returns a small, representative set of colors for a theme id, for
// previewing it in the theme picker. Unknown or empty ids return the default
// palette.
func ThemeSwatch(id string) []color.Color {
	EnsureRegistry()
	t, ok := tint.GetTint(id)
	if !ok || t == nil {
		return defaultSwatch
	}
	return []color.Color{
		t.BrightRed, t.BrightYellow, t.BrightGreen,
		t.BrightCyan, t.BrightBlue, t.BrightPurple,
		t.Fg, t.Bg,
	}
}

// EnsureRegistry populates the tint registry with the built-in tints and any
// custom themes, without enabling theming or changing the current theme. This
// lets the settings page list and preview themes even when the session started
// with no theme selected.
func EnsureRegistry() {
	ensureRegistry.Do(func() {
		newBuiltinRegistry()
		registerCharmToneThemes()
		registerOmarchyThemes()
		rememberBuiltins()
		if themesDir, err := GetThemesDir(); err == nil {
			if _, err := LoadCustomThemes(themesDir); err != nil {
				log.Printf("Warning: error loading custom themes: %v", err)
			}
		}
	})
}

// ensureRegistry is the once behind EnsureRegistry.
//
// It is sync.Once with two things added: whether it has run, and a way to say
// it has not. The registry is built once per process, which is right for a
// process that is one client and wrong for a host that runs one client after
// another: a scope that reads a second project's themes directory needs the
// registry built again from that directory, and the host needs its own back
// afterwards. See ScopeAssets.
var ensureRegistry = &resettableOnce{}

type resettableOnce struct {
	mu  sync.Mutex
	ran bool
}

// Do runs f unless it has already run. The flag is set before f runs, so a
// caller that reaches here from inside f returns rather than waiting on itself.
func (o *resettableOnce) Do(f func()) {
	o.mu.Lock()
	if o.ran {
		o.mu.Unlock()
		return
	}
	o.ran = true
	o.mu.Unlock()
	f()
}

// done reports whether f has run.
func (o *resettableOnce) done() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.ran
}

// set records whether f has run, which is how a scope puts back the process's
// own answer.
func (o *resettableOnce) set(done bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.ran = done
}

// AvailableThemes returns the sorted list of registered theme IDs (built-in
// tints plus any custom themes loaded from the user's themes directory). The
// list is used by the in-app settings page to cycle themes.
func AvailableThemes() []string {
	EnsureRegistry()
	ids := tint.TintIDs()
	sorted := make([]string, len(ids))
	copy(sorted, ids)
	sort.Strings(sorted)
	return sorted
}

// CurrentThemeID returns the ID of the active theme, or an empty string when
// theming is disabled.
func CurrentThemeID() string {
	t := Current()
	if t == nil {
		return ""
	}
	return t.ID
}

// ThemeIsDark reports whether a registered theme is a dark one, as its file or
// the built-in table says.
func ThemeIsDark(id string) (dark, ok bool) {
	t, ok := tint.GetTint(id)
	if !ok || t == nil {
		return false, false
	}
	return t.Dark, true
}
