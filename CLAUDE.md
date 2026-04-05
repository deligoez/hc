# CLAUDE.md

## Project

`ac` (Agentic Commits) -- a CLI tool that creates atomic git commits from a JSON plan. Written in Go.

## Build & Test

```bash
go build ./cmd/ac/       # build
go test ./...            # run all tests
go test ./... -count=1   # no cache
```

## Structure

```
cmd/ac/main.go           Entry point
internal/
  cli/
    root.go              Cobra root, --json/--quiet/--no-color flags
    diff.go              ac diff command
    run.go               ac run command (Phase 1 + Phase 2)
    exitcodes.go         Exit 0/2/3
  diff/
    types.go             FileDiff, Hunk, Line types
    parse.go             Wraps go-gitdiff
    fingerprint.go       SHA-256 content fingerprinting
    match.go             Hunk matching by fingerprint + content-subset fallback
  patch/
    build.go             Patch construction with delta accumulation
    apply.go             git apply --cached --unidiff-zero wrapper
  plan/
    plan.go              Plan, Commit, FileEntry types
    parse.go             JSON parser with validation
    validate.go          Coverage validation + field validation
  git/
    git.go               Git command runner
    diff.go              Diff, IntentToAdd, IsUntracked helpers
    commit.go            Commit, Add, ResetHead helpers
  output/
    output.go            Result types, ACError, TTY/JSON printer
skills/ac/SKILL.md       Agent skill for Claude Code
spec/0.1.0.md            Full specification
```

## Key Design Decisions

- **Zero-context diffs (`-U0`):** Eliminates context-mismatch failures. Each hunk is self-contained.
- **Original indices:** The plan always references hunks from the initial diff. The tool re-indexes internally.
- **Two-phase execution:** Phase 1 validates everything (temporary index). Phase 2 executes deterministically.
- **Content fingerprinting:** SHA-256 of ordered delete/add lines. Matches hunks across commits after line-number shifts.
- **Content-subset fallback:** When git merges adjacent hunks, the tool uses subsequence matching to split them.
- **Pre-staged changes are a hard error:** `ac` requires a clean staging area.

## Conventions

- Error messages and hints must match spec Section 6.2 exactly
- Exit code 2 for all validation errors, 3 for execution errors
- `--no-ext-diff` flag on all git diff calls (bypass external diff tools)
- `-M` flag on diff calls for rename detection
- Tests use real git repos via `t.TempDir()`
