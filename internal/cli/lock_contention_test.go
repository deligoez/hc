package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deligoez/hc/internal/output"
)

// TestLockContentionMidPlanIsNamedInTheHint reproduces the reported failure:
// in an agent's repository some other git process takes the index lock
// between two of a plan's commits, and hc dies on the next `update-index`.
// A post-commit hook stands in for that neighbour, which makes the race
// deterministic. hc cannot retry a lock nobody ever releases, but the hint it
// leaves behind must say so -- otherwise the agent reads exit 3 as a broken
// plan and rebuilds a plan that was never wrong.
func TestLockContentionMidPlanIsNamedInTheHint(t *testing.T) {
	t.Setenv("HC_LOCK_TIMEOUT", "50ms")

	dir := t.TempDir()
	r := initRepo(t, dir)

	must(t, os.WriteFile(filepath.Join(dir, "first.txt"), []byte("a\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "second.txt"), []byte("b\n"), 0o644))
	must(t, run(r, "add", "-A"))
	must(t, run(r, "commit", "-m", "base"))

	must(t, os.WriteFile(filepath.Join(dir, "first.txt"), []byte("a2\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "second.txt"), []byte("b2\n"), 0o644))

	hook := "#!/bin/sh\n: > \"$(git rev-parse --git-path index.lock)\"\n"
	must(t, os.WriteFile(filepath.Join(dir, ".git", "hooks", "post-commit"), []byte(hook), 0o755))

	res, acErr := runPlan([]byte(`{"commits":[
		{"message":"first","files":[{"path":"first.txt"}]},
		{"message":"second","files":[{"path":"second.txt","hunks":[0]}]}]}`), r, false)
	if acErr == nil || acErr.Code != 3 {
		t.Fatalf("want execution error (code 3), got %v", acErr)
	}

	result, ok := res.(*output.Result)
	if !ok || result == nil {
		t.Fatal("execution failure must return the partial *output.Result")
	}
	if result.Committed != 1 {
		t.Fatalf("commit 0 ran before the lock appeared, want 1 committed, got %d", result.Committed)
	}

	failed := result.Commits[1]
	if !strings.Contains(failed.Error, "index.lock") {
		t.Errorf("error should carry git's own message, got %q", failed.Error)
	}
	if !strings.Contains(failed.Hint, "repository lock") {
		t.Errorf("hint must name lock contention rather than blame the plan, got %q", failed.Hint)
	}
}
