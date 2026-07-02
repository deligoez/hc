# CLAUDE.md

## Project

`hc` (Hunk Commits) -- a CLI tool that creates atomic git commits from a JSON plan. Written in Go.

AI agents produce large diffs that should be split into atomic commits. `hc` solves this: the agent writes a JSON plan mapping diff hunks to commits, and `hc` handles all the mechanics -- diff parsing, line-number adjustment, patch construction, and sequential staging.

## Build & Test

```bash
go build ./cmd/hc/       # build
go test ./...            # run all tests
go test ./... -count=1   # no cache
```

## Committing in this repo (dogfooding)

ALWAYS use `hc` itself to commit changes to this repo: build the binary (`go build -o /tmp/hc ./cmd/hc/`), run `/tmp/hc diff --json`, write a plan, and run it via heredoc. This dogfoods the agent workflow and surfaces UX problems and improvement ideas that unit tests cannot. Follow the granularity rules in `skills/hc/SKILL.md` (one file per commit by default; multi-file only for mechanical sweeps or inseparable changes; feat/fix/test/docs never share a commit).

Every time hc is used, if a UX problem, bug, or improvement opportunity is noticed, apply the improvement immediately (code fix, SKILL/spec update, test) and include it as its own commit in the current plan.

## How It Works

```
Agent  --writes-->  plan.json  --stdin/file-->  hc  --git calls-->  repository
        reads diff once          validates         stages & commits
        assigns hunks            re-indexes        working tree untouched
        done                     builds patches
```

1. Agent runs `hc diff --json` -- sees all hunks with indices, section (enclosing function), and content (changed lines)
2. Agent writes a JSON plan mapping hunks to commits
3. Agent runs `hc run plan.json` -- all commits created in one call

## Commands

| Command | Description |
|---------|-------------|
| `hc diff` | Show current diff with numbered hunk indices (TTY) |
| `hc diff --json` | Same, structured JSON with hunk content/section/fingerprint (preferred for agents) |
| `hc run <plan.json>` | Execute commit plan from file |
| `hc run -` | Execute commit plan from stdin |
| `hc run --dry-run <plan>` | Validate plan without committing |
| `hc log <base>..<head>` | Per-commit indexed hunks (`--files-only` = survey mode) |
| `hc split <base>..<head>` | Emit a draft one-file-per-commit rewrite plan |
| `hc rewrite <plan>` | Split existing commits (conflict-free history rewrite, backup ref; `--protect <ref>`, `--summary`) |
| `hc --version` | Show version |

## Plan Format

```json
{
  "commits": [
    {
      "message": "feat(auth): add login",
      "files": [
        {"path": "auth.go", "hunks": [0, 1]},
        {"path": "handler.go"}
      ]
    }
  ],
  "allow_unplanned": ["wip.go"]
}
```

- `hunks` field: indices from `hc diff` output. Omit to stage entire file.
- `allow_unplanned`: file paths/globs excluded from coverage validation (doublestar: `*` one level, `**` recursive).
- Every hunk in the diff must be assigned to exactly one commit.
- `hc run --prefix "WB-1234: "`: prepends the string to every commit message (idempotent). Per-commit tickets go directly into the messages.

## Structure

```
cmd/hc/main.go               Entry point
internal/
  cli/
    root.go                   Cobra root, --json/--quiet/--no-color flags
    diff.go                   hc diff command
    run.go                    hc run command (Phase 1 + Phase 2)
    log.go                    hc log command (per-commit hunks for rewrite)
    split.go                  hc split command (draft file-first rewrite plans)
    rewrite.go                hc rewrite command (conflict-free history splitting)
    exitcodes.go              Exit 0/2/3
  diff/
    types.go                  FileDiff, Hunk, Line types
    parse.go                  Wraps go-gitdiff
    fingerprint.go            SHA-256 content fingerprinting (informational, diff output)
    reconstruct.go            Content reconstruction -- the staging core
  plan/
    plan.go                   Plan, Commit, FileEntry types
    parse.go                  JSON parser with validation
    validate.go               Coverage validation + field validation
    rewrite.go                RewritePlan types + parsing
  git/
    git.go                    Git command runner
    diff.go                   Diff, IntentToAdd, IsUntracked helpers
    commit.go                 Commit, Add, ResetHead helpers
    index.go                  hash-object / update-index staging helpers
    history.go                commit-tree / read-tree / rev-list helpers for rewrite
  output/
    output.go                 Result types, ACError, TTY/JSON printer
skills/hc/SKILL.md            Agent skill for Claude Code
spec/0.2.0.md                 Full specification
```

## Architecture

### Two-Phase Execution

- **Phase 1 (Validation):** Parse plan, capture diff (`git diff -U0 --no-renames`), validate coverage (every hunk assigned), capture per-file base blobs, verify base+all-hunks == working tree, simulate every commit's staging on a temporary index (`GIT_INDEX_FILE`). If anything fails: exit 2, no git state changed.
- **Phase 2 (Execution):** For each commit: reconstruct staged content from base + committed + selected hunks (original diff coordinates -- never re-diffed), store via `git hash-object -w`, point the index at it via `git update-index --cacheinfo`, commit.

### Key Algorithms

- **Content reconstruction:** staged content = Reconstruct(base blob, union of committed+selected hunks), a pure text operation on original diff coordinates. Delete lines are verified byte-for-byte against the base (drift detection). No patch text, no `git apply`, no hunk re-matching -- immune to git re-splitting/sliding hunks over repeated content (proven by property fuzzing).
- **Content fingerprinting:** SHA-256 of ordered delete/add lines; exposed in `hc diff --json` for hunk identification (informational only).

### Error Handling

- Exit code 2 for all validation errors (plan issues, no git state changed)
- Exit code 3 for execution errors (unexpected git failure during Phase 2)
- Every error includes `error`, `code`, `hint` fields in JSON
- Error messages must match spec Section 6.2 exactly

### Edge Cases Handled

- New (untracked) files via `git add -N` before diff capture
- Deleted files via `git add`
- Renamed files as delete+add (`--no-renames`; git reconstructs renames at display time)
- Binary files (full-file only, hunk-select = validation error)
- No-trailing-newline files (`\ No newline at end of file` marker)
- Repeated-content ambiguity (git re-splitting/sliding hunk windows) via reconstruction staging
- Pre-staged changes = hard error (requires clean staging area)

## Conventions

- Error messages and hints must match spec Section 6.2 exactly
- Exit code 2 for all validation errors, 3 for execution errors
- `--no-ext-diff` flag on all git diff calls (bypass external diff tools)
- `--no-renames` on diff calls: renames are committed as delete+add (detection-at-run-time silently dropped old-path deletions from coverage)
- Tests use real git repos via `t.TempDir()`
- All validation errors revert `git add -N` operations before returning

## Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/spf13/cobra` | CLI framework |
| `github.com/bluekeyes/go-gitdiff` | Diff parsing |
| `github.com/bmatcuk/doublestar/v4` | `allow_unplanned` glob matching (`**` support) |
| `github.com/mattn/go-isatty` | TTY detection |
| `github.com/fatih/color` | Colored TTY output |
| `git` (external) | All git operations via `os/exec` |
