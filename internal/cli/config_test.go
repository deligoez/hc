package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deligoez/hc/internal/git"
	"github.com/deligoez/hc/internal/output"
)

func writeHCConfig(t *testing.T, dir, content string) {
	t.Helper()
	must(t, os.WriteFile(filepath.Join(dir, ".hc.json"), []byte(content), 0o644))
}

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

func TestCommitPrefixStatic(t *testing.T) {
	dir, r := setupPrefixRepo(t)
	writeHCConfig(t, dir, `{"commit":{"prefix":"[core] "}}`)

	res, acErr := runPlan([]byte(`{"commits":[{"message":"fix: b","files":[{"path":"f.txt"}]}]}`), r, false)
	if acErr != nil {
		t.Fatalf("run failed: %v", acErr)
	}
	got := res.(*output.Result).Commits[0].Message
	if got != "[core] fix: b" {
		t.Fatalf("message = %q, want prefixed", got)
	}
	log, _ := r.Run("log", "-1", "--format=%s")
	if strings.TrimSpace(log) != "[core] fix: b" {
		t.Fatalf("committed subject = %q", strings.TrimSpace(log))
	}
}

func TestCommitPrefixTicketFromBranch(t *testing.T) {
	dir, r := setupPrefixRepo(t)
	must(t, run(r, "checkout", "-qb", "feature/WB-1234-login"))
	writeHCConfig(t, dir, `{"commit":{"prefix":"${ticket}: ","ticket_from_branch":"[A-Z]+-\\d+"}}`)

	res, acErr := runPlan([]byte(`{"commits":[{"message":"feat: login","files":[{"path":"f.txt"}]}]}`), r, false)
	if acErr != nil {
		t.Fatalf("run failed: %v", acErr)
	}
	result := res.(*output.Result)
	if result.Commits[0].Message != "WB-1234: feat: login" {
		t.Fatalf("message = %q", result.Commits[0].Message)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", result.Warnings)
	}
}

func TestCommitPrefixTicketNoMatchWarnsAndSkips(t *testing.T) {
	dir, r := setupPrefixRepo(t)
	writeHCConfig(t, dir, `{"commit":{"prefix":"${ticket}: ","ticket_from_branch":"[A-Z]+-\\d+"}}`)

	res, acErr := runPlan([]byte(`{"commits":[{"message":"fix: b","files":[{"path":"f.txt"}]}]}`), r, false)
	if acErr != nil {
		t.Fatalf("run failed: %v", acErr)
	}
	result := res.(*output.Result)
	if result.Commits[0].Message != "fix: b" {
		t.Fatalf("message should stay unprefixed, got %q", result.Commits[0].Message)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "commit prefix skipped") {
		t.Fatalf("expected skip warning, got %v", result.Warnings)
	}
}

func TestCommitPrefixIdempotent(t *testing.T) {
	dir, r := setupPrefixRepo(t)
	writeHCConfig(t, dir, `{"commit":{"prefix":"[core] "}}`)

	res, acErr := runPlan([]byte(`{"commits":[{"message":"[core] fix: b","files":[{"path":"f.txt"}]}]}`), r, false)
	if acErr != nil {
		t.Fatalf("run failed: %v", acErr)
	}
	if got := res.(*output.Result).Commits[0].Message; got != "[core] fix: b" {
		t.Fatalf("prefix must not double up, got %q", got)
	}
}

func TestCommitPrefixMalformedConfigRejected(t *testing.T) {
	dir, r := setupPrefixRepo(t)
	writeHCConfig(t, dir, `{"commit":`)

	_, acErr := runPlan([]byte(`{"commits":[{"message":"fix: b","files":[{"path":"f.txt"}]}]}`), r, false)
	if acErr == nil || acErr.Code != 2 || !strings.Contains(acErr.Message, ".hc.json") {
		t.Fatalf("malformed .hc.json should fail validation, got %v", acErr)
	}
}
