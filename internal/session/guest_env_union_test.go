package session

import "testing"

// A control connection has no terminal of its own. Attaching one after the TUI
// client used to replace the session's graphics capability with false, so a
// browser or chafa started from an agent pane advertised TUIOS and fell back to
// block art even though the person's terminal draws kitty images.
func TestControlAttachDoesNotEraseTheDisplayingClientsGraphics(t *testing.T) {
	d, _ := startTestDaemon(t)
	sess, err := d.manager.CreateSession("graphics-union", &SessionConfig{}, 80, 24)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	display := NewTUIClient()
	if err := display.ConnectWithCapabilities("test", 80, 24, &ClientCapabilities{KittyGraphics: true, TerminalName: "ghostty"}); err != nil {
		t.Fatalf("connect display client: %v", err)
	}
	t.Cleanup(func() { _ = display.Close() })
	if _, err := display.AttachSession("graphics-union", false, 80, 24); err != nil {
		t.Fatalf("attach display client: %v", err)
	}

	control := NewTUIClient()
	if err := control.ConnectWithCapabilities("test", 80, 24, nil); err != nil {
		t.Fatalf("connect control client: %v", err)
	}
	t.Cleanup(func() { _ = control.Close() })
	if _, err := control.AttachSession("graphics-union", false, 80, 24); err != nil {
		t.Fatalf("attach control client: %v", err)
	}

	if kitty, _ := sess.GraphicsCapabilities(); !kitty {
		t.Fatal("a control attach erased the displaying client's kitty capability")
	}
	if got := envValue(sess.buildEnv("", false), "TERM_PROGRAM"); got != "ghostty" {
		t.Fatalf("TERM_PROGRAM = %q after a control attach, want ghostty", got)
	}
}

// Two drawing clients are a union: one without kitty must not take it away from
// the other, because the session still has a client that can display it.
func TestGraphicsCapabilitiesAreTheUnionOfDrawingClients(t *testing.T) {
	d, _ := startTestDaemon(t)
	sess, err := d.manager.CreateSession("graphics-both", &SessionConfig{}, 80, 24)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	for _, caps := range []*ClientCapabilities{
		{KittyGraphics: true, TerminalName: "ghostty"},
		{TerminalName: "xterm"},
	} {
		c := NewTUIClient()
		if err := c.ConnectWithCapabilities("test", 80, 24, caps); err != nil {
			t.Fatalf("connect: %v", err)
		}
		t.Cleanup(func() { _ = c.Close() })
		if _, err := c.AttachSession("graphics-both", false, 80, 24); err != nil {
			t.Fatalf("attach: %v", err)
		}
	}
	if kitty, _ := sess.GraphicsCapabilities(); !kitty {
		t.Fatal("the second, plain client erased the first client's kitty capability")
	}
}
