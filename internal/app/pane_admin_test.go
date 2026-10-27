package app

import (
	"encoding/json"
	"errors"
	"slices"
	"syscall"
	"testing"
	"time"

	"github.com/getan/tuios-mini/internal/config"
	"github.com/getan/tuios-mini/internal/terminal"
)

// TestToggleFocusedPaneAdmin: the client starts from the pane's explicit
// grants when it has them, adds admin, and on the next press removes only
// admin, leaving files or respond alone.
func TestToggleFocusedPaneAdmin(t *testing.T) {
	m := NewOS(OSOptions{UserConfig: config.DefaultConfig()})
	m.SessionName = "work"
	m.CurrentWorkspace = 1
	m.Windows = []*terminal.Window{{ID: "w1", Workspace: 1}}
	m.FocusedWindow = 0

	var sent []map[string]any
	explicit := []string{"read", "write", "fan"}
	m.SetInboxVerbCaller(func(verb string, params map[string]any, _ time.Duration) (json.RawMessage, error) {
		switch verb {
		case "pane-grants":
			return json.RawMessage(`{"pane":false,"default_grants":["read","write","fan"]}`), nil
		case "list-windows":
			return json.RawMessage(`{"windows":[{"window_id":"w1","grants":` + mustJSON(t, explicit) + `}]}`), nil
		case "set-pane-grants":
			sent = append(sent, params)
			explicit = params["grants"].([]string)
			return json.RawMessage(`{}`), nil
		}
		t.Fatalf("unexpected verb %s", verb)
		return nil, nil
	}, func() string { return "" })

	msg := m.ToggleFocusedPaneAdmin()().(PaneAdminMsg)
	if msg.Err != nil || !msg.On {
		t.Fatalf("first toggle = %+v", msg)
	}
	if got := sent[0]["grants"]; !slices.Equal(got.([]string), []string{"read", "write", "fan", "admin"}) {
		t.Fatalf("grants on = %v", got)
	}

	msg = m.ToggleFocusedPaneAdmin()().(PaneAdminMsg)
	if msg.Err != nil || msg.On {
		t.Fatalf("second toggle = %+v", msg)
	}
	if got := sent[1]["grants"]; !slices.Equal(got.([]string), []string{"read", "write", "fan"}) {
		t.Fatalf("grants off = %v", got)
	}
	if sent[1]["window"] != "w1" || sent[1]["session"] != "work" {
		t.Fatalf("target = %v", sent[1])
	}
}

func TestPaneAdminReadFailureChangesNoGrants(t *testing.T) {
	for _, failure := range []string{"pane-grants", "list-windows"} {
		t.Run(failure, func(t *testing.T) {
			m := NewOS(OSOptions{UserConfig: config.DefaultConfig()})
			m.SessionName, m.CurrentWorkspace = "work", 1
			m.Windows = []*terminal.Window{{ID: "w1", Workspace: 1}}
			m.FocusedWindow = 0
			m.SetInboxVerbCaller(func(verb string, _ map[string]any, _ time.Duration) (json.RawMessage, error) {
				if verb == failure {
					return nil, syscall.EMFILE
				}
				if verb == "pane-grants" {
					return json.RawMessage(`{"default_grants":["read"]}`), nil
				}
				t.Fatalf("read failure must not call %s or guess grants", verb)
				return nil, nil
			}, nil)
			msg := m.ToggleFocusedPaneAdmin()().(PaneAdminMsg)
			if !errors.Is(msg.Err, syscall.EMFILE) {
				t.Fatalf("error = %v", msg.Err)
			}
		})
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
