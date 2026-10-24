package wire

import (
	"os/exec"
	"sort"
	"strings"
	"testing"
)

// What this package is for is the bytes, and nothing else. If it ever imports
// the session package, the daemon, the TUI or the workspace, the "extraction"
// is a rename: the code would still be reachable only from the whole product,
// and the claim that the protocol is a library would be false. One import
// anywhere in the package, a test included, is enough to break it, and a human
// reading a diff would not see it.
//
// So the check is on the import graph rather than on the linked binary.

const self = "github.com/getan/tuios-mini/internal/wire"

// forbidden names what the wire may never pull in: the session that owns the
// payloads, and the product around it — the TUI, the daemon, the workspace and
// the terminal emulator they are built from.
var forbidden = []string{
	"github.com/getan/tuios-mini/internal/session",
	"github.com/getan/tuios-mini/internal/app",
	"github.com/getan/tuios-mini/internal/harness",
	"github.com/getan/tuios-mini/internal/terminal",
	"github.com/getan/tuios-mini/mini",
	"github.com/getan/tuios-mini/workspace",
	"charm.land/bubbletea",
	"charm.land/lipgloss",
	"charm.land/wish",
	"github.com/charmbracelet/ultraviolet",
	"tuios.dev/vt",
	"tuios.dev/swatch",
	"tuios.dev/progstatus",
}

func TestWireLinksNoneOfTheProduct(t *testing.T) {
	deps := depsOf(t, self)
	if len(deps) == 0 {
		t.Fatal("the dependency list is empty, so this test proves nothing")
	}
	// It really is the protocol: gob, the framing's own packages, and the
	// semaphore the read budget is built on.
	for _, want := range []string{"encoding/gob", "net", "golang.org/x/sync/semaphore"} {
		if !deps[want] {
			t.Errorf("internal/wire does not depend on %s, so this test looked at the wrong graph", want)
		}
	}
	for _, bad := range forbidden {
		for dep := range deps {
			if dep == bad || strings.HasPrefix(dep, bad+"/") {
				t.Errorf("internal/wire imports %s\n\n%s", dep, importedBy(t, dep))
			}
		}
	}
}

// The rule the check above applies, stated once: the standard library, its own
// package, and the one module the read budget needs. Everything else is a
// dependency the wire has no business having.
func allowedDependency(dep string) bool {
	if dep == self || strings.HasPrefix(dep, self+" ") || strings.HasPrefix(dep, self+".test") {
		return true
	}
	if strings.HasPrefix(dep, "vendor/") {
		return true
	}
	// The standard library has no dot in its first element; a module path
	// always does.
	if !strings.Contains(strings.SplitN(dep, "/", 2)[0], ".") {
		return true
	}
	return strings.HasPrefix(dep, "golang.org/x/sync/")
}

// The other half of the claim, and the reason the check above is worth
// anything: applied to a package that is not the wire, the same rule has
// something to report. A guard that flags nothing anywhere would pass above
// and prove nothing.
func TestTheGuardWouldCatchAProductDependency(t *testing.T) {
	sessionDeps := depsOf(t, "github.com/getan/tuios-mini/internal/session")
	var flagged []string
	for dep := range sessionDeps {
		if !allowedDependency(dep) {
			flagged = append(flagged, dep)
		}
	}
	sort.Strings(flagged)
	if len(flagged) == 0 {
		t.Fatal("the guard flagged nothing in internal/session, so it cannot be trusted to flag anything")
	}
	// Named, so a change to the session package that this test reads is not
	// silent: these are the dependencies the wire is kept away from.
	for _, want := range []string{
		"github.com/getan/tuios-mini/internal/harness",
		"tuios.dev/vt",
	} {
		if !sessionDeps[want] {
			t.Fatalf("internal/session no longer imports %s: update the guard's negative control", want)
		}
		if allowedDependency(want) {
			t.Fatalf("%s is allowed, so the guard would not flag it", want)
		}
	}
	t.Logf("the same rule flags %d of internal/session's dependencies, among them %s",
		len(flagged), strings.Join(flagged[:min(3, len(flagged))], ", "))
}

// depsOf is the import graph of one package, its dependencies and theirs.
func depsOf(t *testing.T, pkg string) map[string]bool {
	t.Helper()
	out, err := exec.Command("go", "list", "-deps", pkg).Output()
	if err != nil {
		t.Fatalf("go list -deps %s: %v", pkg, err)
	}
	deps := map[string]bool{}
	for l := range strings.SplitSeq(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			deps[l] = true
		}
	}
	return deps
}

// importedBy names a package in this tree that imports bad, so a failure
// points at the edge to cut rather than at the whole graph.
func importedBy(t *testing.T, bad string) string {
	t.Helper()
	out, err := exec.Command("go", "list", "-f",
		"{{.ImportPath}} imports {{join .Imports \" \"}}",
		"github.com/getan/tuios-mini/...").Output()
	if err != nil {
		return ""
	}
	var hits []string
	for l := range strings.SplitSeq(string(out), "\n") {
		path, imports, ok := strings.Cut(l, " imports ")
		if !ok {
			continue
		}
		for imp := range strings.FieldsSeq(imports) {
			if imp == bad {
				hits = append(hits, "  "+path)
			}
		}
	}
	if len(hits) == 0 {
		return ""
	}
	return "imported by:\n" + strings.Join(hits, "\n")
}
