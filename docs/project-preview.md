# Files, Git and native preview/editor

## Contract and failure cases (written before implementation)

Files and Git are two navigators for the same verified pane/project. Opening a
file selects a client-local native document, not a shell, PTY, daemon session or
numbered workspace. One unpinned preview is reusable; an actual edit pins it.
The terminal layout/processes are not changed while reading a document.

The implementation and its tests must distinguish these failures:

- A file click copies a path, launches a process or types into the underlying
  shell instead of displaying the file. Explicit Copy Path must still work.
- Files and Git disagree about a file's status, or staged/unstaged views show
  the same diff. A file modified on both sides must be selectable on both.
- A later reply for project A replaces project B; previews, saves or review
  notes follow the *current* pane instead of the captured origin.
- Reading or rendering blocks Update, tokenisation runs on every frame, or a
  repository is scanned once per visible file or per keystroke.
- An edit is overwritten by the next preview, undo loses selection/cursor, or
  closing a dirty document silently drops the buffer.
- A truncated, invalid UTF-8, binary, mixed-newline, symlink or special file is
  saved as if it had been completely and faithfully loaded.
- Saving overwrites a file changed by an agent/editor, follows a swapped link,
  changes executable bits/newline encoding, or leaves a partial target.
- File content injects terminal escapes, a remote path is read locally, or an
  automated key acquires the human editor's write authority.
- A failed or timed-out Git read appears as a clean tree. Nested repositories
  and linked worktrees share neither index state nor path identities.

The first editor supports complete local UTF-8 text up to 1 MiB, ordinary
mouse/arrow selection and typing, explicit save, undo/redo and find. Diff,
historical objects and unsupported files stay read-only. A disk conflict asks
the person to reload/compare; there is no automatic force-save, stage, format,
hook execution or model submission.

Git decorations follow herdr-sidebar's VS Code palettes, not terminal warning
yellow: dark modified #E2C08D, added #81B88B, untracked/renamed #73C991,
deleted #C74E39, conflict #E4676B, ignored #6B6B6B; light equivalents live in
the theme package. Status letters remain visible without colour.

## What this change lands

- The rail's Files section opens a native sheet in the content region instead
  of copying a path or starting a pane. The sheet is client-local: no shell,
  PTY, daemon session or numbered workspace is created, and the panes
  underneath keep their layout and processes.
- The sheet reads through `internal/document`: root-confined paths, a 1 MiB
  cap, binary/non-UTF-8 refusal, no symlink following on save, a revision
  check that refuses to overwrite a file another writer changed, and a temp
  file and rename for an atomic write.
- Editing is in place: arrow/Home/End movement, shift-arrow selection, typing,
  backspace/delete, undo/redo (`ctrl+z`, `ctrl+shift+z`), `ctrl+s` to save and
  `ctrl+r` to reload. A dirty document pins the sheet; another file does not
  replace it, and closing asks before discarding.
- Saves are serialized. A reply acknowledges the text actually written, not
  newer edits made while the disk write ran. Another explicit `ctrl+s` during
  that write queues one save of the latest text, using the updated disk
  snapshot. Failure keeps the buffer dirty and drops the queued retry.
  Close/reload/replacement waits for an outstanding writer to finish.
- The project section has two tabs, `files` and `git`. The Git tab lists the
  changed paths of the pane's repository, grouped into conflicts, staged,
  unstaged and untracked, with the VS Code status inks and letters that stay
  legible without colour. The reading runs only while the tab is shown, on the
  Git section's beat.
- Selecting **Staged** opens the existing review UI for that file's
  `HEAD -> index` diff; **Changes** opens `index -> working tree`. The existing
  `review-diff` API already supports `paths` and `index`; this is routing to
  that API, not a new diff renderer. Reload retains the path/index scope.
  Original full-pane review shortcuts and palette actions remain unchanged.
- Line and hunk comments (`c` / `C`), edit (`e`), resolve (`x`), reload (`r`)
  and send (`S`) reuse the original review editor, daemon note store and human
  nonce checks. A single-file review sends only its visible unsent note IDs.
  The captured session/pane and optional `expected_worktree` precondition
  prevent a focus or repository change from redirecting reads/comments/sends.
- Directory watching is platform-specific behind `internal/dirwatch`: kqueue
  on macOS and inotify on Linux. Git status/diff reading uses Git on both.
  No macOS-only filesystem event dependency is added.

## Not in this change

- Standalone or remote-host diff review. Tracked changes require the local
  daemon's review API; an unsupported daemon is reported, not silently
  replaced with a full-file preview. Untracked paths still open the sheet.
- Index conflicts that Git cannot write as a tree are reported by the existing
  review API; no merge-resolution editor is added.
- Find inside the editor, mouse drag selection and horizontal scrolling.
- New staging/commit/format/hook controls. Review comments continue using the
  existing note/send APIs; document writes remain explicit `ctrl+s` saves.

## Verification

`TestProjectSidebarReusesReviewDiffAndComments` drives a real daemon, real Git
index and real TUI through mouse selection of the same file on both sides,
comment creation/edit/reload/resolve, and the shared CLI note store. It avoids
the legacy splash/mode helpers and saves staged, unstaged and comment frames:

```sh
TUIOS_E2E=1 TUIOS_E2E_FRAMES="$PWD/.dev/project-frames" \
  go test ./e2e/tui -run '^TestProjectSidebarReusesReviewDiffAndComments$' -count=1
```

The in-process regressions schedule out-of-order status/save replies and
focus changes deterministically, verify scoped sends and automated-key
refusals, and check the rail memo invalidates when a status read completes.
