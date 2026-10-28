# Git preview index isolation

Failure inventory, written before the fix:

- `IndexTree` calls `git write-tree` on the real index; a live writer's
  `index.lock` makes an otherwise read-only preview fail.
- Even without contention, `write-tree` can write its cache-tree extension
  back to the real index. Background status can also refresh that index.
- Two in-flight preview requests can contend with one another. Dropping a
  stale UI generation does not undo a subprocess already running.
- Rebuilding from HEAD or from working files would lose staged-only content,
  intent-to-add flags, conflicts and sparse/split-index information.
- A split index refers to a shared index beside the original. Relocating a
  raw copy to an unrelated temporary directory can lose that base.
- A linked worktree's index is not the main checkout's index.
- An unborn repository may have no index; previewing it must not create one.
- Corruption or cancellation must remain errors, not appear as a clean tree.
- Cleanup may remove only the uniquely owned snapshot and its lock, never a
  user's real `index.lock`.
- An ambient GIT_INDEX_FILE/GIT_DIR/GIT_WORK_TREE must not retarget a preview.

Regression strategy: real Git in owned throwaway repositories, with an
explicit fixture lock held throughout staged/unstaged reads. Compare the
returned tree with Git's own answer, and compare the original index bytes,
its mtime, the lock marker and working files afterwards. Exercise concurrent
reads and linked, split, sparse, v4, intent-to-add and unborn indexes. A
separate status regression forces a stat-cache refresh and verifies that the
background observer does not write it. These are deterministic contention
regressions; they do not require timing a short-lived index.lock or deleting
any real repository lock.

## Implementation and negative controls

`IndexTree` resolves the actual worktree index with Git's sanitized environment,
opens it as an immutable read snapshot and copies it to a uniquely named
`.tuios-review-index-*` beside the original. Keeping the directory preserves
split-index base resolution; `-c core.splitIndex=false` applies only to the
copy's `write-tree`, not to the repository configuration. An absent original
index is represented by an absent temporary index, not by rebuilding HEAD.
Neither a real index nor a real index.lock is created, refreshed or removed.

The background porcelain reader uses `git --no-optional-locks`, including when
the caller's environment opted in to optional locks. Status letters, rename
parsing and the staged/unstaged grouping are unchanged.

Before changing the implementation, the held-lock regression failed on all
nine original index fixtures and all sixteen concurrent reads; the background
status regression failed because it rewrote stat-cache data. After the fix,
the ten fixtures (including a relative worktree path), both preview modes,
the concurrent reads and error/cleanup cases pass. Verbose output records
the resulting tree, the original index SHA-256 and `lock_preserved=true`.

Golder additionally exercises its real embedded daemon via `review-diff`:
staged/unstaged content is checked while a fixture index.lock remains held,
then four independent clients issue concurrent previews. The old `v0.10.14`
pin fails this wire test with `git_failed: ... index.lock`; the local fixed
module passes without changing the index, the lock marker or working content.

These fixes concern observations, not writes. Actual stage/commit/discard
operations still obey Git's real locks and are not automatically replayed.

## Local validation

- Formatting, root-module build and vet: passed.
- The ordinary full root-module suite (`-count=1 -timeout=20m`): passed,
  including app, session and the testutil whole-suite isolation guard.
- Focused race tests for review, gitstatus, worktree, mini and mini/control:
  passed, as did golder's embedded-daemon held-lock wire regression under race.
- Linux arm64, CGO-disabled root-module cross-build: passed.
- A full root-module race run exceeded both a 6m and a 20m app budget; it is
  **not** recorded as a pass. This repository deliberately runs that detector
  campaign separately with an 80m nightly budget (`go-test.yml`). The ordinary
  full suite and the race coverage of all changed packages are the local gate.
- Linux runtime/container preflight remains suspended; cross-compilation is
  not claimed as Linux runtime testing.
