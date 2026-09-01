package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deligoez/hc/internal/output"
)

// stopAfterFirstCommit arranges for the second commit of a plan to fail the
// way the reported bug did: a post-commit hook takes the index lock, so the
// next `update-index` cannot have it. Returns a function that lets the
// repository work normally again.
func stopAfterFirstCommit(t *testing.T, dir string) func() {
	t.Helper()
	hook := filepath.Join(dir, ".git", "hooks", "post-commit")
	must(t, os.WriteFile(hook, []byte("#!/bin/sh\n: > \"$(git rev-parse --git-path index.lock)\"\n"), 0o755))
	return func() {
		must(t, os.Remove(hook))
		_ = os.Remove(filepath.Join(dir, ".git", "index.lock"))
	}
}

// TestContinueFinishesAPlanStoppedMidway is the whole point of --continue: the
// remaining commits are created from the ORIGINAL hunk indices. By the time it
// runs, the index has moved forward by one commit, so a plain re-run would see
// a different, shorter diff and index 1 would no longer mean what the plan
// said. The resumed run re-derives the diff from the recorded base instead,
// which is why hunk 1 still lands in commit 1 and hunk 2 in commit 2.
func TestContinueFinishesAPlanStoppedMidway(t *testing.T) {
	t.Setenv("HC_LOCK_TIMEOUT", "50ms")

	dir, r := setupRepo(t)
	writeFile(t, dir, "app.txt", "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\n")
	gitHelper(t, dir, "add", "-A")
	gitHelper(t, dir, "commit", "-m", "base")

	// Three changes far enough apart to stay three hunks under -U0.
	writeFile(t, dir, "app.txt", "ONE\ntwo\nthree\nfour\nFIVE\nsix\nseven\neight\nNINE\n")

	planJSON := []byte(`{"commits":[
		{"message":"first","files":[{"path":"app.txt","hunks":[0]}]},
		{"message":"second","files":[{"path":"app.txt","hunks":[1]}]},
		{"message":"third","files":[{"path":"app.txt","hunks":[2]}]}]}`)

	letItRun := stopAfterFirstCommit(t, dir)
	res, acErr := runPlan(planJSON, r, false)
	if acErr == nil || acErr.Code != 3 {
		t.Fatalf("want the run to stop with an execution error, got %v", acErr)
	}
	first := res.(*output.Result)
	if first.Committed != 1 {
		t.Fatalf("want 1 commit before the stop, got %d", first.Committed)
	}
	if !strings.Contains(first.Hint, "--continue") {
		t.Errorf("the hint should offer --continue, got %q", first.Hint)
	}
	letItRun()

	st, acErr := loadRunState(r)
	if acErr != nil {
		t.Fatalf("the stopped run should have left a resume record: %v", acErr)
	}
	res2, acErr := runPlanFrom(st.Plan, r, false, st.Prefix, st)
	if acErr != nil {
		t.Fatalf("--continue: %v", acErr)
	}

	result := res2.(*output.Result)
	if result.Committed != 3 || result.Total != 3 {
		t.Fatalf("want 3/3 after continuing, got %d/%d", result.Committed, result.Total)
	}
	if result.Commits[0].SHA != first.Commits[0].SHA {
		t.Errorf("the resumed result should carry the original first SHA, got %q want %q",
			result.Commits[0].SHA, first.Commits[0].SHA)
	}
	got := readGitLog(t, dir)
	if tail := strings.Join(got[len(got)-3:], ","); tail != "first,second,third" {
		t.Fatalf("want the plan's three commits in order, got %v", got)
	}

	// Each commit must carry exactly its own hunk, and the three together must
	// leave nothing behind -- the working tree is now the tip.
	for i, want := range []string{"ONE", "FIVE", "NINE"} {
		d := getCommitDiff(t, dir, result.Commits[i].SHA)
		if !strings.Contains(d, "+"+want) {
			t.Errorf("commit %d should add %s, got:\n%s", i, want, d)
		}
	}
	if out := gitHelper(t, dir, "status", "--porcelain"); strings.TrimSpace(out) != "" {
		t.Errorf("working tree should be fully committed, got:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "hc-run-state.json")); !os.IsNotExist(err) {
		t.Error("the resume record should be gone once the plan is complete")
	}
}
