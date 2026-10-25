<div align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="./assets/brand/banner-dark.png">
    <source media="(prefers-color-scheme: light)" srcset="./assets/brand/banner-light.png">
    <img alt="tuios: a terminal window manager that knows what your agents are doing. Tilly, a small purple CRT whose screen is a tiled layout, stands beside the tuios wordmark and a tiled terminal session." src="./assets/brand/banner-light.png" width="100%">
  </picture>

  <p><strong>tuios-mini: trimmed TUIOS core for golder</strong></p>

  <a href="https://pkg.go.dev/github.com/getan/tuios-mini"><img src="https://pkg.go.dev/badge/github.com/getan/tuios-mini.svg" alt="Go Reference"></a>
  <a href="https://github.com/getan/golder"><img src="https://img.shields.io/badge/used%20by-golder-blue" alt="Used by golder"></a>
</div>

<picture>
  <source srcset="./assets/demo.avif" type="image/avif">
  <img alt="TUIOS" src="./assets/demo.gif">
</picture>

> **Independent fork.** `getan/tuios-mini` began as a trimmed derivative of
> [TUIOS](https://github.com/Gaurav-Gosain/tuios) and is maintained separately
> for [golder](https://github.com/getan/golder). It has since diverged from
> upstream: behavior, APIs, documentation, CI and distribution policy here are
> not guaranteed to match TUIOS. Upstream releases, package-manager installs,
> support channels and issue tracker do not apply to this repository. This fork
> is not affiliated with or endorsed by the upstream project.

`tuios-mini` keeps the parts of TUIOS that golder needs: terminal panes, the PTY
and daemon plumbing, the JSON control surface, and agent-state reporting. It is
a Go module consumed by golder and pinned to a commit; it is not a replacement
distribution of the full TUIOS application.

TUIOS itself is a modern terminal multiplexer and window manager built with Go,
with a vim-like modal interface, workspaces, BSP tiling, kitty graphics protocol
support and a command palette. The remaining feature and design documentation
below is inherited from upstream and may describe code this fork has removed.

## Documentation

Upstream documentation is available at **[tuios.dev](https://tuios.dev)**.
Some of it is also present in the [`docs/`](./docs/) folder, inherited from
upstream and not maintained as a description of this fork. Treat the code and
the tests in this repository as authoritative.

### Inherited upstream quick links
- **[Getting Started](https://tuios.dev/docs/getting-started)**: Install and first session
- **[Keybindings](docs/KEYBINDINGS.md)**: Default keys and how to rebind them
- **[BSP Tiling](docs/BSP_TILING.md)**: Tiling with preselection and split control
- **[Layout Modes](docs/LAYOUT_MODES.md)**: BSP, master-stack and scrolling layouts, aggregate view, multifocus
- **[Configuration](docs/CONFIGURATION.md)**: Customize keybindings, themes, and behavior
- **[Hooks](docs/HOOKS.md)**: Run shell commands on window, session and agent events
- **[Themes](docs/THEMES.md)**: Built-in themes and custom theme JSON
- **[Glyph sets](docs/GLYPHS.md)**: The characters the chrome is drawn with
- **[CLI Reference](docs/CLI_REFERENCE.md)**: All command-line options
- **[Tape Scripting](docs/TAPE_SCRIPTING.md)**: Automate workflows
- **[Sessions](docs/SESSIONS.md)**: Daemon mode, attach/detach, other machines, and what survives
- **[Agents](docs/AGENT_STATE.md)**: Running coding agents in tuios: state, the Inbox, approvals, fleets, other machines, grants and MCP
- **[Program Status](docs/PROGRAM_STATUS.md)**: OSC 7501 from any program, `tuios status`, and the reports tuios sends to its own terminal
- **[tmux Shim](docs/TMUX_SHIM.md)**: Run tools that drive tmux, such as Claude Code agent teams
- **[Control Protocol](docs/protocol.md)**: JSON verb protocol for driving the daemon
- **[Architecture](docs/ARCHITECTURE.md)**: Technical design

<details>
<summary>Table of Contents</summary>

<!--toc:start-->
- [Using this repository](#using-this-repository)
- [Features](#features)
- [Quick Start](#quick-start)
- [Architecture](#architecture)
- [Performance](#performance)
- [Development](#development)
- [License](#license)
<!--toc:end-->

</details>

## Using this repository

This fork is consumed by golder as a Go module pinned to a commit. It does not
publish its own binaries, containers, Homebrew/AUR formula, or Nix package.
Use upstream TUIOS if you want the complete, supported application and its
installation instructions.

Building this repository from source needs Go 1.26.6 or newer:

```bash
git clone https://github.com/getan/tuios-mini.git
cd tuios-mini
go build ./...
go test ./...
```

The upstream compatibility entry points remain in the tree, but they are not a
promise that this fork tracks upstream behavior. Downstream users should pin an
exact commit or pseudo-version rather than follow a moving branch.

## Features

![TUIOS](./assets/tuios.gif)

<table>
  <tr>
    <td width="50%"><a href="https://tuios.dev/docs/layout-modes"><img src="https://tuios.dev/shots/overview-800.webp" width="400" alt="tuios with four tiled panes: a git diff, a Go file, test output and a build. The sidebar lists the session, its files and four agents with their states."></a><br><b>Panes and the rail.</b> Tiled panes, and every agent's state at a glance.</td>
    <td width="50%"><a href="https://tuios.dev/docs/agent-inbox"><img src="https://tuios.dev/shots/inbox-800.webp" width="400" alt="The tuios Inbox listing an approval marked risky, a question and an errored agent, with the reason the approval is risky."></a><br><b>The Inbox.</b> Everything your agents wait on, in one list.</td>
  </tr>
  <tr>
    <td width="50%"><a href="https://tuios.dev/docs/agent-inbox#reviewing-an-agents-changes"><img src="https://tuios.dev/shots/review-800.webp" width="400" alt="The tuios review view: a file list on the left and a highlighted diff on the right, with keys for notes, hunks and files."></a><br><b>Review.</b> Read an agent's changes and send notes back to it.</td>
    <td width="50%"><a href="https://tuios.dev/docs/configuration"><img src="https://tuios.dev/shots/settings-800.webp" width="400" alt="The tuios settings page searched for theme. The Theme row carries a dot because it differs from the default."></a><br><b>Settings.</b> Every option on one page, applied at once.</td>
  </tr>
</table>

Each picture is a capture that tuios made of itself. Click one to read its docs page.

### Core
- **Multiple Terminal Panes**: Create, resize, drag, and organize terminal sessions
- **9 Workspaces**: Independent workspace isolation with instant switching
- **Modal Interface**: Vim-inspired Window Management and Terminal modes
- **Command Palette**: Fuzzy-searchable action launcher (<kbd>Ctrl</kbd>+<kbd>P</kbd>)
- **Launcher**: Fuzzy search everything on `$PATH` plus your installed desktop apps (<kbd>Alt</kbd>+<kbd>Space</kbd>), ranked by what you actually run. <kbd>Enter</kbd> starts it; <kbd>Tab</kbd> opens a shell with the command typed but not entered, so you can add arguments. App icons are drawn where the terminal supports kitty graphics.
- **Pane Zoom**: Zoom any pane with <kbd>z</kbd> (WM mode) or <kbd>Prefix</kbd>+<kbd>z</kbd>. It takes 95% of the screen by default; set `appearance.zoom_size = 100` for fullscreen. A fullscreen zoom hides the shared borders, and the dockbar shows a **Z** indicator.
- **Session Rail**: A sidebar with sessions, terminals, files, git state and agents, on by default on the right (`appearance.sidebar.enabled`)
- **Settings Page**: Change options in the app with <kbd>Prefix</kbd>+<kbd>,</kbd>
- **Popups**: `tuios popup -- fzf` runs a command in a floating pane that closes when it exits

### Agents
The guide is [docs/AGENT_STATE.md](docs/AGENT_STATE.md).
- **Agent State**: Panes running a coding agent show whether it is working, waiting for you, done or errored, as a shape in the title and a row on the rail. `tuios integration install` wires 19 harnesses (Claude Code, Codex, Gemini CLI, opencode and more) to report it, and tuios detects 24 agent CLIs by their process and screen
- **Program Status (OSC 7501)**: Any program can say what it is doing with the Program Status Protocol, or with `tuios status working --app build --progress 40`. A build, a deploy script or `brew` then shows on the rail and in the Inbox like an agent. tuios also reports its panes to a terminal that reads the protocol, such as Rex ([docs](docs/PROGRAM_STATUS.md))
- **Inbox**: <kbd>Prefix</kbd>+<kbd>i</kbd> lists everything waiting for you in every session and on every machine: approvals, questions, mail, errors, finished turns. <kbd>Prefix</kbd>+<kbd>o</kbd> jumps to the oldest. Answer a prompt from there without going to the pane, and with `[agents.approvals]` answer Claude Code, opencode, Kilo and Qwen Code permission requests with one key
- **Questions and Messages**: `tuios ask-human` puts a question with fixed answers in your Inbox. Agents mail each other with `tuios send-agent-message`, and `tuios ask-agent` asks one and waits for its answer. It never types into a pane waiting on a prompt, and replies from you are marked verified
- **Fleets**: `tuios fan` starts one prompt in several agents, mixed harnesses allowed, each in its own git worktree. `tuios start-agent` starts one helper beside you, in its TUI or headless over ACP or the Codex app-server. Selectors such as `group:fan/retry needs:you` address a whole group
- **Resume**: After a daemon restart, the Inbox offers to resume each agent conversation that was running
- **Pane Grants**: Say what an agent's pane may do through tuios (`read`, `write`, `fan`, `respond`, `admin`), and give a helper less with `--grants`
- **MCP Server**: `tuios mcp` serves the same surface as MCP tools, read-only and held to the agent's own session unless you say otherwise
- **tmux Shim**: `tuios tmux-shim` runs tools that drive tmux, such as Claude Code agent teams, with their panes opened as tuios panes
- **Session Stash**: `tuios stash put` keeps a file for the session, so another agent can still open it
- **Agent Skill**: `tuios --skill` prints the short guide an agent in a pane reads to drive tuios, and `tuios --skill TOPIC` the rest, recipes included

### Machines
- **Hosts**: `tuios hosts add` names another machine, reached over ssh. `tuios hosts tailnet` lists the machines on a Tailscale tailnet
- **Remote Sessions**: `tuios attach --host build api` draws a session on another machine in this client, and `-s HOST:SESSION` sends any command there
- **Hosted Panes**: `tuios new-window NAME --host build` runs one pane's process on another machine, in a session here
- **Global Sessions**: `tuios new NAME --global` holds panes from several machines ([docs](docs/SESSIONS.md))
- **Agents on Other Machines**: `tuios fan --host build` and `tuios start-agent -s build:api` run agents there, their Inbox items show here, and `tuios worktree pull` brings their work back. Each machine's `[hosts]` policy says what the others may do

### Tiling
- **BSP Tiling**: Binary Space Partitioning with spiral layout
- **Scrolling Layout**: niri-style columns on an infinite horizontal strip ([docs](docs/LAYOUT_MODES.md))
- **Master-Stack Layout**: One master pane with the rest stacked beside it
- **Smart Auto-Split**: Aspect-ratio-aware splitting (opt-in)
- **Shared Borders**: tmux-style separator lines between panes (`--shared-borders`)
- **Preselection**: Control where the next pane spawns
- **Equalize Splits**: Reset all splits to balanced ratios

### Scrollback & Copy Mode
- **Vim-Style Copy Mode**: Navigate 10,000-line scrollback with hjkl, search down with `/` and up with `?`, yank with `y`
- **Multi Copy Mode**: Copy mode on every pane of the multifocus set at once. Search once, select with `v`/`V` in each pane, and yank all selections as plain text, markdown or JSON, to the clipboard or a file ([docs](docs/LAYOUT_MODES.md#multi-copy-mode))
- **Mouse Wheel Scrollback**: The wheel scrolls history with no mode entered; typing or reaching the bottom returns to live output
- **Interactive Scrollbar**: Click or drag the right border to jump to scroll position
- **Selection Auto-Scroll**: Drag selection above/below pane to scroll
- **Scrollback Browser**: OSC 133-aware command/output block navigation
- **Scroll Position Indicator**: Shows offset/total on the bottom border

### Graphics & Protocols
- **Kitty Graphics Protocol**: Full image rendering with flicker-free video playback. `mpv --vo=kitty` works (both shm and base64), and [youterm](https://github.com/Gaurav-Gosain/youterm) works.
- **Sixel Graphics**: Sixel image passthrough (experimental, no pixel-level clipping yet)
- **Kitty Keyboard Protocol**: Progressive enhancement (CSI u) with push/pop/query support. Fish 4.x compatible; Shift+printable bypasses the protocol and sends text directly.
- **Synchronized Output**: Mode 2026 prevents screen tearing
- **Shared Memory Support**: `t=s` passthrough for mpv `--vo-kitty-use-shm`
- **Animation Frames**: A guest's `a=f` frame edits are forwarded to the host, so a program that patches its own image costs a rectangle instead of a whole bitmap. TUIOS also patches guests that only retransmit. Panes are told whether the host carries frame edits through `TUIOS_KITTY_ANIMATION`, because the host's reply is not relayed back into the pane and a guest cannot find out for itself.
- **Terminal Queries**: OSC 4 palette, OSC 10-12 colors, CSI 14/16/18t sizing, DA1/DA2
- **Experimental**: Kitty text sizing protocol (OSC 66). Basic passthrough works but has known issues with scrollback and window repositioning
- **Kitty Animation Protocol**: Frame transmission, composition, and control (a=f, a=a, a=c), with damage-patch streaming for animated guests

### Session Management
- **Daemon Mode**: Persistent sessions with detach/reattach (like tmux)
- **Session Resurrection**: Sessions come back after a daemon restart or reboot with their structure and working directories ([docs](docs/SESSIONS.md))
- **Session Switcher**: In-app session list (<kbd>Prefix</kbd>+<kbd>S</kbd>)
- **Layout Templates**: Save/load window arrangements with working directories and startup commands
- **Layout CLI**: `tuios layout list`, `tuios layout delete`, `tuios layout export`

### Automation
- **Tape Scripting**: DSL for recording and replaying terminal workflows
- **Tape Recording**: Record live sessions (<kbd>Prefix</kbd>+<kbd>T</kbd> <kbd>r</kbd>)
- **Headless Execution**: `tuios tape exec` runs a tape against a running daemon session
- **Layout Export**: Convert layouts to tape scripts for sharing

### Discovery & Navigation
- **Which-Key Popup**: Hold the prefix key to see the chords available (`appearance.whichkey_enabled`, [docs](docs/KEYBINDINGS.md))
- **App Launcher**: <kbd>Alt</kbd>+<kbd>Space</kbd> runs anything on `$PATH`, frecency-ranked, with desktop-entry names and icons
- **Keybind Manager**: <kbd>Prefix</kbd>+<kbd>k</kbd> in-app, or `tuios keybinds doctor` and `tuios keybinds explain <key>` from the shell
- **Aggregate View**: Searchable list of every window across every workspace, with previews ([docs](docs/LAYOUT_MODES.md#aggregate-view))
- **Multifocus**: Broadcast typing to several panes at once, `Ctrl`+`Shift`+click to select ([docs](docs/LAYOUT_MODES.md#multifocus))

### More
- **Showkeys Overlay**: Display pressed keys for presentations
- **Spotlight**: Light one area of the screen and dim the rest, for demos and recordings (`[spotlight]` config table)
- **Screenshots**: `tuios screenshot` renders a pane to PNG, SVG, ANSI, HTML or text
- **Dock Components**: Your own commands drawn in the dock, updated on events, from a running command, or by polling ([examples](examples/dock/README.md))
- **Customizable Keybindings**: TOML configuration with Kitty protocol support
- **Hooks**: Run shell commands on ten events, including window, workspace, attach and agent state changes ([docs](docs/HOOKS.md))
- **Mouse Support**: Wheel scrollback, drag-to-select with copy on release, double-click word and triple-click line, window drag, resize, scrollbar
- **SSH Server Mode**: Remote terminal multiplexing
- **Web Terminal Mode**: Browser-based access (separate `tuios-web` binary)
- **Themes**: Bundled themes plus custom themes from JSON, with chrome designed for truecolor, 256 and 16 colours and for light themes ([docs](docs/THEMES.md))
- **Host Colours**: tuios asks your terminal for its colours, passes them to programs that ask, and follows its light and dark switch
- **Backgrounds**: `appearance.background` paints empty cells with the theme's background or a colour of your own, per surface if you like
- **Motion**: `appearance.motion` is `none`, `basic` or `full` (fades, the working shimmer, confetti)
- **Glyph Sets**: Choose the characters the chrome is drawn with ([docs](docs/GLYPHS.md))

## Quick Start

```bash
tuios                    # Launch TUIOS
tuios --show-keys        # Launch with key overlay for learning
tuios --standalone       # Launch without the daemon, for this run only
```

`tuios` attaches to a daemon-backed session, so the session outlives the
terminal window it started in. New panes are tiled. See
[SESSIONS.md](docs/SESSIONS.md) to turn either off.

### Essential Keys

| Key | Action |
|-----|--------|
| <kbd>Ctrl</kbd>+<kbd>P</kbd> | **Command palette**: search and run any action |
| <kbd>Alt</kbd>+<kbd>Space</kbd> | **Launcher**: search and start a program (<kbd>Enter</kbd> runs it, <kbd>Tab</kbd> types it out) |
| <kbd>n</kbd> | New pane (Window Management mode) |
| <kbd>i</kbd> / <kbd>Enter</kbd> | Enter Terminal mode |
| <kbd>Prefix</kbd>+<kbd>Esc</kbd> or <kbd>Alt</kbd>+<kbd>Esc</kbd> | Back to Window Management mode (a bare <kbd>Esc</kbd> goes to the shell) |
| <kbd>Prefix</kbd>+<kbd>d</kbd> | Detach in a daemon session, otherwise back to Window Management mode |
| <kbd>z</kbd> (WM) or <kbd>Prefix</kbd>+<kbd>z</kbd> | Toggle pane zoom |
| <kbd>Prefix</kbd>+<kbd>Space</kbd> | Toggle BSP tiling |
| <kbd>Prefix</kbd>+<kbd>[</kbd> | Enter copy mode (vim scrollback) |
| <kbd>Prefix</kbd>+<kbd>S</kbd> | Session switcher |
| <kbd>Prefix</kbd>+<kbd>L</kbd> then <kbd>l</kbd>/<kbd>s</kbd> | Load/Save layout template |
| <kbd>Prefix</kbd>+<kbd>?</kbd> | Help overlay |
| <kbd>Prefix</kbd>+<kbd>q</kbd> | Quit |

The **prefix key** is <kbd>Ctrl</kbd>+<kbd>B</kbd> by default (configurable).

### Daemon Mode

```bash
tuios new mysession          # Create persistent session
tuios attach mysession       # Reattach
tuios ls                     # List sessions
tuios kill-session mysession # Kill session
```

### Layout Templates

```bash
# In-app: Ctrl+B, L, l to load / Ctrl+B, L, s to save
# Or via command palette: Ctrl+P → "Save layout" / "Load layout"

# CLI:
tuios layout list            # List saved layouts
tuios layout delete mysetup  # Delete a layout
tuios layout export mysetup  # Export as tape script
```

### Configuration

```bash
tuios config edit            # Edit config in $EDITOR
tuios keybinds list          # View the common keybindings
```

See the [configuration reference](https://tuios.dev/docs/configuration) (or run `tuios list-options`) for all options including `show_clock`, `show_cpu`, `show_ram`, `shared_borders`, `window_button_style`, `window_button_position`, custom themes, and keybinding customization.

## Architecture

TUIOS follows the Model-View-Update pattern on Bubble Tea v2. For details, see [Architecture Guide](docs/ARCHITECTURE.md).

**Key design decisions:**
- **Event-driven rendering**: PTY reader goroutines signal bubbletea via a buffered channel. No fixed-rate ticking for terminal content.
- **Kitty graphics passthrough**: Image IDs are reused across frames for flicker-free video. Output is batched with the render cycle and wrapped in mode 2026 sync.
- **BSP tiling**: Binary space partitioning tree with configurable schemes (spiral, smart split). Shared borders mode overlaps window rects and draws separator lines as a separate layer.
- **Copy mode**: Full vim navigation over scrollback. Wheel scrolling and mouse selection borrow the same machinery through an implicit session that presents as nothing at all, plus scrollbar interaction and selection auto-scroll (timer-based continuous drag scrolling).

**Core Components:**
- **Window Manager** ([`internal/app/os.go`](./internal/app/os.go)): Central state, workspaces, overlays
- **Terminal Emulation** ([`vt/`](./vt/)): ANSI parser with scrollback, kitty/sixel graphics, kitty keyboard protocol, OSC 133
- **Rendering** ([`internal/app/render.go`](./internal/app/render.go)): Layer composition, viewport culling, graphics batching
- **Input** ([`internal/input/`](./internal/input/)): Modal routing, 100+ configurable keybindings, mouse handling
- **Kitty Passthrough** ([`internal/app/kitty_passthrough.go`](./internal/app/kitty_passthrough.go)): Flicker-free image forwarding with ID reuse and sync output

## Performance

- **Event-driven rendering**: Zero CPU at idle. Renders only when PTY data arrives or interaction occurs.
- **Kitty graphics**: Flicker-free via image ID reuse. Tearing-free via mode 2026 sync + render cycle batching.
- **Fast unfocused render**: Unfocused panes use emulator's built-in `Render()` instead of cell-by-cell, unless `appearance.dim_unfocused` needs each cell.
- **Style caching**: LRU cache with sequence-based change detection (40-60% allocation reduction).
- **Viewport culling**: Off-screen and minimized panes skip rendering.
- **Memory pooling**: Pooled strings, buffers, and styles.

## Development

```bash
git clone https://github.com/getan/tuios-mini.git
cd tuios-mini
go build -o tuios ./cmd/tuios
./tuios
```

```bash
go test ./...              # Run tests
go vet ./...               # Vet
golangci-lint run          # Lint with the checks CI runs (.golangci.yml)
govulncheck ./...          # Known vulnerabilities in what the build reaches
```

## License

This fork is distributed under the MIT License. The original TUIOS copyright
(c) 2025 Gaurav Gosain is retained in [LICENSE](LICENSE), together with the
copyright for modifications made by this fork. See [LICENSE](LICENSE) for the
full text.

## Acknowledgments

- [TUIOS](https://github.com/Gaurav-Gosain/tuios) by Gaurav Gosain, the
  MIT-licensed project from which this fork derives
- The [Charm](https://charm.sh) team for Bubble Tea, Lipgloss, and the Go terminal ecosystem
- [CharmTone](https://github.com/charmbracelet/tone) by Charmbracelet, Inc. (Apache License 2.0) for the colours of the `charmtone` themes
- The vim, tmux, and i3 communities for interface design inspiration
- [Ghostty](https://ghostty.org), [Kitty](https://sw.kovidgoyal.net/kitty/), and [WezTerm](https://wezfurlong.org/wezterm/) for excellent terminal emulators with graphics support
