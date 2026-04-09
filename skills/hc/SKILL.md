---
name: hc
description: Hunk-based atomic git commits for AI agents. Splits large diffs into precise, atomic commits by selecting specific diff hunks per commit.
---

# hc -- Hunk Commits Skill

Hunk-based atomic commits for AI agents. One JSON plan, N commits. Agent assigns hunks, tool handles everything else.

## Activation

This skill activates when:
- The agent needs to create atomic commits from uncommitted changes
- The user asks for hunk-level commit granularity
- The agent has written multiple logical changes (e.g., several tests, feature + test, refactor + fix)

## Workflow

```
# Step 1: See what changed (indexed hunks)
hc diff --json

# Step 2: Write a commit plan
# Map hunks to commits based on logical grouping.
# Use the ORIGINAL diff's hunk indices -- hc handles re-indexing.
# IMPORTANT: Every hunk must be assigned to exactly one commit.

# Step 3: Execute
echo '<plan-json>' | hc run -

# Or with a file:
hc run plan.json

# Optional: validate first
echo '<plan-json>' | hc run --dry-run -
```

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
  "allow_unplanned": ["wip_file.go"]
}
```

| Field | Type | Description |
|-------|------|-------------|
| `commits` | array | Ordered list of commits (required) |
| `commits[].message` | string | Non-empty commit message |
| `commits[].files` | array | Files in this commit (at least one) |
| `commits[].files[].path` | string | Relative path from repo root |
| `commits[].files[].hunks` | int[] | Hunk indices from `hc diff`. Omit for full-file staging. |
| `allow_unplanned` | string[] | Paths/globs excluded from coverage validation |

## Plan Writing Rules

1. **Run `hc diff --json` once.** Use the indexed output to see all files and hunk indices.
2. **Assign EVERY hunk to exactly one commit.** Complete coverage is required. No hunk can be left unassigned.
3. **Use original indices.** Always reference hunks by their position in the `hc diff` output, even for later commits. hc handles re-indexing internally.
4. **Full-file for simple cases.** If an entire file belongs in one commit, omit `hunks`.
5. **Group by logical change.** Same type + same specific problem + direct dependency = same commit.
6. **Conventional commit messages.** Use the project's commit convention.
7. **Use `allow_unplanned` sparingly.** Only for files with WIP changes that should not be committed yet.

## Common Patterns

**One test per commit (5 tests in one file):**
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
  "allow_unplanned": ["experiments/*"],
  "commits": [
    {
      "message": "fix(db): close connections on timeout",
      "files": [{"path": "db.go", "hunks": [0]}, {"path": "db_test.go"}]
    }
  ]
}
```

## Error Recovery

Errors only happen during validation (before any commits):
1. Read the `error` and `hint` fields from JSON output
2. Fix the plan (adjust hunk indices, add missing files, etc.)
3. Retry: `echo '<fixed-plan>' | hc run -`

No commits created, no git state changed. Simple retry.

## Key Commands

| Command | Purpose |
|---------|---------|
| `hc diff` | Show all files and hunks with indices |
| `hc diff --json` | Same, as structured JSON (preferred) |
| `echo '<json>' \| hc run -` | Execute plan from stdin |
| `hc run plan.json` | Execute plan from file |
| `hc run --dry-run -` | Validate plan without committing |
| `hc --version` | Show version |

## Installation

```bash
# Install the binary
go install github.com/deligoez/hc/cmd/hc@latest

# Install the skill for Claude Code
mkdir -p ~/.claude/skills/hc
cp skills/hc/SKILL.md ~/.claude/skills/hc/SKILL.md
```
