# tuios-mini

A trimmed, staging fork of [tuios](https://github.com/Gaurav-Gosain/tuios)
whose only job is the terminal core golder needs: sessions, panes, PTYs and
the control surface `tuiosctl` speaks.

This is not a distribution. The target is to merge the retained core into
golder as its workspace mode, so one installed binary can host panes, the
browser host and the agent. Changes here may break tuios compatibility; the
golder caller is the contract, not the standalone CLI.

- Upstream: <https://github.com/Gaurav-Gosain/tuios>
- Baseline: `v0.10.1` (f8b7820a), the release golder runs against today
- License: MIT, see `LICENSE`; upstream copyright is preserved

## Scope

Keep: PTY and terminal emulation, sessions/windows/workspaces, the control
verbs golder calls, pane lifecycle, and the minimal permission scope.

Not in scope: the standalone product surface (plugins, hooks, federation,
webshell, MCP, tmux compatibility, review/worktree/fan, chart/TML tools) or a
long-lived independent release cadence.

## Working with upstream

```sh
git fetch upstream
git merge upstream/main   # choose deliberately; never blanket-merge
```

The baseline is a tag, not upstream `main` (which is 543 commits ahead at the
time of the fork). Security fixes are reviewed and cherry-picked on purpose.
