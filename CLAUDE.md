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

## The gate

CI runs exactly this, and so should you before pushing:

```bash
go build ./... && go test -race ./... -count=1 && golangci-lint run && ./scripts/deadcode.sh
```

`-race` is in the gate because hc shells out to git and reads its output; a race
here surfaces as a flaky staging bug, which is the worst kind to chase.

`.golangci.yml` enables the usual correctness set plus **`gocognit`,
`funlen` and `dupl`**, because hc measured the worst complexity profile of the
Go repos here (mean cognitive 6.29 against tp's 2.80, and 10.4% of files over
500 lines). Three notes on how that file is set up, each of which took a
measurement to get right:

1. **Test files are excluded from `gocognit`/`funlen`/`dupl`.** hc's two most
   complex functions are the property tests (cognitive 94 and 71) and every
   clone found at threshold 150 was in a `_test.go`. Left on, the three linters
   would only ever complain about the most valuable code in the repo.
2. **Eleven production functions are baselined with `//nolint` and their
   measured number.** Read the number: if a function grew past what its comment
   claims, the comment is now a lie. `nolintlint` requires an explanation on
   every suppression, so a baseline cannot quietly become an excuse. Refactoring
   one to clear it is welcome; adding a twelfth is not.
3. **`uniq-by-line: false`**, because the default hides every `funlen` finding
   behind the `gocognit` one on the same declaration line.

## Tooling beyond the gate

| Tool | When | Why |
|------|------|-----|
| `deadcode -test ./...` | In the gate (`scripts/deadcode.sh`) | Fails on code nothing reaches at all. golangci-lint's `unused` skips exported identifiers by design, so an exported function with zero callers passes it; `deadcode` builds a call graph and does not care about case |
| `deadcode ./...` | End of an implementation phase, **diffed against the previous run** | Reports test-only code; never in the gate, where it would fail on work in progress |
| `govulncheck ./...` | Before every release | Reports only vulnerabilities the code actually calls |
| `gremlins unleash ./internal/<pkg>` | Before a release, and whenever a claim is made about test quality | Mutation testing. Pass ONE package (`./internal/plan`), not `./internal/plan/...` -- gremlins appends `/...` itself |

**NEVER pass `--test-cpu`. It silently falsifies the result.** gremlins passes
`-cpu N` to `exec.Command` as a single argument, so `go test` dies on an
unrecognised flag, the test binary never starts, the exit code is non-zero, and
gremlins scores **every runnable mutant as not-surviving**. Measured here on one
tree with one known survivor:

```
--workers 4        Killed 29  Lived 1  96.67%   (finds validate.go:197)
--test-cpu 2       Killed 30  Lived 0  100.00%  (finds nothing, reports perfection)
```

**The sharp tell is `Not covered > 0` next to `Lived: 0`, which cannot happen
honestly.** A tree carrying mutants no test reaches is a tree whose tests are
not exhaustive, so something should have survived. `Lived: 0` beside `100.00%`
is NOT the tell on its own -- a genuinely exhaustive package produces it
honestly, and `internal/plan` here does.

**Do not build the detector on which bucket the mutants land in -- that part is
machine-dependent.** On this machine they all became KILLED, and the arithmetic
closed on a second repo's package: `83 + 5 + 3 = 91`, the broken run's KILLED
equalling the honest run's KILLED + LIVED + TIMED OUT. On a third machine the
same flag turned 47 KILLED and 5 LIVED into 52 TIMED OUT instead -- same false
100%, opposite shape. Someone hunting the KILLED signature there would have read
52 timeouts as a loaded machine and moved on.

What IS portable is that NOT COVERED never moves, because the coverage profile
is collected BEFORE any mutant runs and so never reaches the failing `exec`.
That is the whole reason the tell above is the one to use: it rests on the only
part of the shape that survives the change of machine.

Two corollaries worth keeping:

- A run reporting `Lived: N` for N > 0 is proof the harness really ran, since
  broken mode has no surviving category left to report.
- Confirm a real 100% the way this repo's was: apply a mutation by hand and
  watch the suite go red. `ValidateCoverage`'s `>=` to `>` fails
  `TestValidateCoverage_HunkOnePastTheEnd`, so the number here is earned.

Upstream fix is PR #273, in no release yet.

**Calibrate the timeout coefficient, or the output is noise.** On default
settings `internal/plan` reports 0% efficacy with 24 mutants timing out; at
`--timeout-coefficient 50` the same tree reports 100%. The coefficient is the
price of one hung mutant, so it usually dominates the run -- more than
`--workers` or package scope. The criterion for lowering it is NOT "efficacy
stopped moving": that hides the real risk, which is a **false kill** when a slow
but passing test gets cut off and reads as detection. The criterion is: does any
mutant that LIVED at the high coefficient become TIMED OUT at the low one? Diff
the two `-o` files on file+line+column+mutator; the answer must be zero.

**Always pass `--workers <n>`.** Unpinned, gremlins drove this machine's load
average from 8 to 177 and manufactured most of its own timeouts; two unpinned
runs of the same tree do not compare. For the same reason, never let two runs
overlap -- check `pgrep -x gremlins` first. Use `-x`, not `-f`: `pgrep -f
'gremlins unleash'` matches the waiting shell's own command line and loops
forever.

**`-o <file>` makes classification mechanical.** It writes `file_name`, `line`,
`column`, `type` and `status` per mutant, so "what is new since last round" is a
set difference rather than a squint.

**`-D/--diff` does not work in this version -- do not reach for it.** Measured:
on a clean tree, `-D master` (an empty diff, so zero mutants expected) mutated
the whole package, 30 mutants. Narrow by package instead, which is not an
approximation but an exact equivalence: in default mode gremlins runs only the
mutated package's own tests, so an unchanged package's verdicts cannot change.
The same fact means efficacy UNDERSTATES detection -- package B's tests
exercising package A never count toward A's mutants.

Cost, measured: `internal/plan` is 30 mutants in ~4s; `internal/cli` is 426
mutants in ~16 minutes. That is a release-time check, not a per-commit gate.

**A surviving mutant is not a score to drive down.** Classify it: an equivalent
mutant nothing can observe, an undocumented boundary, or a documented contract
with no boundary test. Only the last is worth acting on, and say which ones are
being left and why. `NOT COVERED` and `TIMED OUT` are their own categories, not
survivors.

Worked example, since this repo has one: `ValidateCoverage`'s `h >= len(hunks)`
survived a `>=`-to-`>` mutation because the only test used index 5 against a
3-hunk file -- a value an off-by-one still rejects. The contract was documented
(the error names `indices 0-%d`); the boundary was not tested. Index 3 was the
one input that separates the two, and `internal/plan` now runs at 100% efficacy
with 100% mutator coverage.

**Do not gate on** 100% coverage, CRAP, or Halstead Difficulty. Some teams hold
100% coverage against a 4% mutation score; CRAP at full coverage reduces to
cyclomatic complexity and carries no independent information; and across 4,618
functions in these four repos exactly three exceed Halstead D of 80, none of
them in hc.
## Committing in this repo (dogfooding)

ALWAYS use `hc` itself to commit changes to this repo: build the binary (`go build -o /tmp/hc ./cmd/hc/`), run `/tmp/hc diff --json`, write a plan, and run it via heredoc. This dogfoods the agent workflow and surfaces UX problems and improvement ideas that unit tests cannot. Follow the granularity rules in `skills/hc/SKILL.md` (one file per commit by default; multi-file only for mechanical sweeps or inseparable changes; feat/fix/test/docs never share a commit; tests are ALWAYS separate commits from the code they cover, one commit per NEW test -- only modifications to existing tests may group, when one context drives them; history size is never a concern). Commit at unit-of-work cadence -- after each change + its passing test -- never as one batch at the end of the task (stacked edits fuse into inseparable hunks).

Every time hc is used, if a UX problem, bug, or improvement opportunity is noticed, apply the improvement immediately (code fix, SKILL/spec update, test) and include it as its own commit in the current plan.

## Releasing

Tagging `vX.Y.Z` triggers goreleaser (binaries + brew formula); pushing master publishes the skill. RULE: every release MUST get curated release notes -- goreleaser's grouped changelog is only a fallback. Write them in the house style of the previous releases (`# hc vX.Y.Z`, one-line tagline, `## Highlights` with a subsection and code sample per feature, breaking changes called out, short `## Docs` section) and apply with `gh release edit vX.Y.Z --notes-file notes.md` right after the release lands.

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
| `hc run --continue` | Finish a plan a stopped run left part-way (same plan, same hunk indices) |
| `hc plan` | Draft working-tree plan (file-first + section-split; TODO messages rejected by run) |
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
    plancmd.go                hc plan command (draft working-tree plans)
    run.go                    hc run command (Phase 1 + Phase 2)
    runstate.go               Resume record behind hc run --continue
    log.go                    hc log command (per-commit hunks for rewrite)
    split.go                  hc split command (draft file-first rewrite plans)
    rewrite.go                hc rewrite command (conflict-free history splitting)
    section.go                Hunk section attribution (declaration / import re-labeling)
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
- Every git command runs with `LC_ALL=C`. git's lockfile error is translated and the translation moves the punctuation: under `tr_TR.UTF-8` the path comes back in double quotes, which is enough for the retry matcher to miss it and silently disable the retry for exactly the users most likely to hit it
- A command that loses the race for `index.lock` is retried in `internal/git/lock.go` (20 ms to 400 ms with jitter, 2 s total, `HC_LOCK_TIMEOUT` to override). The gate is git's "unable to create the lock" error and nothing else -- that error proves the command never touched the index, which is the only reason repeating it is safe

## Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/spf13/cobra` | CLI framework |
| `github.com/bluekeyes/go-gitdiff` | Diff parsing |
| `github.com/bmatcuk/doublestar/v4` | `allow_unplanned` glob matching (`**` support) |
| `github.com/mattn/go-isatty` | TTY detection |
| `github.com/fatih/color` | Colored TTY output |
| `git` (external) | All git operations via `os/exec` |
