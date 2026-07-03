---
name: hc
description: Hunk-based atomic git commits for AI agents. Splits large diffs into precise, atomic commits by selecting specific diff hunks per commit.
---

# hc -- Hunk Commits Skill

Hunk-based atomic commits for AI agents. One JSON plan, N commits. You assign hunks, hc handles all git mechanics (staging, committing, and -- for existing commits -- conflict-free history splitting). Works from any subdirectory of the repo; paths are always repo-root-relative.

## Activation

This skill activates when:
- The agent needs to create atomic commits from uncommitted changes
- The user asks for hunk-level commit granularity
- The agent has written multiple logical changes (e.g., several tests, feature + test, refactor + fix)
- The user asks to split, break up, or re-granularize EXISTING commits (use `hc log` + `hc rewrite`)

## Workflow

```bash
# Step 1: See what changed. ONE call gives you everything:
# indices, headers, enclosing function (section), and the changed lines (content).
# Do NOT run 'git diff' separately -- hc diff --json already includes hunk content.
hc diff --json

# Step 2 (RECOMMENDED): start from the draft plan -- never from a blank page.
hc plan > draft.json
```

`hc plan` emits the finest mechanical split: one commit per file, split further by enclosing section when a file's hunks span several. Every message is a `TODO (...)` placeholder and **`hc run` refuses TODO messages**, so each entry must be reviewed. Your review job, per entry:

1. **Write a real commit message.** While writing it, sanity-check the entry is one idea.
2. **MERGE entries that belong together** -- mechanical sweeps, inseparable changes, one idea that happens to span sections. Merging is a conscious act; splitting is the default you inherit.
3. **Drop untracked entries** you don't want committed.

```bash
hc run draft.json
```

Hand-written plans (heredoc) remain fine for small diffs -- but read each hunk's `section`/`content` before bundling, and heed the `review granularity` warning `hc run` emits when one commit bundles hunks from multiple sections of a file:

```bash
hc run - <<'PLAN'
{"commits":[{"message":"feat(auth): add login","files":[{"path":"auth.go","hunks":[0,1]}]}]}
PLAN
```

`hc run` is atomic at the plan level: the whole plan is validated (including a simulated apply of every commit) before the first real commit is created. A validation failure means nothing changed -- fix the plan and retry. `--dry-run` exists but is rarely needed; `hc run` performs the same validation anyway.

## Reading the diff

Each hunk in `hc diff --json` carries what you need to classify it -- never guess from headers alone:

- `content` -- the changed lines, `+`/`-` prefixed. Diffs use `-U0`, so this is exactly the change, no context lines.
- `section` -- the enclosing function/context from git (which function does this hunk touch?).
- Per-file `sections` -- the distinct sections the file's hunks touch, in order. **More than one entry = probably more than one idea**: plan hunk-level splits.
- **Signal hierarchy for "is this one idea?":** different files > different sections > distant regions. Non-adjacent changes are separate hunks and therefore split CANDIDATES -- nearby hunks in the SAME section are usually one idea, but far-apart regions in a sectionless file (configs, docs, top-level code) usually are not. `hc plan` encodes exactly this: sections first, then a ~8-unchanged-line gap fallback (skipped for scattered-many files like lockfiles, which are one mechanical change).
- `index` -- what you reference in the plan.
- Top-level `untracked` -- plain untracked paths (compact string array). They carry no hunks and never enter coverage validation; plan a path only to commit that new file.
- Top-level `warnings` -- non-fatal issues (e.g. pre-staged changes that `hc run` will reject). Always check it.

**File states and what to plan:**

| Diff entry looks like | State | Plan entry |
|---|---|---|
| `hunks: [...]` | Modified file | `{"path": ..., "hunks": [...]}` or omit `hunks` for whole file |
| Path in top-level `untracked` array | New file | Only if it should be committed: full-file `{"path": ...}` (no hunk indices exist). Otherwise ignore -- untracked files never enter coverage validation |
| `is_deleted: true` | Deleted file | `{"path": ...}` (full-file stages the deletion) |
| `is_binary: true` | Binary file | Full-file only; `hunks` is a validation error |
| `hunks: []`, no flags | Mode-only change (e.g. chmod +x) | Full-file: `{"path": ...}` |
| Old path deleted + new path untracked | Rename/move | TWO entries: `{"path": "old"}` and `{"path": "new"}` (may share a commit); git shows it as a rename in history automatically |
| `is_intent_to_add: true` (new file WITH hunks) | Stale `git add -N` from another tool | Nothing -- hc skips it from coverage and warns; plan its path only if you want it committed |

**Hunk boundaries are git's:** `-U0` merges edits on adjacent lines into ONE hunk, and hc cannot split inside a hunk. If two logical changes ended up in the same hunk, either commit them together or make the edits in separate passes next time.

## Plan Format

```json
{
  "commits": [
    {
      "message": "feat(auth): add login endpoint",
      "files": [
        {"path": "auth.go", "hunks": [0, 1]},
        {"path": "handler.go"}
      ]
    }
  ],
  "allow_unplanned": ["experiments/**"]
}
```

| Field | Type | Description |
|-------|------|-------------|
| `commits` | array | Ordered list of commits (required) |
| `commits[].message` | string | Non-empty commit message |
| `commits[].files` | array | Files in this commit (at least one) |
| `commits[].files[].path` | string | Relative path from repo root |
| `commits[].files[].hunks` | int[] | Hunk indices from `hc diff`. Omit to stage the whole file. |
| `allow_unplanned` | string[] | Globs excluded from coverage validation (`*` = one level, `**` = recursive) |

## Ticket / Prefix Conventions

- **Same ticket for the whole run:** pass it once -- `hc run --prefix "WB-1234: " -` prepends it to every commit message (idempotent: already-prefixed messages are left alone). Write plain conventional messages and let the flag do the rest.
- **Different tickets per commit** (umbrella branch, many issues in one run): write the ticket directly into each commit message -- `"message": "WB-2940: feat(auth): add login"`. Per-commit prefixes are the plan author's job; hc keeps messages otherwise opaque.

## Anti-patterns -- do NOT do these

- **Do NOT put untracked paths into `allow_unplanned` or into commits "to satisfy coverage".** Coverage validation only covers files with hunks in the diff. Entries in the top-level `untracked` array require NOTHING from you; reference one only when you actually want that new file committed. If you think hc demanded an untracked file, re-read the error -- it was about a different (tracked or intent-to-add) file.
- **Do NOT run `git diff`, `git add`, or `git commit` alongside hc.** `hc diff --json` has everything; `hc run` does all staging.
- **Do NOT re-run `hc diff` between commits of one plan.** One read, one plan, one run.
- **Do NOT bundle same-kind work across files** ("add tests for X, Y and Z" in one commit). Unless it is a mechanical sweep or an inseparable change, every file is its own commit.
- **Do NOT write `hunks: [all indices]` (or omit `hunks`) for a multi-hunk file without reading each hunk's `section`/`content`.** This is the most common under-split. Start from `hc plan` instead -- it pre-splits by section -- and treat `hc run`'s `review granularity` warning as a prompt to re-check.

## Commit Granularity -- the most important rule

Agents systematically err toward commits that are TOO BIG. Default to splitting.

**The default unit is ONE FILE PER COMMIT.** A commit containing two or more files must justify itself against exactly two exceptions:

1. **Mechanical sweep:** the SAME repetitive transformation applied across many files -- a lint/format run, a rename, a comment reword, a codemod, an import reorder. One commit, message names the sweep (`style: apply linter across services`). The test: the per-file diffs are interchangeable in kind; describing one describes all.
2. **Inseparable change:** files that cannot compile or pass independently -- a signature change plus its call sites, code plus the new type it requires. Keep this narrow: "related" is NOT "inseparable". Same feature, same ticket, same directory are NOT reasons to combine.

Everything else splits:

- **Same KIND of change across files is not a sweep.** "Fork the Store* action tests" over 9 files is 9 commits (`test: fork StoreOrderAction test`, `test: fork StorePaymentAction test`, ...) -- each file reviews and reverts on its own. A sweep transforms existing lines mechanically; writing/forking N distinct files is N pieces of work.
- **Split within a file too -- the most-skipped rule.** If a file's hunks carry separable ideas, give each its own commit. Check the file's `sections` array first: more than one section usually means more than one idea. Worked example: a state-machine file with 5 hunks across `region`, `isReadyForSubmission` and a new endpoint = 3 commits (imports ride with the code that needs them), NOT `"hunks": [0,1,2,3,4]` in one.
- **Type boundaries are commit boundaries.** feat / fix / test / refactor / docs / chore never share a commit.
- **The litmus tests:** (a) would the commit message still be accurate for each file alone? Then each file is its own commit. (b) Could `git revert` of this commit undo exactly one decision?
- **New files can't be hunk-split.** If a new file will contain several logical changes, prefer creating it in separate passes and committing between them.

Do not fear high commit counts: 30 one-file commits are better than 6 bundles. hc executes large plans cheaply.

## Commit Ordering

Order commits so the history builds cleanly:
1. Infrastructure / types / helpers with no dependencies first
2. Code that uses them second
3. Tests last (or paired with their feature if the project convention is feature+test)

Goal: each commit should compile and pass tests on its own. hc creates commits strictly in plan order.

## Plan Writing Rules

1. **Run `hc diff --json` once, immediately before planning.** Classify each hunk from its `content` and `section`.
2. **Assign EVERY hunk to exactly one commit.** Complete coverage is validated; unassigned hunks are a hard error.
3. **Use original indices everywhere.** Even in later commits, reference hunks by their position in that one `hc diff` output -- hc rebuilds staged content from those original coordinates, so line-number shifts from earlier commits never matter.
4. **Match the plan entry to the file state** (see the table above): untracked/binary/mode-only/deleted are full-file; renames need both paths.
5. **Conventional commit messages** following the project's convention.
6. **Use `allow_unplanned` sparingly** -- only for TRACKED files with WIP changes that must stay uncommitted. Untracked and intent-to-add files never need it: they are only committed when you explicitly plan their path. `*` matches one path level; use `dir/**` for recursive.

## Common Patterns

**One test per commit (tests in one file, classified via `section`):**
```json
{
  "commits": [
    {"message": "test(auth): add token expiry test", "files": [{"path": "auth_test.go", "hunks": [0]}]},
    {"message": "test(auth): add token refresh test", "files": [{"path": "auth_test.go", "hunks": [1]}]},
    {"message": "test(auth): add token revoke test", "files": [{"path": "auth_test.go", "hunks": [2]}]}
  ]
}
```

**Feature + tests, one file per commit (default granularity):**
```json
{
  "commits": [
    {"message": "feat(auth): add refresh endpoint", "files": [{"path": "auth.go", "hunks": [0, 1]}]},
    {"message": "feat(auth): route refresh endpoint", "files": [{"path": "handler.go"}]},
    {"message": "test(auth): cover refresh endpoint", "files": [{"path": "auth_test.go"}]},
    {"message": "test(auth): cover refresh routing", "files": [{"path": "handler_test.go"}]}
  ]
}
```

**Mechanical sweep -- the one legitimate many-files commit:**
```json
{
  "commits": [
    {"message": "style: apply linter across services", "files": [
      {"path": "svc/a.go"}, {"path": "svc/b.go"}, {"path": "svc/c.go"}
    ]}
  ]
}
```

**Partial commit with WIP excluded:**
```json
{
  "allow_unplanned": ["experiments/**"],
  "commits": [
    {
      "message": "fix(db): close connections on timeout",
      "files": [{"path": "db.go", "hunks": [0]}, {"path": "db_test.go"}]
    }
  ]
}
```

## Splitting Existing Commits (`hc log` + `hc rewrite`)

Already-made commits that are too coarse (pre-hc history, or over-grouped runs) can be split retroactively.

**Fast path -- file-level splitting (the common case):**

```bash
# 1. Survey the range cheaply: per-file flags + hunk_count, NO hunk content.
hc log <base>..HEAD --files-only --json

# 2. Generate the default one-file-per-commit plan (multi-file commits split,
#    single-file commits and merges left as-is). Prints the plan; applies nothing.
hc split <base>..HEAD > plan.json

# 3. REVIEW the draft -- this is your semantic job:
#    - DELETE rewrites that are mechanical sweeps (lint/rename/codemod): they should stay one commit.
#    - Refine messages ({subject} ({basename}) is only a default; --message-template "{subject} :: {path}" etc.).
#    - Add within-file hunk splits where one file carries separable ideas (see below).

# 4. Validate, then apply.
hc rewrite --dry-run --summary plan.json
hc rewrite plan.json
```

**Hunk-level splitting within a file** works exactly like `hc run`: read that commit's entry from `hc log <range> --json` (WITH content) and assign hunk indices across replacement commits:

```json
{"rewrites": [{"commit": "a1b2c3d4e5f6", "commits": [
  {"message": "feat: edit A", "files": [{"path": "f.go", "hunks": [0]}]},
  {"message": "fix: edit B",  "files": [{"path": "f.go", "hunks": [1, 2]}]}
]}]}
```

Rules:

1. **`commit`** is a SHA (12-char prefix from `hc log`/`hc split` is fine). Commits NOT listed in `rewrites` are kept as they are -- their SHAs still change because ancestry changes, but message/author/date/content stay identical.
2. **Coverage applies per commit:** the replacement commits together must cover EVERY hunk of the original commit exactly once (same guarantee as `hc run`; no `allow_unplanned` here). **Hunk indices are per-file within the commit** (each file's hunks start at 0); for whole files just omit `hunks`.
3. **Granularity rules apply unchanged:** default one file per replacement commit; split a file's hunks further when they carry separable ideas; keep mechanical sweeps together (delete them from `hc split` drafts).
4. **Conflict-free by construction:** each split must reproduce the original commit's tree byte-for-byte (hc verifies it; the result reports `"tree_identical": true`), so downstream commits re-parent cleanly -- no rebase conflicts, and the working tree is never touched (uncommitted changes are safe).
5. **Do NOT re-run tests/builds after a rewrite.** The final tree is byte-identical, so every build/test result is unchanged by construction -- `tree_identical: true` in the result is the only verification needed.
6. **Merges mid-range are fine:** an untouched merge is preserved (re-parented with its other parents intact). Only SPLITTING a merge (or the root commit) is refused.
7. **Protect other people's history:** `--protect origin/develop` (repeatable) refuses any rewrite of commits reachable from that ref -- use it whenever the branch builds on shared history, instead of eyeballing the range.
8. **Safety rails:** the old head is saved at `refs/hc/backup/<branch>` (restore with `git reset --hard <backup-ref>`); commits already on a remote are refused unless `--force` (then `git push --force-with-lease`); requires a checked-out branch (no detached HEAD).
9. **`--dry-run`** builds and validates the whole new history (including tree invariants) without moving the branch -- it works even on pushed history without `--force`. Add `--summary` to get counts (`{split, replacements, kept, total_after}`) without the full replacement list.
10. Exit codes match `hc run`: 2 = plan problem, nothing changed; the branch only ever moves in one final atomic step.

## Error Recovery

Every error is JSON with `error`, `code`, and `hint` fields. Exit codes tell you the recovery path:

| Exit | Meaning | Recovery |
|------|---------|----------|
| 2 | Validation error. **No git state changed.** | Fix the plan per the `hint`, retry the same `hc run`. |
| 3 | Execution error mid-plan. Some commits may exist. | The JSON result lists every commit with `status` and `sha` -- committed ones are done. Run `hc diff --json` again and write a NEW plan for the remaining changes only. |

Common validation errors:
- `staging area is not clean` -- something is pre-staged. Run `git reset HEAD`, then retry.
- `hunks [...] not assigned to any commit` / `has changes but is not in the plan` -- add the listed hunks/file to a commit or use `allow_unplanned`.
- `hunk index N out of range` -- the diff changed since you read it. Re-run `hc diff --json` and re-plan.
- `git commit failed` (exit 3) -- usually a pre-commit hook. Staging is left intact: fix the issue, run `git commit -m "<message>"` manually, then re-plan the rest.

## Key Commands

| Command | Purpose |
|---------|---------|
| `hc diff --json` | Indexed hunks WITH content and section -- everything needed to plan |
| `hc plan > draft.json` | Draft plan: file-first + section-split, TODO messages (run refuses TODOs) |
| `hc diff` | Same, compact TTY view (no content) |
| `hc run - <<'PLAN' ... PLAN` | Execute plan from stdin (preferred) |
| `hc run plan.json` | Execute plan from file |
| `hc run --prefix "WB-1234: " -` | Prepend a uniform prefix to every commit message |
| `hc run --dry-run -` | Validate only (rarely needed; `run` validates first anyway) |
| `hc log <base>..HEAD --files-only --json` | Cheap per-commit file survey (no hunk content) |
| `hc log <base>..HEAD --json` | Per-commit indexed hunks WITH content (for hunk-level splits) |
| `hc split <base>..HEAD` | Emit the default one-file-per-commit rewrite plan (review, then pipe) |
| `hc split --hunks <range>` | Same, plus within-file splits grouped by section (draft heuristic) |
| `hc rewrite - <<'PLAN' ... PLAN` | Split existing commits; conflict-free, backup ref kept |
| `hc rewrite --dry-run --summary -` | Validate a rewrite (counts only) without moving the branch |
| `hc --version` | Show version |

## Installation

```bash
# Install the binary
brew install deligoez/tap/hc   # alias for the deligoez-hc formula; installs the 'hc' binary

# Install this skill for Claude Code
npx skills add -g deligoez/hc
```
