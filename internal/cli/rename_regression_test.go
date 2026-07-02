package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRenameRequiresBothPaths guards the --no-renames decision: a rename must
// surface as old-path deletion + new-path addition, and coverage validation
// must reject a plan that covers only the new path. With -M rename detection
// the old-path deletion was silently dropped from coverage after git add -N
// paired the files at run time.
func TestRenameRequiresBothPaths(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)

	must(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\nthree\n"), 0o644))
	must(t, run(r, "add", "a.txt"))
	must(t, run(r, "commit", "-m", "add a"))

	// Simulate an unstaged rename: delete old, create new.
	must(t, os.Remove(filepath.Join(dir, "a.txt")))
	must(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("one\ntwo\nthree\n"), 0o644))

	// Plan covering only the new path must fail coverage validation.
	_, acErr := runPlan([]byte(`{"commits":[{"message":"move","files":[{"path":"b.txt"}]}]}`), r, false)
	if acErr == nil {
		t.Fatal("plan without the old-path deletion should fail coverage validation")
	}
	if acErr.Code != 2 || !strings.Contains(acErr.Message, "not in the plan") {
		t.Fatalf("want coverage error (code 2), got code=%d msg=%q", acErr.Code, acErr.Message)
	}
	if out, _ := r.Run("status", "--porcelain"); !strings.Contains(out, "b.txt") {
		t.Fatalf("failed validation must not consume the working tree state:\n%s", out)
	}

	// Plan covering both paths commits the full rename.
	_, acErr = runPlan([]byte(`{"commits":[{"message":"move a to b","files":[{"path":"a.txt"},{"path":"b.txt"}]}]}`), r, false)
	if acErr != nil {
		t.Fatalf("full rename plan failed: %v", acErr)
	}
	if out, _ := r.Run("status", "--porcelain"); strings.TrimSpace(out) != "" {
		t.Fatalf("tree not clean after rename commit:\n%s", out)
	}
	// git reconstructs the rename at display time.
	show, err := r.Run("show", "-M", "--stat", "HEAD")
	if err != nil || !strings.Contains(show, "a.txt => b.txt") {
		t.Errorf("git show -M should detect the rename, got:\n%s", show)
	}
}
