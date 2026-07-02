package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deligoez/hc/internal/git"
	"github.com/deligoez/hc/internal/output"
)

func setupPrefixRepo(t *testing.T) (string, *git.Runner) {
	t.Helper()
	dir := t.TempDir()
	r := initRepo(t, dir)
	must(t, os.WriteFile(filepath.Join(dir, "f.txt"), []byte("a\n"), 0o644))
	must(t, run(r, "add", "-A"))
	must(t, run(r, "commit", "-qm", "base"))
	must(t, os.WriteFile(filepath.Join(dir, "f.txt"), []byte("b\n"), 0o644))
	return dir, r
}

func TestPrefixFlagPrepends(t *testing.T) {
	_, r := setupPrefixRepo(t)

	res, acErr := runPlan([]byte(`{"commits":[{"message":"fix: b","files":[{"path":"f.txt"}]}]}`), r, false, "WB-1234: ")
	if acErr != nil {
		t.Fatalf("run failed: %v", acErr)
	}
	if got := res.(*output.Result).Commits[0].Message; got != "WB-1234: fix: b" {
		t.Fatalf("message = %q, want prefixed", got)
	}
	log, _ := r.Run("log", "-1", "--format=%s")
	if strings.TrimSpace(log) != "WB-1234: fix: b" {
		t.Fatalf("committed subject = %q", strings.TrimSpace(log))
	}
}

func TestPrefixFlagIdempotent(t *testing.T) {
	_, r := setupPrefixRepo(t)

	res, acErr := runPlan([]byte(`{"commits":[{"message":"WB-1234: fix: b","files":[{"path":"f.txt"}]}]}`), r, false, "WB-1234: ")
	if acErr != nil {
		t.Fatalf("run failed: %v", acErr)
	}
	if got := res.(*output.Result).Commits[0].Message; got != "WB-1234: fix: b" {
		t.Fatalf("prefix must not double up, got %q", got)
	}
}

func TestNoPrefixLeavesMessagesAlone(t *testing.T) {
	_, r := setupPrefixRepo(t)

	res, acErr := runPlan([]byte(`{"commits":[{"message":"fix: b","files":[{"path":"f.txt"}]}]}`), r, false)
	if acErr != nil {
		t.Fatalf("run failed: %v", acErr)
	}
	if got := res.(*output.Result).Commits[0].Message; got != "fix: b" {
		t.Fatalf("message changed without --prefix: %q", got)
	}
}
