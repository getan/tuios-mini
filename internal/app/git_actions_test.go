package app

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/getan/tuios-mini/internal/config"
	"github.com/getan/tuios-mini/internal/gitstatus"
)

func gitActionsOS(t *testing.T) (*OS, gitStatusOrigin) {
	t.Helper()
	m := NewOS(OSOptions{UserConfig: config.DefaultConfig()})
	m.SessionName = "work"
	origin := gitStatusOrigin{Session: "work", Window: "w1", Dir: "/repo"}
	m.gitStatus.Origin = origin
	m.gitStatus.Status.Root = "/repo"
	m.projectTab = 1
	m.SidebarFocused = true
	m.SidebarNav = []sidebarNavRow{{Kind: sidebarRowGitEntry, WindowIndex: 0}}
	m.SidebarCursor = 0
	m.gitStatus.entries = []gitStatusEntry{{
		Entry:  gitstatus.Entry{Path: "a.go", Letter: gitstatus.Modified},
		Origin: origin,
		Root:   "/repo",
		Index:  "unstaged",
	}}
	return m, origin
}

func TestSidebarGitStageSendsTheCursorPath(t *testing.T) {
	m, origin := gitActionsOS(t)
	var params map[string]any
	m.SetInboxVerbCaller(func(verb string, p map[string]any, _ time.Duration) (json.RawMessage, error) {
		if verb != "review-stage" {
			t.Fatalf("verb = %s", verb)
		}
		params = p
		return json.RawMessage(`{}`), nil
	}, func() string { return "nonce" })

	msg := m.SidebarGitStage(false)().(gitActionMsg)
	if msg.Err != "" {
		t.Fatal(msg.Err)
	}
	if params["action"] != "stage" || params["path"] != "a.go" {
		t.Fatalf("params = %v", params)
	}
	if params["session"] != origin.Session || params["window"] != origin.Window {
		t.Fatalf("target = %v", params)
	}
	if params["human_nonce"] != "nonce" {
		t.Fatalf("nonce = %v", params["human_nonce"])
	}
}

func TestSidebarGitCommitPromptSendsTheStagedCommit(t *testing.T) {
	m, origin := gitActionsOS(t)
	var params map[string]any
	m.SetInboxVerbCaller(func(verb string, p map[string]any, _ time.Duration) (json.RawMessage, error) {
		if verb != "review-commit" {
			t.Fatalf("verb = %s", verb)
		}
		params = p
		return json.RawMessage(`{"commit":"abcdef123456"}`), nil
	}, func() string { return "nonce" })

	if !m.SidebarGitCommitPrompt() {
		t.Fatal("the commit prompt did not open")
	}
	if m.filePrompt.Kind != filePromptCommit {
		t.Fatalf("prompt kind = %v", m.filePrompt.Kind)
	}
	m.FilePromptType("fix the retry")
	cmd := m.FilePromptSubmit()
	if cmd == nil {
		t.Fatal("an empty commit command")
	}
	msg := cmd().(gitActionMsg)
	if msg.Err != "" || msg.OK != "Committed abcdef12." {
		t.Fatalf("commit = %+v", msg)
	}
	if params["message"] != "fix the retry" || params["session"] != origin.Session {
		t.Fatalf("params = %v", params)
	}
}

func TestSidebarGitCommitRefusesAnEmptyMessage(t *testing.T) {
	m, _ := gitActionsOS(t)
	m.SidebarGitCommitPrompt()
	if cmd := m.FilePromptSubmit(); cmd != nil {
		t.Fatal("an empty message started a commit")
	}
	if m.filePrompt.Err == "" {
		t.Fatal("the empty message was not refused in the dialog")
	}
}
