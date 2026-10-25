# Focus is input

tuios-mini has one working interaction model: **focus a pane and type**.
Window management is a command, not a persistent keyboard mode. The golder
workspace uses this same model; it does not require a separate terminal-mode
toggle.

## Everyday operations

Default leader: `Ctrl+B`. The keys below are successive presses, not chords
held together. A configured leader or preset still wins.

| Operation | Default |
| --- | --- |
| New pane | leader, `c` |
| Focus a neighbouring pane | leader, arrow |
| Focus pane by number | leader, `1`–`9` |
| Rename pane | leader, `r` |
| Close pane | leader, `x` (existing confirmation rules apply) |
| Workspace/tab commands | leader, `w` |
| Command palette | leader, `P` |
| Settings / help | leader, `,` / leader, `?` |
| Detach a daemon client | leader, `d` |
| Send the leader to the program | leader twice |

Escape or Ctrl+C cancels an armed command, including a submenu. Unknown or
unavailable commands are reported, never replayed into the shell. A chord
expires after three seconds; no buffered input is sent on expiry. Explicitly
configured repeatable commands keep the existing bounded repeat behaviour.

Outside an armed command, Escape, Tab, Enter, Ctrl+C, Ctrl+P and ordinary
characters are the pane program's keys. Direct Alt-arrow, Alt+n/p and global
palette shortcuts are opt-in. Conditional scrollback and host-paste bindings
remain available; full-screen mouse-aware programs keep their scroll keys.

## Mouse

- A content click focuses the pane. A program that requested mouse tracking
  receives that same click, without an extra activation click.
- Dragging ordinary terminal content selects text; double/triple click keeps
  the existing word/line selection behaviour.
- Drag a title bar to move a floating pane or perform a tiled layout drop.
- Drag a border/divider to resize. The drag owns motion and release, never
  leaking an unmatched mouse event into a program underneath.
- Alt+left-drag (when enabled) and the existing Ctrl-drag gesture still move
  panes from content. They work when title bars are hidden too.
- Ending a frame gesture immediately gives input back to the focused pane.
- Hover does not change focus by default. Explicit focus-follows-mouse remains
  an opt-in policy.

## Temporary input owners

Dialogs, search, sidebars and explicit copy selection own their input while
visible/focused. Unhandled keys and paste do not leak into a pane behind them.
Closing a dialog leaves the underlying focus intact. Leaving the sidebar
restores the previous pane by identity, not its potentially stale list index.

On an empty tab there is no input destination. The landing page offers `n`
(new pane), `N` (new session), `?` (help) and `,` (settings), as well as leader
commands. Typing arbitrary text starts no process and queues no shell input.

A newly requested daemon pane is asynchronous. Until its focused pane arrives,
the badge says `OPENING` and text/paste intended for it is discarded rather
than sent to the old shell. An explicit focus on an existing pane cancels that
wait. A ten-second timeout clears the stale focus and explains how to recover.

Key releases follow the pane that received the press even when focus changes
or a dialog opens in between. A pane receives no release for a host command
or dialog key it never received.

## Existing configurations and tapes

- `appearance.click_to_type`, `appearance.auto_enter_terminal_on_focus` and
  `startup.start_in_terminal_mode` remain parseable but have no effect. Their
  controls are removed from Settings.
- Enter/exit/hold-window-mode key bindings are retired. Loading a config that
  specifies them reports a migration warning.
- Old window-management/layout command maps remain readable for compatibility
  and diagnostics, not as bare keys intercepted while typing. Reports mark
  those definitions inactive; the live UI advertises leader/pane shortcuts.
- On first migration, the former default Alt-arrow/n/p and Ctrl+P/Alt+Space
  bindings are released. Different custom direct chords are preserved.
- `[keybindings] input_version = 1` records that migration. Once present,
  deliberately rebinding an old default key is respected on future loads.

For example, an explicit opt-in direct shortcut:

```toml
[keybindings]
input_version = 1

[keybindings.terminal_mode]
terminal_focus_left = ["alt+left"]
terminal_focus_right = ["alt+right"]
```

`OS.Mode`, old tape mode commands and their source aliases are transitional
compatibility values only. They cannot turn off typing in a focused pane.
This change does not alter daemon permissions, pane proofs or session scope.
