package app

import "testing"

// A real TUI can keep showing "reading changes..." even after the model has
// received status, if the rail's memo key omits the Git tab/read state. This
// deterministic cache regression complements the screen-level E2E.
func TestProjectTabAndStatusInvalidateTheRailMemo(t *testing.T) {
	m, _ := reviewOS(t)
	before := m.sidebarSignature()
	m.projectTab = 1
	if m.sidebarSignature() == before {
		t.Fatal("switching project tabs keeps the cached rail")
	}
	m.gitStatus.gen, m.gitStatus.inFlight = 1, true
	before = m.sidebarSignature()
	m.ApplyGitStatus(GitStatusMsg{Gen: 1})
	if m.sidebarSignature() == before {
		t.Fatal("a finished Git status read leaves the loading frame cached")
	}
}
