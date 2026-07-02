# hc -- Hunk Commits

Hunk-based atomic commits for AI agents. One JSON plan, N commits.

AI agents produce large diffs that should be split into atomic commits. The agent knows *which hunks belong together* but has no reliable way to execute that plan -- `git add -p` is interactive, manual `git apply` requires line-number arithmetic, and full-file `git add` can't split a file across commits.

`hc` solves this: the agent writes a JSON plan mapping hunks to commits, and `hc` handles everything else.

## Install

**Homebrew** (macOS / Linux):

```bash
brew install deligoez/tap/hc   # alias for the deligoez-hc formula; installs the 'hc' binary
```

**Go**:

```bash
go install github.com/deligoez/hc/cmd/hc@latest
```

Or download a binary from [releases](https://github.com/deligoez/hc/releases).

## Quick Start

```bash
# 1. See what changed -- indices, enclosing function, and the changed lines in one call
hc diff --json
```

```json
{
  "files": [{
    "path": "auth.go",
    "hunks": [{
      "index": 0,
      "header": "@@ -12,3 +12,5 @@",
      "section": "func Login(w http.ResponseWriter, r *http.Request) {",
      "added": 5, "deleted": 3,
      "fingerprint": "3f2a9c184b0d",
      "content": "-if token == \"\" {\n+if len(token) < 16 {\n..."
    }]
  }],
  "summary": {"files": 1, "hunks": 1, "added": 5, "deleted": 3}
}
```

```bash
# 2. Write a plan
cat > plan.json << 'EOF'
{
  "commits": [
    {
      "message": "fix(auth): validate token length",
      "files": [{"path": "auth.go", "hunks": [0, 1]}]
    },
    {
      "message": "feat(auth): add refresh endpoint",
      "files": [
        {"path": "auth.go", "hunks": [2, 3]},
        {"path": "handler.go"}
      ]
    }
  ]
}
EOF

# 3. Execute
hc run plan.json
```

## How It Works

```
Agent  --writes-->  plan.json  --stdin/file-->  hc  --git calls-->  repository
         Looks at diff once.      Validates plan.       Stages & commits.
         Assigns hunks.           Re-indexes hunks.     Working tree untouched.
         Done.                    Builds patches.
```

1. Agent runs `hc diff --json` to see all hunks with indices, enclosing function, and changed lines
2. Agent writes a commit plan (JSON) mapping hunks to commits
3. Agent runs `hc run plan.json` -- all commits created in one call

The agent never touches `git add`, `git apply`, or `git commit` directly.

## Commands

| Command | Description |
|---------|-------------|
| `hc diff` | Show current diff with numbered hunk indices |
| `hc diff --json` | Same, as structured JSON with hunk content, section, and fingerprint |
| `hc run <plan>` | Execute commit plan from file |
| `hc run -` | Execute commit plan from stdin |
| `hc run --dry-run <plan>` | Validate plan without committing |
| `hc log <base>..<head>` | Per-commit indexed hunks (`--files-only` for a cheap survey) |
| `hc split <base>..<head>` | Emit a draft one-file-per-commit rewrite plan |
| `hc rewrite <plan>` | Split existing commits -- conflict-free history rewrite with backup ref |
| `hc --version` | Show version |

## Splitting Existing Commits

Too-coarse commits that already exist (pre-hc history, over-grouped runs) can be split retroactively:

```bash
hc log main..HEAD --json     # per-commit indexed hunks, same schema as hc diff
hc rewrite plan.json         # {"rewrites":[{"commit":"<sha>","commits":[...]}]}
```

Each split must reproduce the original commit's tree byte-for-byte (verified), so downstream commits re-parent without conflicts, the working tree is never touched, and the branch moves in one atomic step with the old head saved at `refs/hc/backup/<branch>`. Author identities/dates are preserved; pushed commits require `--force`.

## Plan Format

```json
{
  "commits": [
    {
      "message": "commit message",
      "files": [
        {"path": "file.go", "hunks": [0, 2]},
        {"path": "other.go"}
      ]
    }
  ],
  "allow_unplanned": ["wip.go"]
}
```

- **`hunks`**: Indices from `hc diff` output. Omit to stage the entire file.
- **`--prefix`**: `hc run --prefix "WB-1234: " <plan>` prepends the string to every commit message (idempotent). For per-commit tickets, write them into the messages.
- **`allow_unplanned`**: Files/globs excluded from coverage validation (`*` = one level, `**` = recursive).
- Every hunk in the diff must be assigned to exactly one commit (complete coverage).
- Renamed/moved files appear as two entries -- a deletion (old path) and a new file -- and both must be planned; git reconstructs the rename in history automatically.

## Architecture

**Two-phase execution:**

- **Phase 1 (Validation):** Parse plan, capture diff, validate coverage, sequential dry-run with temporary index. If anything fails: exit 2, no git state changed.
- **Phase 2 (Execution):** For each commit: reconstruct the staged file content from the original diff, stage it directly into the index, commit.

**Key algorithms:**
- Content reconstruction: staged content is rebuilt from the original diff coordinates (base blob + selected hunks) and staged directly via `git hash-object` + `git update-index` -- no patch application, no hunk re-matching
- Byte-for-byte verification of every deletion against the base, plus a base+all-hunks == working-tree invariant before any commit
- SHA-256 content fingerprinting exposed in `hc diff --json`

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 2 | Validation error (plan issue, no git state changed) |
| 3 | Execution error (unexpected git failure) |

All errors include `error`, `code`, and `hint` fields for agent consumption. On exit 3 the full result is still printed -- every commit with its `status` and `sha` -- so the caller can re-plan only the remaining changes.

## Claude Code Skill

```bash
npx skills add -g deligoez/hc
```

Update after new releases:

```bash
npx skills update -g deligoez/hc
```

## License

MIT
