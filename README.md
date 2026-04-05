# ac -- Agentic Commits

Hunk-based atomic commits for AI agents. One JSON plan, N commits.

AI agents produce large diffs that should be split into atomic commits. The agent knows *which hunks belong together* but has no reliable way to execute that plan. `ac` solves this: the agent writes a JSON plan mapping hunks to commits, and `ac` handles all the mechanics -- diff parsing, line-number adjustment, patch construction, and sequential staging.

## Install

```bash
go install github.com/deligoez/ac/cmd/ac@latest
```

## Quick Start

```bash
# 1. See what changed
ac diff --json

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
ac run plan.json
```

## How It Works

```
Agent  --[writes]--> plan.json --[stdin/file]--> ac  --[git calls]--> repository
         Looks at diff once.      Validates plan.       Stages & commits.
         Assigns hunks.           Re-indexes hunks.     Working tree untouched.
         Done.                    Builds patches.
```

1. Agent runs `ac diff --json` to see all hunks with indices
2. Agent writes a commit plan (JSON) mapping hunks to commits
3. Agent runs `ac run plan.json` -- all commits created in one call

The agent never touches `git add`, `git apply`, or `git commit` directly.

## Commands

| Command | Description |
|---------|-------------|
| `ac diff` | Show current diff with numbered hunk indices |
| `ac diff --json` | Same, as structured JSON |
| `ac run <plan>` | Execute commit plan from file |
| `ac run -` | Execute commit plan from stdin |
| `ac run --dry-run <plan>` | Validate plan without committing |
| `ac version` | Show version |

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

- **`hunks`**: Indices from `ac diff` output. Omit to stage the entire file.
- **`allow_unplanned`**: Files/globs excluded from coverage validation.
- Every hunk in the diff must be assigned to exactly one commit (complete coverage).

## Architecture

**Two-phase execution:**

- **Phase 1 (Validation):** Parse plan, capture diff, validate coverage, sequential dry-run with temporary index. If anything fails: exit 2, no git state changed.
- **Phase 2 (Execution):** For each commit: re-diff against current index, match hunks by content fingerprint, build adjusted patch, apply, commit.

**Key algorithms:**
- Delta accumulation for line-number adjustment (from Git's `add-patch.c`)
- SHA-256 content fingerprinting for hunk matching across commits
- Content-subset matching for handling git's merged adjacent hunks

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 2 | Validation error (plan issue, no git state changed) |
| 3 | Execution error (unexpected git failure) |

All errors include `error`, `code`, and `hint` fields for agent consumption.

## Claude Code Skill

To use `ac` as a Claude Code skill:

```bash
mkdir -p ~/.claude/skills/ac
cp skills/ac/SKILL.md ~/.claude/skills/ac/SKILL.md
```

## License

MIT
