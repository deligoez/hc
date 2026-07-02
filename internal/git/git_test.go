package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newTestRepo creates a real git repository in a temp dir with one initial commit.
func newTestRepo(t *testing.T) *Runner {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	run("config", "commit.gpgsign", "false")

	if err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "base.txt")
	run("commit", "-q", "-m", "initial")

	return NewRunner(dir)
}

func TestRunReturnsStdout(t *testing.T) {
	r := newTestRepo(t)
	out, err := r.Run("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("expected branch name, got empty output")
	}
}

func TestRunErrorIncludesArgsAndStderr(t *testing.T) {
	r := newTestRepo(t)
	_, err := r.Run("rev-parse", "--verify", "nonexistent-ref")
	if err == nil {
		t.Fatal("expected error for nonexistent ref")
	}
	if !strings.Contains(err.Error(), "rev-parse") {
		t.Errorf("error should mention the git args, got: %v", err)
	}
}

func TestEnsureRepo(t *testing.T) {
	r := newTestRepo(t)
	if err := r.EnsureRepo(); err != nil {
		t.Fatalf("EnsureRepo in a repo: %v", err)
	}

	outside := NewRunner(t.TempDir())
	if err := outside.EnsureRepo(); err == nil {
		t.Fatal("EnsureRepo outside a repo should fail")
	}
}

func TestEnsureCleanStaging(t *testing.T) {
	r := newTestRepo(t)
	if err := r.EnsureCleanStaging(); err != nil {
		t.Fatalf("clean staging reported dirty: %v", err)
	}

	if err := os.WriteFile(filepath.Join(r.Dir, "base.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.Add("base.txt"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := r.EnsureCleanStaging(); err == nil {
		t.Fatal("staged change should make staging dirty")
	}
}

func TestIsUntrackedAndIntentToAdd(t *testing.T) {
	r := newTestRepo(t)

	path := "new.txt"
	if err := os.WriteFile(filepath.Join(r.Dir, path), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	untracked, err := r.IsUntracked(path)
	if err != nil {
		t.Fatalf("IsUntracked: %v", err)
	}
	if !untracked {
		t.Fatal("new file should be untracked")
	}

	if err := r.IntentToAdd(path); err != nil {
		t.Fatalf("IntentToAdd: %v", err)
	}

	// After add -N the file appears in the diff (tracked with empty blob).
	out, err := r.Diff("-U0")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !strings.Contains(out, path) {
		t.Fatalf("diff should include intent-to-add file, got:\n%s", out)
	}

	if err := r.RevertIntentToAdd(path); err != nil {
		t.Fatalf("RevertIntentToAdd: %v", err)
	}
	untracked, err = r.IsUntracked(path)
	if err != nil {
		t.Fatalf("IsUntracked after revert: %v", err)
	}
	if !untracked {
		t.Fatal("file should be untracked again after revert")
	}
}

func TestCommitReturnsShortSHA(t *testing.T) {
	r := newTestRepo(t)

	if err := os.WriteFile(filepath.Join(r.Dir, "base.txt"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.Add("base.txt"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	sha, err := r.Commit("test: update base")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if len(sha) < 7 {
		t.Fatalf("expected short SHA, got %q", sha)
	}
}

func TestResetHead(t *testing.T) {
	r := newTestRepo(t)

	if err := os.WriteFile(filepath.Join(r.Dir, "base.txt"), []byte("v3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.Add("base.txt"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := r.ResetHead(); err != nil {
		t.Fatalf("ResetHead: %v", err)
	}
	if err := r.EnsureCleanStaging(); err != nil {
		t.Fatalf("staging should be clean after reset: %v", err)
	}
}

func TestRunWithStdin(t *testing.T) {
	r := newTestRepo(t)
	out, err := r.RunWithStdin([]byte("hello\n"), "hash-object", "--stdin")
	if err != nil {
		t.Fatalf("RunWithStdin: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("expected object hash on stdout")
	}
}
