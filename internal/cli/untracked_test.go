package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deligoez/hc/internal/output"
)

// TestPlainUntrackedFilesDoNotBlockCoverage documents that untracked files
// never enter coverage validation: they are not in the unstaged diff, so a
// plan covering only real changes succeeds without any allow_unplanned noise.
func TestPlainUntrackedFilesDoNotBlockCoverage(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)

	must(t, os.WriteFile(filepath.Join(dir, "real.txt"), []byte("a\nb\n"), 0o644))
	must(t, run(r, "add", "-A"))
	must(t, run(r, "commit", "-qm", "base"))

	// Scratch files: untracked, not gitignored, not in the plan.
	must(t, os.MkdirAll(filepath.Join(dir, "tmp"), 0o755))
	for _, p := range []string{"tmp/s1.txt", "tmp/s2.txt", "thread.json"} {
		must(t, os.WriteFile(filepath.Join(dir, p), []byte("scratch\n"), 0o644))
	}
	must(t, os.WriteFile(filepath.Join(dir, "real.txt"), []byte("a\nCHANGED\n"), 0o644))

	res, acErr := runPlan([]byte(`{"commits":[{"message":"fix: real","files":[{"path":"real.txt","hunks":[0]}]}]}`), r, false)
	if acErr != nil {
		t.Fatalf("plain untracked files must not require planning: %v", acErr)
	}
	if r := res.(*output.Result); r.Committed != 1 {
		t.Fatalf("expected 1 commit, got %+v", r)
	}
	// Scratch files remain untracked.
	out, _ := r.Run("ls-files", "--others", "--exclude-standard")
	for _, p := range []string{"tmp/s1.txt", "tmp/s2.txt", "thread.json"} {
		if !strings.Contains(out, p) {
			t.Errorf("%s should remain untracked", p)
		}
	}
}

// TestStaleIntentToAddSkippedFromCoverage guards the fix for stale
// `git add -N` entries: they put untracked files INTO the unstaged diff, and
// coverage used to demand each one in the plan or allow_unplanned. Since hc
// can never stage a file the plan does not reference, unplanned intent-to-add
// entries are now skipped from coverage with a warning.
func TestStaleIntentToAddSkippedFromCoverage(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)

	must(t, os.WriteFile(filepath.Join(dir, "real.txt"), []byte("a\nb\n"), 0o644))
	must(t, run(r, "add", "-A"))
	must(t, run(r, "commit", "-qm", "base"))

	// Simulate a crashed tool / editor plugin leaving intent-to-add entries.
	for _, p := range []string{"scratch1.md", "scratch2.md"} {
		must(t, os.WriteFile(filepath.Join(dir, p), []byte("wip\n"), 0o644))
		must(t, run(r, "add", "-N", "--", p))
	}
	must(t, os.WriteFile(filepath.Join(dir, "real.txt"), []byte("a\nCHANGED\n"), 0o644))

	res, acErr := runPlan([]byte(`{"commits":[{"message":"fix: real","files":[{"path":"real.txt","hunks":[0]}]}]}`), r, false)
	if acErr != nil {
		t.Fatalf("stale intent-to-add entries must not block the plan: %v", acErr)
	}
	result := res.(*output.Result)
	if result.Committed != 1 {
		t.Fatalf("expected 1 commit, got %+v", result)
	}
	if len(result.Warnings) != 2 {
		t.Fatalf("expected 2 skip warnings, got %v", result.Warnings)
	}
	for _, w := range result.Warnings {
		if !strings.Contains(w, "intent-to-add") {
			t.Errorf("warning should explain the skip: %q", w)
		}
	}

	// The scratch files stay uncommitted (still intent-to-add in the index)
	// and the real change is the only thing committed.
	show, _ := r.Run("show", "--stat", "--format=", "HEAD")
	if strings.Contains(show, "scratch1.md") || !strings.Contains(show, "real.txt") {
		t.Fatalf("commit should contain only real.txt:\n%s", show)
	}

	// A plan that DOES reference an intent-to-add file still commits it.
	_, acErr = runPlan([]byte(`{"commits":[{"message":"docs: keep scratch1","files":[{"path":"scratch1.md"}]}]}`), r, false)
	if acErr != nil {
		t.Fatalf("explicitly planned intent-to-add file must still commit: %v", acErr)
	}
	if err := run(r, "ls-files", "--error-unmatch", "scratch1.md"); err != nil {
		t.Error("scratch1.md should now be committed")
	}
}
