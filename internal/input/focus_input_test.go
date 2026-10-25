package input

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/getan/tuios-mini/internal/app"
	"github.com/getan/tuios-mini/internal/config"
	"github.com/getan/tuios-mini/internal/terminal"
	"tuios.dev/vt"
)

// twoPaneWM keeps the fixture used by layout/Inbox tests. Its old mode value
// deliberately stays at zero: a mode bit must never be needed to type.
func twoPaneWM(t *testing.T) *app.OS {
	t.Helper()
	o := osWithBindings(t, func(*config.KeybindingsConfig) {})
	o.Width, o.Height = 120, 40
	o.AutoTiling = true
	ws := o.CurrentWorkspace
	o.Windows = []*terminal.Window{
		{ID: "a", Width: 60, Height: 40, Workspace: ws},
		{ID: "b", X: 60, Width: 60, Height: 40, Workspace: ws},
	}
	o.FocusedWindow = 0
	return o
}

func twoInputPanes(t *testing.T) (*app.OS, *capturePty, *capturePty) {
	t.Helper()
	o, first := osWithFocusedPane(t, config.DefaultConfig(), app.WindowManagementMode)
	o.Windows[0].Width = 50
	em := vt.NewEmulator(48, 24)
	t.Cleanup(func() { _ = em.Close() })
	second := &capturePty{}
	o.Windows = append(o.Windows, &terminal.Window{
		ID: "focus-second", X: 55, Width: 50, Height: 26,
		Workspace: o.CurrentWorkspace, Terminal: em, Pty: second,
	})
	return o, first, second
}

func TestFocusedPaneOwnsTypingWithoutAModeSwitch(t *testing.T) {
	for _, oldMode := range []app.Mode{app.WindowManagementMode, app.TerminalMode} {
		t.Run(string(rune('0'+oldMode)), func(t *testing.T) {
			o, pty := osWithFocusedPane(t, config.DefaultConfig(), oldMode)
			for _, text := range []string{"i", "n", "q", "x", "h", "j", "k", "l", "1", "?", ","} {
				HandleInput(press(text), o)
			}
			HandleInput(tea.KeyPressMsg{Code: tea.KeyEnter}, o)
			HandleInput(tea.KeyPressMsg{Code: tea.KeyTab}, o)
			HandleInput(tea.KeyPressMsg{Code: tea.KeyEscape}, o)
			HandleInput(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}, o)
			HandleInput(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}, o)
			if got, want := string(pty.got), "inqxhjkl1?,\r\t\x1b\x03\x10"; got != want {
				t.Fatalf("pane input = %q, want %q", got, want)
			}
			if len(o.Windows) != 1 || o.ShowQuitMenu || o.ShowCommandPalette || o.ShowHelp || o.ShowSettings {
				t.Fatal("ordinary pane input ran a workspace action")
			}
		})
	}
}

func TestExplicitFocusAlwaysGivesTypingToTheChosenPane(t *testing.T) {
	for _, policy := range []config.AutoEnterTerminalPolicy{config.AutoEnterTerminalOff, config.AutoEnterTerminalTargeted, config.AutoEnterTerminalAll} {
		t.Run(string(policy), func(t *testing.T) {
			o, first, second := twoInputPanes(t)
			o.Settings.AutoEnterTerminalOnFocus = policy // deprecated; deliberately ignored
			o.FocusWindow(1)
			HandleInput(press("b"), o)
			o.FocusWindow(1) // reselecting is ready too
			HandleInput(press("c"), o)
			if string(second.got) != "bc" || len(first.got) != 0 {
				t.Fatalf("wrong input targets: first=%q second=%q", first.got, second.got)
			}
		})
	}
}

func TestPrefixFocusReturnsImmediatelyToPaneInput(t *testing.T) {
	o, first, second := twoInputPanes(t)
	leader(o, press("2"))
	HandleInput(press("b"), o)
	leader(o, press("1"))
	HandleInput(press("a"), o)
	if string(first.got) != "a" || string(second.got) != "b" {
		t.Fatalf("first=%q second=%q", first.got, second.got)
	}
}

func TestPlainTabDoesNotCyclePanes(t *testing.T) {
	o, first, second := twoInputPanes(t)
	HandleInput(tea.KeyPressMsg{Code: tea.KeyTab}, o)
	if o.FocusedWindow != 0 || string(first.got) != "\t" || len(second.got) != 0 {
		t.Fatalf("Tab did not belong to its pane: focus=%d first=%q second=%q", o.FocusedWindow, first.got, second.got)
	}
}

func TestUnknownPrefixAndCancellationNeverReachTheShell(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{press("$"), {Code: tea.KeyEscape}, {Code: 'c', Mod: tea.ModCtrl}} {
		o, pty := osWithFocusedPane(t, config.DefaultConfig(), app.WindowManagementMode)
		leader(o, key)
		if len(pty.got) != 0 || o.PrefixActive {
			t.Fatalf("command %q leaked or stayed armed: %q", key.String(), pty.got)
		}
		HandleInput(press("a"), o)
		if string(pty.got) != "a" {
			t.Fatalf("normal input did not resume: %q", pty.got)
		}
	}
}

func TestPrefixExpiryDoesNotReplayAnyBufferedInput(t *testing.T) {
	o, pty := osWithFocusedPane(t, config.DefaultConfig(), app.WindowManagementMode)
	o.BeginPrefix()
	started := o.LastPrefixTime
	o.ExpirePrefix(app.PrefixExpiredMsg{Started: started.Add(-time.Second)})
	if !o.PrefixActive {
		t.Fatal("stale timer cancelled a newer command")
	}
	o.ExpirePrefix(app.PrefixExpiredMsg{Started: started})
	if o.PrefixActive || len(pty.got) != 0 {
		t.Fatal("expired prefix stayed active or replayed input")
	}
	HandleInput(press("a"), o)
	if string(pty.got) != "a" {
		t.Fatalf("typing after expiry = %q", pty.got)
	}
}

func TestDialogsOwnTypingAndPasteThenGiveThePaneBack(t *testing.T) {
	for _, open := range []func(*app.OS){
		func(o *app.OS) { o.ShowHelp = true },
		func(o *app.OS) { o.OpenCommandPalette() },
		func(o *app.OS) { o.OpenSettings() },
		func(o *app.OS) { o.BeginRenameWindow(o.GetFocusedWindow()) },
	} {
		o, pty := osWithFocusedPane(t, config.DefaultConfig(), app.WindowManagementMode)
		open(o)
		HandleInput(press("a"), o)
		HandleInput(tea.PasteMsg{Content: "not for shell"}, o)
		if len(pty.got) != 0 {
			t.Fatalf("dialog leaked into pane: %q", pty.got)
		}
		// Settings' type-to-search is itself a nested dialog: Escape first
		// closes the search, then the settings page.
		for i := 0; i < 3 && o.InputOverlayOpen(); i++ {
			HandleInput(tea.KeyPressMsg{Code: tea.KeyEscape}, o)
		}
		HandleInput(press("z"), o)
		if string(pty.got) != "z" {
			t.Fatalf("closing dialog did not restore pane input: %q", pty.got)
		}
	}
}

func TestOldHoldBindingDoesNotCreateAnInputMode(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Keybindings.ModeControl[app.HoldModeAction] = []string{"f13"}
	o, pty := osWithFocusedPane(t, cfg, app.WindowManagementMode)
	HandleInput(tea.KeyPressMsg{Code: tea.KeyF13}, o)
	pty.got = nil
	HandleInput(press("n"), o)
	if string(pty.got) != "n" || o.HoldModeActive() {
		t.Fatalf("legacy hold changed ownership: %q", pty.got)
	}
}
