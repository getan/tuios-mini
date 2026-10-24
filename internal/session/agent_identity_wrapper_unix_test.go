//go:build linux || darwin

package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const agentIdentityHelperEnv = "TUIOS_TEST_IDENTITY_HELPER"
const agentIdentityWrapperCommand = `"$1" -test.run='^TestAgentIdentityProcessHelper$'; true`

// Use our own binary as the long-lived process instead of a system utility.
// Ubuntu 26's Rust coreutils can reject sleep invoked through a crush symlink:
// "Security violation: Requested utility `crush` does not match executable name".
// The helper has no argv[0] dispatch and works under any executable alias.
func TestAgentIdentityProcessHelper(t *testing.T) {
	if os.Getenv(agentIdentityHelperEnv) != "1" {
		return
	}
	time.Sleep(time.Minute)
}

// Exercise the same wrapper launch on macOS too: the transcript regression is
// Linux-only, so a passing macOS suite previously did not run its fixture at all.
// The Linux transcript test separately covers the executable alias.
func TestAgentIdentityBehindStartedWrapper(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	wantVersion := filepath.Base(executable)
	d, _ := startTestDaemonWith(t, &DaemonConfig{AgentBinaries: []string{wantVersion}})
	sess, err := d.manager.CreateSession("work", &SessionConfig{}, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	w, err := sess.AddDaemonWindowWith(NewWindowOptions{
		Title: "Wrapper", Focus: true,
		Command: []string{"/bin/sh", "-c", agentIdentityWrapperCommand, "agent-wrapper", executable},
		Env:     []string{agentIdentityHelperEnv + "=1"},
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
	if _, version := d.paneAgentIdentifier(sess)(w.PTYID); version != wantVersion {
		t.Fatalf("version = %q, want the child's executable name %q", version, wantVersion)
	}
}
