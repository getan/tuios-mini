# SSH reduced rendering

## Failure inventory and acceptance checks (before implementation)

- A local client started through OpenSSH is still `ClientLocal`; testing only
  `IsSSHMode` misses the actual `golder workspace` path.
- A web client hosted by a server reached over SSH must not inherit the
  operator's transport policy. Never mutate config.Global or the saved config.
- Changing only max_fps leaves logo size, per-cell backgrounds/gradients,
  hover traffic, dock clocks/meters/custom commands and effects unchanged.
- Refresh-rate auto detection, reload and settings changes must not silently
  re-enable a high-rate renderer in reduced mode.
- Decoration removal must preserve session/window switching, input, scroll,
  selection, prompts, errors and guest-requested mouse behavior.
- Guest text, colors, paste and escape payloads must not be pattern-filtered.
- A slow output transport must not acquire an unbounded queue of old frames;
  tests must measure first-frame bytes and input latency, not just constructor
  flags or CPU. Do not claim this proves every production 60-second stall.

The intended SSH profile uses a small plain welcome, ASCII flat chrome,
no cosmetic motion/gradients/dimming/hover, no cosmetic dock jobs or screen
savers, and a 10 Hz maximum for background pane output. Input and core pane
operations are retained. Local and browser rendering stay unchanged.

The profile is attached to a client/model, not to daemon/session state. A
local operator may request the full UI with `TUIOS_SSH_FULL_UI=1` for comparison.
This is not an SSH-server configuration or a permission override.

## Implemented policy

- OpenSSH-launched `ClientLocal` and explicitly served SSH clients get a
  per-model profile. Browser clients do not inherit the server shell's SSH
  variables. The profile is not written back to the user's configuration.
- The welcome is three short plain lines, not a block-font logo/boxed splash.
- ASCII borders and rail/file marks; no Nerd Font/type-specific icon colours,
  gradients, UI-painted backgrounds, modal dimming or marquee text. The existing
  file fallback distinguishes directories (`>`), parent (`^`) and files (`.`);
  filenames/extensions, Unicode text and file operations remain unchanged.
- Motion, spotlight, decorative clock/CPU/RAM jobs, custom dock/rail jobs and
  screen savers are disabled. Dock keeps mode/session/workspace/window controls,
  copy help, recording state and notifications. Reload cannot lift the cap.
- Window close, minimize and zoom stay as plain monochrome Windows marks
  (`- □ ✕`) instead of the coloured macOS traffic lights, and remain clickable.
  These three glyphs are the only non-ASCII part of the reduced chrome.
- Background pane output composes at at most 10 Hz. Input retains its immediate
  response window. The existing latest-state compositor is used; no unbounded
  output queue or text/escape-pattern filter is introduced.
- Host mouse mode is 1002 (click, wheel, drag) rather than cosmetic 1003 hover.
  A guest that requests all-motion still receives it. Unsolicited button-free
  chrome motion is discarded while real gestures/capture remain operational.

This reduces decorations, not a guest's terminal data or application colours.
It does not promise a remote reset can cross a broken SSH connection.

## Repeated real OpenSSH measurement

Linux arm64, 120x40 terminal, private temporary workspace/home, ordinary test
user, real OpenSSH. No model credentials or external API calls. The slow case
uses an authenticated SSH connection through a loopback TCP proxy limiting the
server-to-client stream to 4096 bytes/sec. The client-to-server direction is
not rate-limited. Timings include terminal delivery, not only View CPU time.

The same fixture starts a shell pane, overwrites a counter 80 times at 20ms
intervals, tests input echo during output, switches to an empty workspace,
waits idle, injects 495 mouse motion records, opens help and detaches/stops.
It records received raw byte counts and monotonic timestamps.

| 4 KiB/s case | Existing v0.10.16 UI | Reduced UI |
|---|---:|---:|
| Whole controlled interaction, received bytes | 22,770 | 9,292 |
| Counter-stream interval, received bytes | 3,466 | 1,015 |
| Input echo during counter output | 259 ms | 63 ms |
| Empty workspace idle for 1 second | 0 bytes | 0 bytes |
| Unsolicited 495 bare-motion records, output | 30 bytes | 0 bytes |
| Help response marker delivery | 1.195 s | 0.911 s |

Total bytes fell about 59%, and the counter-stream interval about 71%. On the
unlimited local SSH link both versions accepted input in milliseconds; the
default UI was already idle-silent in this fixture. Therefore idle repainting
was **not** observed as the cause of a one-minute stall here.

The first alternate-screen marker is not the same as a fully delivered frame.
Likewise a help marker can arrive before the remainder of the overlay; do not
interpret marker-delivery timings as full-frame presentation timings. These
are controlled comparisons, not measurements from the user's bastion.

Evidence: golder `.build/ssh-workspace-evidence/{baseline-stream,
baseline-4k-repeat,reduced-final-4k}.{json,raw.log}`. Scratch probe:
`.scratch/ssh_workspace_probe.py`. Raw streams contain only fixture data;
never enable this recording against real account/session contents by default.

## Validation boundary

The perf-budget regression fails on the unmodified implementation (60 FPS,
6591-byte decorated welcome, all-motion mouse) and passes with this profile.
The root-module full race suite passed in 941 seconds (app: 913 seconds),
using the long budget the repository's workflow already requires. Earlier
4/6-minute attempts were insufficient for the exhaustive settings/fuzz tests.
The complete run also exposed queued attach pushes and stale overlay text
policy; both were repaired. Final app/file/sidebar/rename/workspace/reattach
race regressions passed, including Unicode filenames under ASCII SSH chrome.
Build, vet, formatting, tidy and Linux arm64 cross-build checks passed.

The three retained scheduled-CI fuzz artifacts were replayed before/after
their fixes. Short mutator campaigns passed. The eight-session Linux native
SHM flood integration passed three consecutive runs. Root causes and negative
controls are in [the CI repair report](CI_2026_10_28.md). These Linux native
tests are not a claim that the suspended full Linux container suite ran.

The measurement binary used Golder's private development workspace while
this source was under test; downstream releases must pin the published
`v0.10.17` tag, not commit local `replace` or `go.work` paths.
