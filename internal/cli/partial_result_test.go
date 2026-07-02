package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/deligoez/hc/internal/output"
)

// TestPartialResultOnExecutionFailure guards that an execution failure still
// returns the partial result listing committed SHAs, so the agent can re-plan
// only the remaining changes.
func TestPartialResultOnExecutionFailure(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)

	must(t, os.WriteFile(filepath.Join(dir, "good.txt"), []byte("g\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "bad.txt"), []byte("b\n"), 0o644))
	must(t, run(r, "add", "-A"))
	must(t, run(r, "commit", "-m", "base"))

	must(t, os.WriteFile(filepath.Join(dir, "good.txt"), []byte("g2\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "bad.txt"), []byte("b2\n"), 0o644))

	hook := "#!/bin/sh\ngit diff --cached --name-only | grep -q bad.txt && exit 1\nexit 0\n"
	must(t, os.WriteFile(filepath.Join(dir, ".git", "hooks", "pre-commit"), []byte(hook), 0o755))

	res, acErr := runPlan([]byte(`{"commits":[
		{"message":"good","files":[{"path":"good.txt"}]},
		{"message":"bad","files":[{"path":"bad.txt"}]}]}`), r, false)
	if acErr == nil || acErr.Code != 3 {
		t.Fatalf("want execution error (code 3), got %v", acErr)
	}

	result, ok := res.(*output.Result)
	if !ok || result == nil {
		t.Fatal("execution failure must return the partial *output.Result")
	}
	if result.Committed != 1 || len(result.Commits) != 2 {
		t.Fatalf("partial result should report 1 committed of 2, got %+v", result)
	}
	if result.Commits[0].Status != "committed" || result.Commits[0].SHA == "" {
		t.Errorf("commit 0 should be committed with a SHA, got %+v", result.Commits[0])
	}
	if result.Commits[1].Status != "failed" {
		t.Errorf("commit 1 should be failed, got %+v", result.Commits[1])
	}
	if result.Code != 3 {
		t.Errorf("result.Code = %d, want 3", result.Code)
	}
}
