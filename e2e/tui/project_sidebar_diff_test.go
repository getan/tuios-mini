package tuie2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Gaurav-Gosain/tuitest"
	"github.com/getan/tuios-mini/internal/testutil"
)

// The actual sidebar -> existing review path, on a real daemon and a real Git
// index. The fixture has a different marker in HEAD, index and working files;
// matching only the filename would let both rows show the same wrong diff.
// Comments are typed through the existing review editor, reloaded, edited and
// resolved. Artifacts keep the staged/unstaged frames and the comment frame.
func TestProjectSidebarReusesReviewDiffAndComments(t *testing.T) {
	base, repo := t.TempDir(), testutil.GitRepo(t)
	file := filepath.Join(repo, "both.txt")
	write := func(path, text string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(file, "base-only\n")
	write(filepath.Join(repo, "other.txt"), "other-base\n")
	testutil.Git(t, repo, "add", ".")
	testutil.Git(t, repo, "commit", "-qm", "base")
	write(file, "staged-only\n")
	testutil.Git(t, repo, "add", "both.txt")
	write(file, "working-only\n")
	write(filepath.Join(repo, "other.txt"), "unrelated-change\n")
	status := testutil.Git(t, repo, "status", "--porcelain")
	if out, err := tuiosCLIIn(t, base, repo, "new", "project", "--detach"); err != nil {
		t.Fatalf("new project: %v\n%s", err, out)
	}
	// Attach directly: this test does not depend on the fork's splash text or
	// the legacy mode-switch notifications the older harness waits for.
	term := startIn(t, base, startOpts{cols: 150, rows: 42, args: []string{"attach", "project"}})
	if err := term.WaitFor(func(s tuitest.Screen) bool { return countWindows(s) == 1 }, bootTimeout); err != nil {
		t.Fatalf("attach: %v\n%s", err, term.Snapshot())
	}
	// Use the shipped prefix binding; ctrl+p by itself belongs to the pane.
	sendKeys(t, term, tuitest.Ctrl('b'), "b")
	waitForAll(t, term, uiTimeout, "project files", "both.txt")
	x, y, ok := findOnGrid(term.Screen(), "git")
	if !ok {
		t.Fatalf("Git tab missing:\n%s", term.Snapshot())
	}
	mouseClick(t, term, x, y, tuitest.MouseLeft, 0)
	waitForAll(t, term, uiTimeout, "Git sides", "Staged 1", "Changes 2")
	art := artifactDir(t)
	openRow := func(which int, want, absent string) {
		t.Helper()
		var rows []int
		for row, line := range strings.Split(term.Screen().Text(), "\n") {
			if strings.Contains(line, "both.txt") {
				rows = append(rows, row)
			}
		}
		if len(rows) != 2 {
			t.Fatalf("two selectable sides missing: %v\n%s", rows, term.Snapshot())
		}
		mouseClick(t, term, 5, rows[which], tuitest.MouseLeft, 0)
		if err := term.WaitFor(func(s tuitest.Screen) bool {
			txt := s.Text()
			return strings.Contains(txt, want) && strings.Contains(txt, "staged-only") &&
				!strings.Contains(txt, absent) && !strings.Contains(txt, "other.txt")
		}, uiTimeout); err != nil {
			t.Fatalf("wrong single-file diff: %v\n%s", err, term.Snapshot())
		}
	}
	openRow(0, "base-only", "working-only")
	saveArtifact(t, term, art, "staged")
	// The first diff row is the hunk header. C attaches a whole-hunk comment.
	sendKeys(t, term, "C", "please test this hunk", tuitest.Enter)
	waitForAll(t, term, uiTimeout, "comment saved", "please test this hunk")
	sendKeys(t, term, "r")
	waitForAll(t, term, uiTimeout, "comment reloaded", "please test this hunk", "base-only")
	sendKeys(t, term, "G", "e", " updated", tuitest.Enter)
	waitForAll(t, term, uiTimeout, "comment edited", "please test this hunk updated")
	saveArtifact(t, term, art, "comment")
	// The only review note is also visible to the CLI: no second comment store.
	out, err := tuiosCLI(t, base, "review", "notes", "-s", "project", "--json")
	if err != nil || !strings.Contains(out, "please test this hunk updated") {
		t.Fatalf("comment not in the existing review store: %v\n%s", err, out)
	}
	sendKeys(t, term, "G", "x")
	if err := term.WaitFor(func(s tuitest.Screen) bool {
		return !strings.Contains(s.Text(), "please test this hunk")
	}, uiTimeout); err != nil {
		t.Fatalf("comment was not resolved: %v\n%s", err, term.Snapshot())
	}
	saveArtifact(t, term, art, "comment-resolved")
	sendKeys(t, term, tuitest.Esc)
	waitForAll(t, term, uiTimeout, "back to Git", "Staged 1", "Changes 2")
	openRow(1, "working-only", "base-only")
	saveArtifact(t, term, art, "unstaged")
	sendKeys(t, term, tuitest.Esc)
	waitForAll(t, term, uiTimeout, "review closed", "Staged 1", "Changes 2")
	if got := testutil.Git(t, repo, "status", "--porcelain"); got != status {
		t.Fatalf("reading/commenting changed the index or files: before %q, after %q", status, got)
	}
	waitWindowCount(t, term, 1, "reviewing through the sidebar")
}
