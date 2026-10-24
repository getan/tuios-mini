//go:build linux || darwin

package session

import (
	"os/exec"
	"testing"
	"time"
)

// Exercise the same wrapper launch on macOS too: the transcript regression is
// Linux-only, so a passing macOS suite previously did not run its fixture at all.
// Using the real sleep name here avoids the platforms' different treatment of
// executable symlinks; the Linux transcript test covers the crush -> sleep link.
func TestAgentIdentityBehindStartedWrapper(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	d, _ := startTestDaemonWith(t, &DaemonConfig{AgentBinaries: []string{"sleep"}})
	sess, err := d.manager.CreateSession("work", &SessionConfig{}, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	w, err := sess.AddDaemonWindowWith(NewWindowOptions{
		Title: "Wrapper", Focus: true,
		Command: []string{"/bin/sh", "-c", `"$1" 60; true`, "agent-wrapper", sleep},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	resolve := d.foregroundResolver(sess)
	deadline := time.Now().Add(10 * time.Second)
	var info foregroundInfo
	var det detection
	var matched bool
	for time.Now().Before(deadline) {
		info, _ = resolve(w.PTYID)
		det, matched = d.agentMatcher.identifyDetail(info)
		if matched {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !matched {
		t.Fatalf("no agent behind wrapper: comm=%q argv=%q exe=%q", info.comm, info.argv, info.exe)
	}
	if len(det.via) == 0 || det.proc.pid == info.pid {
		t.Fatalf("matched the wrapper rather than its child: leader=%d match=%d via=%v", info.pid, det.proc.pid, det.via)
	}
	if _, version := d.paneAgentIdentifier(sess)(w.PTYID); version != "sleep" {
		t.Fatalf("version = %q, want the child's executable name sleep", version)
	}
}
