package mini_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// goEnvAtStart is the go command's own configuration, read once before the
// package's TestMain redirects HOME (internal/testutil). The module cache,
// build cache and GOPATH live under HOME by default, and with the redirect in
// place a child go command would look for an empty cache: with GOPROXY=off it
// then fails instead of resolving, which is how a test that shells out to go
// would report an import graph nobody could produce. Reading the values before
// the redirect and handing them to the child keeps the check about the graph.
var goEnvAtStart = func() map[string]string {
	out, err := exec.Command("go", "env", "-json", "GOMODCACHE", "GOCACHE", "GOPATH", "GOFLAGS").Output()
	if err != nil {
		return nil
	}
	var env map[string]string
	if err := json.Unmarshal(out, &env); err != nil {
		return nil
	}
	return env
}()

// What an embedded workspace is, as the linker sees it.
//
// The profile in internal/session decides what a local-only daemon starts, and
// that is a runtime boundary: the code behind the services it does not start is
// still compiled in, still linked, and could be reached by a verb. Saying "the
// standalone product is not in the workspace" would be false. Saying exactly what
// is there is the useful thing, so this file states both halves:
//
//   - what the workspace must never pull in, and
//   - what it still pulls in, pinned so a change is a deliberate edit here.
//
// The second list is the honest part. A reader who deletes a service from the
// profile must not conclude the code left the binary; the check below says which
// packages are still linked, and a future change that removes one has to update
// both the list and the claim in docs/M4.md.

const miniSelf = "github.com/getan/tuios-mini/mini"

// neverLinked is the TUI, the browser host and the CLIs. An embedded workspace
// is driven over the control socket, so linking a terminal UI or a command-line
// entry point would mean the embedding boundary had been crossed by an import.
var neverLinked = []string{
	"github.com/getan/tuios-mini/internal/app",
	"github.com/getan/tuios-mini/workspace",
	"github.com/getan/tuios-mini/cmd/tuios",
	"github.com/getan/tuios-mini/cmd/tuios-web",
	"github.com/getan/tuios-mini/cmd/tuios-mini",
	"github.com/getan/tuios-mini/e2e/tui",
	"charm.land/bubbletea",
	"charm.land/wish",
	"charm.land/ssh",
}

// stillLinked is the standalone product's code that the daemon core imports. A
// local-only profile does not start the federation supervisors, the plugin host,
// the hook runner, the push notifier, the worktree and review helpers or the
// herdr protocol socket, but their packages are still in the binary. The list is
// pinned so nobody reads the profile as a source deletion.
var stillLinked = []string{
	"github.com/getan/tuios-mini/internal/agentproto",
	"github.com/getan/tuios-mini/internal/federation",
	"github.com/getan/tuios-mini/internal/ghpr",
	"github.com/getan/tuios-mini/internal/herdrcli",
	"github.com/getan/tuios-mini/internal/herdrplugin",
	"github.com/getan/tuios-mini/internal/hooks",
	"github.com/getan/tuios-mini/internal/integration",
	"github.com/getan/tuios-mini/internal/pushnotify",
	"github.com/getan/tuios-mini/internal/review",
	"github.com/getan/tuios-mini/internal/risk",
	"github.com/getan/tuios-mini/internal/shimlink",
	"github.com/getan/tuios-mini/internal/tape",
	"github.com/getan/tuios-mini/internal/transcript",
	"github.com/getan/tuios-mini/internal/transcriptview",
	"github.com/getan/tuios-mini/internal/worktree",
	// The terminal libraries the daemon's own rendering-adjacent code pulls in.
	"charm.land/lipgloss",
	"github.com/charmbracelet/ultraviolet",
	"tuios.dev/vt",
}

// TestMiniLinksTheDaemonAndNotTheTUI is the boundary the embedding depends on.
func TestMiniLinksTheDaemonAndNotTheTUI(t *testing.T) {
	deps := depsOf(t, miniSelf)
	if len(deps) == 0 {
		t.Fatal("the dependency list is empty, so this test proves nothing")
	}
	// It really is the embedded daemon: the session package owns the PTYs and
	// the control client is how a host drives it.
	for _, want := range []string{
		"github.com/getan/tuios-mini/internal/session",
		"github.com/getan/tuios-mini/mini/control",
		"github.com/getan/tuios-mini/internal/wire",
		"github.com/getan/tuios-mini/internal/config",
		"github.com/getan/tuios-mini/internal/guestenv",
	} {
		if !deps[want] {
			t.Errorf("mini does not depend on %s, so this test looked at the wrong graph", want)
		}
	}
	for _, bad := range neverLinked {
		for dep := range deps {
			if dep == bad || strings.HasPrefix(dep, bad+"/") {
				t.Errorf("mini links %s, which the embedded workspace must not carry", dep)
			}
		}
	}
}

// TestMiniStillLinksTheProductItDoesNotRun pins the other half of the truth: the
// services the local-only profile does not start are compiled in. This is a
// guard in both directions — deleting one of these packages is a real change
// that has to update the list and the documentation, and a reader must not take
// the profile for source removal.
func TestMiniStillLinksTheProductItDoesNotRun(t *testing.T) {
	deps := depsOf(t, miniSelf)
	var missing []string
	for _, want := range stillLinked {
		if !hasDep(deps, want) {
			missing = append(missing, want)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("the local-only profile does not start these, but their packages are no longer linked either: %s\n\n"+
			"If a package really left the import graph, update stillLinked and docs/M4.md together. "+
			"If it did not, this test is telling you the boundary changed.",
			strings.Join(missing, ", "))
	}
}

// TestTheBoundaryGuardWouldCatchTheTUI is the negative control, and the reason
// the check above is worth anything: the same rule applied to a command that
// really does carry the TUI has to flag it. A guard that flags nothing anywhere
// would pass above and prove nothing.
func TestTheBoundaryGuardWouldCatchTheTUI(t *testing.T) {
	deps := depsOf(t, "github.com/getan/tuios-mini/cmd/tuios")
	var flagged []string
	for _, bad := range neverLinked {
		if hasDep(deps, bad) {
			flagged = append(flagged, bad)
		}
	}
	for _, want := range []string{
		"github.com/getan/tuios-mini/internal/app",
		"charm.land/bubbletea",
	} {
		if !hasDep(deps, want) {
			t.Fatalf("cmd/tuios no longer links %s, so this control cannot show the guard flags anything", want)
		}
	}
	if len(flagged) < 2 {
		t.Fatalf("the neverLinked rule flagged only %v in cmd/tuios, which links the TUI", flagged)
	}
	t.Logf("the same rule flags %d packages in cmd/tuios, among them %s", len(flagged), strings.Join(flagged[:2], ", "))
}

// hasDep reports whether pkg is in deps, either exactly or as a subpackage: a
// module with a major version in its path (charm.land/lipgloss/v2) is imported
// under the versioned path.
func hasDep(deps map[string]bool, pkg string) bool {
	if deps[pkg] {
		return true
	}
	for dep := range deps {
		if strings.HasPrefix(dep, pkg+"/") {
			return true
		}
	}
	return false
}

// depsOf is the import graph of one package, its dependencies and theirs.
func depsOf(t *testing.T, pkg string) map[string]bool {
	t.Helper()
	cmd := exec.Command("go", "list", "-deps", pkg)
	if goEnvAtStart != nil {
		env := os.Environ()
		for name, value := range goEnvAtStart {
			if value != "" {
				env = append(env, name+"="+value)
			}
		}
		cmd.Env = env
	}
	out, err := cmd.Output()
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
