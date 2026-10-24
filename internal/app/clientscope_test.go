package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/adrg/xdg"

	"github.com/getan/tuios-mini/pkg/applist"
)

// These are the checks behind ScopeClientState. The client keeps a handful of
// small files of its own — the sidebar, the launcher's history, saved layouts,
// tapes, a crash report — each derived from an XDG base directory. A host that
// embedded the client inside a tree of its own must find every one of them
// there, and the person's directories must be exactly as they were.

// TestScopedClientStateMovesEveryFile is the audit of that list: each path is
// asked for inside the scope and after it, and each one moves.
func TestScopedClientStateMovesEveryFile(t *testing.T) {
	userState := filepath.Join(xdg.StateHome, "tuios")
	userLayouts := filepath.Join(xdg.ConfigHome, "tuios", "layouts")
	// What GetTapeDirectory answers for a process that is not scoped. It is the
	// parent of "tuios/tapes", which is what that function has always returned:
	// xdg.DataFile makes the directory of the path it is given and the code
	// takes its parent. A scoped tree is new and has no such history, so its
	// tapes go in a directory of their own: <state>/tapes.
	userTapes := filepath.Join(xdg.DataHome, "tuios")

	// What the user's process answers before anything is scoped.
	if got := sidebarStateDir(); got != userState {
		t.Fatalf("sidebarStateDir = %q, want %q", got, userState)
	}
	if got := CrashLogDir(); got != userState {
		t.Errorf("CrashLogDir = %q, want %q", got, userState)
	}
	if got := GetTemplatesDir(); got != userLayouts {
		t.Errorf("GetTemplatesDir = %q, want %q", got, userLayouts)
	}
	if got := applist.DefaultPath(); got != filepath.Join(userState, "launcher.json") {
		t.Errorf("applist.DefaultPath = %q, want it under %q", got, userState)
	}
	if got, err := GetTapeDirectory(); err != nil || got != userTapes {
		t.Errorf("GetTapeDirectory = %q, %v; want %q", got, err, userTapes)
	}

	dir := t.TempDir()
	restore, err := ScopeClientState(dir)
	if err != nil {
		t.Fatalf("ScopeClientState: %v", err)
	}

	if got, ok := ScopedClientState(); !ok || got != dir {
		t.Fatalf("ScopedClientState = %q, %v; want %q", got, ok, dir)
	}
	if got := sidebarStateDir(); got != dir {
		t.Errorf("inside the scope sidebarStateDir = %q, want %q", got, dir)
	}
	if got := CrashLogDir(); got != dir {
		t.Errorf("inside the scope CrashLogDir = %q, want %q", got, dir)
	}
	if got := GetTemplatesDir(); got != filepath.Join(dir, "layouts") {
		t.Errorf("inside the scope GetTemplatesDir = %q, want it under %q", got, dir)
	}
	if got := applist.DefaultPath(); got != filepath.Join(dir, "launcher.json") {
		t.Errorf("inside the scope applist.DefaultPath = %q, want it under %q", got, dir)
	}
	if got, err := GetTapeDirectory(); err != nil || got != filepath.Join(dir, "tapes") {
		t.Errorf("inside the scope GetTapeDirectory = %q, %v; want it under %q", got, err, dir)
	}
	// The two a diagnostic can ask for are asked for here too, so a path added
	// to the family without going through uiStateDir is caught: every one of
	// them has to sit inside the tree the host owns.
	for _, path := range []string{defaultRenderTracePath(), capabilitiesDebugPath()} {
		if !strings.HasPrefix(path, dir+string(os.PathSeparator)) {
			t.Errorf("a diagnostic path %q is outside the scoped tree %q", path, dir)
		}
	}

	restore()
	// Closing twice is what a host that also defers it does.
	restore()

	if _, ok := ScopedClientState(); ok {
		t.Error("the client scope is still open after it was restored")
	}
	if got := sidebarStateDir(); got != userState {
		t.Errorf("after the scope sidebarStateDir = %q, want the user's %q", got, userState)
	}
	if got := applist.DefaultPath(); got != filepath.Join(userState, "launcher.json") {
		t.Errorf("after the scope applist.DefaultPath = %q, want the user's", got)
	}
	if got, err := GetTapeDirectory(); err != nil || got != userTapes {
		t.Errorf("after the scope GetTapeDirectory = %q, %v; want the user's %q", got, err, userTapes)
	}
}

// TestAWorkspacesSidebarIsItsOwn: the file is keyed by nothing but the
// directory it lives in, so two clients sharing the user's state directory is
// two clients overwriting each other's rail. Scoped, each has its own.
func TestAWorkspacesSidebarIsItsOwn(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	for i, dir := range []string{first, second} {
		restore, err := ScopeClientState(dir)
		if err != nil {
			t.Fatalf("ScopeClientState(%s): %v", dir, err)
		}
		path := filepath.Join(dir, sidebarStateFileName)
		body := "{\"order\":[\"session-" + strconv.Itoa(i) + "\"]}"
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		restore()
	}

	body := func(dir string) string {
		data, err := os.ReadFile(filepath.Join(dir, sidebarStateFileName))
		if err != nil {
			t.Fatalf("read the sidebar state in %s: %v", dir, err)
		}
		return string(data)
	}
	if body(first) == body(second) {
		t.Errorf("two scopes hold the same sidebar state: %q", body(first))
	}
}

// TestClientScopesAreExclusive: two clients in one process would each read the
// other's sidebar, so the second scope is refused, and the refusal leaves the
// first untouched.
func TestClientScopesAreExclusive(t *testing.T) {
	dir := t.TempDir()
	restore, err := ScopeClientState(dir)
	if err != nil {
		t.Fatalf("the first scope: %v", err)
	}
	if _, err := ScopeClientState(t.TempDir()); err == nil {
		t.Fatal("a second client scope was accepted while one was open")
	}
	if got, ok := ScopedClientState(); !ok || got != dir {
		t.Errorf("the refused scope disturbed the open one: %q, %v", got, ok)
	}
	restore()
	if got, err := ScopeClientState(filepath.Join(dir, "next")); err != nil || got == nil {
		t.Fatalf("a scope after a restored one was refused: %v", err)
	} else {
		got()
	}
}

// TestClientScopeRefusesADirectoryItWasNotGiven: a scope with no directory
// would leave the client writing the user's files, which is the whole of what
// it is for.
func TestClientScopeRefusesADirectoryItWasNotGiven(t *testing.T) {
	if _, err := ScopeClientState(""); err == nil {
		t.Error("ScopeClientState accepted an empty directory")
	}
	if _, ok := ScopedClientState(); ok {
		t.Error("a refused scope left the client scoped")
	}
}
