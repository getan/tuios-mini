//go:build linux

package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// explainUntil polls explain-agent-detect until ready accepts the answer or the
// deadline passes, then returns the last answer for the assertions.
func explainUntil(t *testing.T, c *verbConn, winID string, ready func(map[string]any) bool) map[string]any {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var res map[string]any
	for time.Now().Before(deadline) {
		res = result(t, c.call(t, `{"id":1,"verb":"explain-agent-detect","params":{"session":"work","window":"`+winID+`"}}`))
		if ready(res) {
			return res
		}
		time.Sleep(100 * time.Millisecond)
	}
	return res
}

// TestTranscriptIdentityReadsTheAgentBehindAWrapper pins that the transcript
// join checks a candidate file against the agent's own executable, not the
// shell that launched it: a wrapper's build is "bash", and no transcript was
// ever written by bash.
func TestTranscriptIdentityReadsTheAgentBehindAWrapper(t *testing.T) {
	if _, err := os.Stat("/proc/self/task"); err != nil {
		t.Skip("no procfs")
	}
	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(binDir, "crush")
	if err := os.Symlink("/usr/bin/sleep", fake); err != nil {
		t.Fatal(err)
	}
	d, sp := startTestDaemon(t)
	sess, err := d.manager.CreateSession("work", &SessionConfig{}, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	// Start the wrapper as argv, not type-ahead into a shell that has not
	// reached its prompt. A successful send-text only acknowledges a PTY write;
	// the shell can discard queued input while initializing its line editor.
	// That left CI inspecting an idle /bin/sh rather than an agent behind sh.
	// Positional arguments also keep the temporary path out of shell syntax.
	w, err := sess.AddDaemonWindowWith(NewWindowOptions{
		Title: "Wrapper", Focus: true,
		Command: []string{"/bin/sh", "-c", `"$1" 60; true`, "agent-wrapper", fake},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := dialVerb(t, sp)
	res := explainUntil(t, c, w.ID, func(res map[string]any) bool { return res["matched"] == true })
	if res["matched"] != true {
		t.Fatalf("the pane was not detected as crush behind a shell: diagnostic=%v screen=%q",
			res, capturePane(t, c, "work", w.ID))
	}
	if via, _ := res["matched_via"].([]any); len(via) == 0 {
		t.Fatalf("the fixture did not exercise detection behind a wrapper: %v", res)
	}

	_, version := d.paneAgentIdentifier(sess)(w.PTYID)
	if version != "sleep" {
		t.Fatalf("version = %q, want the agent's own executable name (sleep), not the wrapper's", version)
	}
}
