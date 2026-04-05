---
name: ac
description: Hunk-based atomic git commits for AI agents. Splits large diffs into precise, atomic commits by selecting specific diff hunks per commit.
---

## Activation

This skill activates when:
- The agent needs to create atomic commits from uncommitted changes
- The user asks for hunk-level commit granularity
- The agent has written multiple logical changes (e.g., several tests, feature + test, refactor + fix)

## Workflow

```
# Step 1: See what changed (indexed hunks)
ac diff --json

# Step 2: Write a commit plan
# Map hunks to commits based on logical grouping.
# Use the ORIGINAL diff's hunk indices -- ac handles re-indexing.
# IMPORTANT: Every hunk must be assigned to exactly one commit.

# Step 3: Execute
echo '<plan-json>' | ac run -

# Or with a file:
ac run plan.json

# Optional: validate first
echo '<plan-json>' | ac run --dry-run -
```

## Plan Writing Rules

1. **Run `ac diff --json` once.** Use the indexed output to see all files and hunk indices.
2. **Assign EVERY hunk to exactly one commit.** Complete coverage is required. No hunk can be left unassigned.
3. **Use original indices.** Always reference hunks by their position in the `ac diff` output, even for later commits.
4. **Full-file for simple cases.** If an entire file belongs in one commit, omit `hunks`.
5. **Group by logical change.** One test per commit, one feature per commit, etc.
6. **Conventional commit messages.** Use the project's commit convention.
7. **Use `allow_unplanned` sparingly.** Only for files with WIP changes that should not be committed yet.

## Common Patterns

**One test per commit (5 tests in one file):**
```json
{
  "commits": [
    {"message": "test(auth): add token expiry test", "files": [{"path": "auth_test.go", "hunks": [0]}]},
    {"message": "test(auth): add token refresh test", "files": [{"path": "auth_test.go", "hunks": [1]}]},
    {"message": "test(auth): add token revoke test", "files": [{"path": "auth_test.go", "hunks": [2]}]},
    {"message": "test(auth): add token rotate test", "files": [{"path": "auth_test.go", "hunks": [3]}]},
    {"message": "test(auth): add token validate test", "files": [{"path": "auth_test.go", "hunks": [4]}]}
  ]
}
```

**Feature + test across files:**
```json
{
  "commits": [
    {
      "message": "feat(auth): add refresh endpoint",
      "files": [
        {"path": "auth.go", "hunks": [0, 1]},
        {"path": "handler.go"}
      ]
    },
    {
      "message": "test(auth): add refresh endpoint tests",
      "files": [
        {"path": "auth_test.go"},
        {"path": "handler_test.go"}
      ]
    }
  ]
}
```

**Partial commit with WIP excluded:**
```json
{
  "allow_unplanned": ["experiments/new_idea.go"],
  "commits": [
    {
      "message": "fix(db): close connections on timeout",
      "files": [
        {"path": "db.go", "hunks": [0]},
        {"path": "db_test.go"}
      ]
    }
  ]
}
```

## Error Recovery

ac is designed so errors only happen during validation (before any commits):
1. Read the error and hint from ac's JSON output
2. Fix the plan (adjust hunk indices, add missing files, etc.)
3. Retry: `echo '<fixed-plan>' | ac run -`

No commits have been created, no git state has changed. Simple retry.

## Key Commands

| Command | Purpose |
|---------|---------|
| `ac diff` | See all files and hunks with indices (agent-friendly) |
| `ac diff --json` | Same, as structured JSON (preferred for agents) |
| `echo '<json>' \| ac run -` | Execute plan from stdin (preferred) |
| `ac run plan.json` | Execute plan from file |
| `ac run --dry-run plan.json` | Validate plan without committing |
