---
name: hc
description: Hunk-based atomic git commits for AI agents. Splits large diffs into precise, atomic commits by selecting specific diff hunks per commit.
---

# hc -- Hunk Commits Skill

Hunk-based atomic commits for AI agents. One JSON plan, N commits. You assign hunks, hc handles all git mechanics (re-indexing, patch construction, staging, committing). Works from any subdirectory of the repo; paths are always repo-root-relative.

## Activation

This skill activates when:
- The agent needs to create atomic commits from uncommitted changes
- The user asks for hunk-level commit granularity
- The agent has written multiple logical changes (e.g., several tests, feature + test, refactor + fix)

## Workflow

```bash
# Step 1: See what changed. ONE call gives you everything:
# indices, headers, enclosing function (section), and the changed lines (content).
# Do NOT run 'git diff' separately -- hc diff --json already includes hunk content.
hc diff --json

# Step 2: Write the plan (see rules below), then execute via heredoc.
# ALWAYS use a quoted heredoc, never echo '<json>' -- commit messages
# containing quotes break shell quoting.
hc run - <<'PLAN'
{"commits":[{"message":"feat(auth): add login","files":[{"path":"auth.go","hunks":[0,1]}]}]}
PLAN
```

`hc run` is atomic at the plan level: the whole plan is validated (including a simulated apply of every commit) before the first real commit is created. A validation failure means nothing changed -- fix the plan and retry. `--dry-run` exists but is rarely needed; `hc run` performs the same validation anyway.

## Reading the diff

Each hunk in `hc diff --json` carries what you need to classify it -- never guess from headers alone:

- `content` -- the changed lines, `+`/`-` prefixed. Diffs use `-U0`, so this is exactly the change, no context lines.
- `section` -- the enclosing function/context from git (which function does this hunk touch?).
- `index` -- what you reference in the plan.
- Top-level `warnings` -- non-fatal issues (e.g. pre-staged changes that `hc run` will reject). Always check it.

**File states and what to plan:**

| Diff entry looks like | State | Plan entry |
|---|---|---|
| `hunks: [...]` | Modified file | `{"path": ..., "hunks": [...]}` or omit `hunks` for whole file |
| `is_untracked: true`, `hunks: []` | New file | Full-file only (no indices exist yet): `{"path": ...}` |
| `is_deleted: true` | Deleted file | `{"path": ...}` (full-file stages the deletion) |
| `is_binary: true` | Binary file | Full-file only; `hunks` is a validation error |
| `hunks: []`, no flags | Mode-only change (e.g. chmod +x) | Full-file: `{"path": ...}` |
| Old path deleted + new path untracked | Rename/move | TWO entries: `{"path": "old"}` and `{"path": "new"}` (may share a commit); git shows it as a rename in history automatically |

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

## Commit Granularity -- the most important rule

Agents systematically err toward commits that are TOO BIG. Default to splitting.

- **One reviewable idea per commit.** If you would need "and" in the commit message, split it: `feat(auth): add login endpoint and fix token expiry` is two commits.
- **Type boundaries are commit boundaries.** feat / fix / test / refactor / docs / chore never share a commit unless inseparable (a rename that the feature requires, for example).
- **Same file is NOT a reason to combine.** Five new tests in one file = five commits (one hunk each). Use the `section` field to see which function each hunk touches.
- **Different subsystems = different commits**, even for the same kind of change.
- **The litmus test:** could `git revert` of this commit undo exactly one decision? If it would drag unrelated changes along, split.
- Combine only when hunks are mutually dependent -- code + the type it requires, a call site + the signature change it follows.
- **New files can't be hunk-split.** If a new file will contain several logical changes, prefer creating it in separate passes and committing between them.

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
6. **Use `allow_unplanned` sparingly** -- only for WIP that must stay uncommitted. `*` matches one path level; use `dir/**` for recursive.

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

**Feature + test across files:**
```json
{
  "commits": [
    {
      "message": "feat(auth): add refresh endpoint",
      "files": [{"path": "auth.go", "hunks": [0, 1]}, {"path": "handler.go"}]
    },
    {
      "message": "test(auth): add refresh endpoint tests",
      "files": [{"path": "auth_test.go"}, {"path": "handler_test.go"}]
    }
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
| `hc diff` | Same, compact TTY view (no content) |
| `hc run - <<'PLAN' ... PLAN` | Execute plan from stdin (preferred) |
| `hc run plan.json` | Execute plan from file |
| `hc run --dry-run -` | Validate only (rarely needed; `run` validates first anyway) |
| `hc --version` | Show version |

## Installation

```bash
# Install the binary
brew install deligoez/tap/hc   # alias for the deligoez-hc formula; installs the 'hc' binary

# Install this skill for Claude Code
npx skills add -g deligoez/hc
```
