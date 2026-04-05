package patch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deligoez/ac/internal/diff"
	"github.com/deligoez/ac/internal/git"
)

// initRepo creates a temp git repo, commits an initial file, and returns the runner and cleanup.
func initRepo(t *testing.T) *git.Runner {
	t.Helper()
	dir := t.TempDir()
	r := git.NewRunner(dir)
	if _, err := r.Run("init"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run("config", "user.email", "test@test.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run("config", "user.name", "Test"); err != nil {
		t.Fatal(err)
	}
	return r
}

// writeFile writes content to a file in the repo directory.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Test 31: Apply valid patch succeeds
func TestApply_ValidPatch(t *testing.T) {
	r := initRepo(t)

	// Create and commit a file
	writeFile(t, r.Dir, "hello.txt", "line1\nline2\nline3\n")
	r.Run("add", "hello.txt")
	r.Run("commit", "-m", "init")

	// Build a patch that changes line2 -> line2_modified
	file := diff.FileDiff{
		Path:    "hello.txt",
		OldMode: "100644",
		NewMode: "100644",
	}
	hunks := []diff.Hunk{
		{Index: 0, OldStart: 2, OldCount: 1, NewStart: 2, NewCount: 1, Lines: []diff.Line{
			{Op: diff.OpDelete, Content: "line2"},
			{Op: diff.OpAdd, Content: "line2_modified"},
		}},
	}

	p, err := BuildPatch(file, []int{0}, hunks)
	if err != nil {
		t.Fatal(err)
	}

	if err := Apply(r, p); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	// Verify the change is staged by reading the staged content
	out, err := r.Run("show", ":hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "line2_modified") {
		t.Errorf("expected line2_modified in staged content, got:\n%s", out)
	}
	if strings.Contains(out, "line2\n") {
		t.Errorf("old line2 should not be in staged content, got:\n%s", out)
	}
}

// Test 32: Apply patch with adjusted line numbers -- correct staging
func TestApply_AdjustedLineNumbers(t *testing.T) {
	r := initRepo(t)

	// Create a file with enough lines
	var lines []string
	for i := 1; i <= 60; i++ {
		lines = append(lines, fmt.Sprintf("line%d", i))
	}
	writeFile(t, r.Dir, "big.txt", strings.Join(lines, "\n")+"\n")
	r.Run("add", "big.txt")
	r.Run("commit", "-m", "init")

	// Two hunks, select only the second -- it needs adjustment
	file := diff.FileDiff{
		Path:    "big.txt",
		OldMode: "100644",
		NewMode: "100644",
	}
	allHunks := []diff.Hunk{
		// H0: delete 2 lines at line 5, add 5 lines (net +3) -- SKIP this
		{Index: 0, OldStart: 5, OldCount: 2, NewStart: 5, NewCount: 5, Lines: []diff.Line{
			{Op: diff.OpDelete, Content: "line5"},
			{Op: diff.OpDelete, Content: "line6"},
			{Op: diff.OpAdd, Content: "a"},
			{Op: diff.OpAdd, Content: "b"},
			{Op: diff.OpAdd, Content: "c"},
			{Op: diff.OpAdd, Content: "d"},
			{Op: diff.OpAdd, Content: "e"},
		}},
		// H1: change line 20 -> modified (same count)
		{Index: 1, OldStart: 20, OldCount: 1, NewStart: 23, NewCount: 1, Lines: []diff.Line{
			{Op: diff.OpDelete, Content: "line20"},
			{Op: diff.OpAdd, Content: "line20_modified"},
		}},
	}

	// Select only H1. Skipping H0: delta += 2-5 = -3. H1 adjusted: 23+(-3) = 20.
	p, err := BuildPatch(file, []int{1}, allHunks)
	if err != nil {
		t.Fatal(err)
	}

	// The patch should have @@ -20,1 +20,1 @@ which is correct for the original file
	if err := Apply(r, p); err != nil {
		t.Fatalf("Apply with adjusted line numbers failed: %v", err)
	}

	out, err := r.Run("show", ":big.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "line20_modified") {
		t.Errorf("expected line20_modified in staged content, got:\n%s", out)
	}
}

// Test 33: Apply patch for new file (after git add -N) -- correct staging
func TestApply_NewFile(t *testing.T) {
	r := initRepo(t)

	// Create an initial commit so HEAD exists
	writeFile(t, r.Dir, ".gitkeep", "")
	r.Run("add", ".gitkeep")
	r.Run("commit", "-m", "init")

	// Create a new file and intent-to-add
	writeFile(t, r.Dir, "new.txt", "hello\nworld\n")
	r.Run("add", "-N", "new.txt")

	file := diff.FileDiff{
		Path:    "new.txt",
		IsNew:   true,
		NewMode: "100644",
	}
	hunks := []diff.Hunk{
		{Index: 0, OldStart: 0, OldCount: 0, NewStart: 1, NewCount: 2, Lines: []diff.Line{
			{Op: diff.OpAdd, Content: "hello"},
			{Op: diff.OpAdd, Content: "world"},
		}},
	}

	p, err := BuildPatch(file, []int{0}, hunks)
	if err != nil {
		t.Fatal(err)
	}

	if err := Apply(r, p); err != nil {
		t.Fatalf("Apply new file patch failed: %v", err)
	}

	// Verify staged content
	out, err := r.Run("show", ":new.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "hello") || !strings.Contains(out, "world") {
		t.Errorf("expected new file content staged, got:\n%s", out)
	}
}

// Test 34: Apply invalid patch -> error captured from git stderr
func TestApply_InvalidPatch(t *testing.T) {
	r := initRepo(t)

	writeFile(t, r.Dir, "x.txt", "aaa\n")
	r.Run("add", "x.txt")
	r.Run("commit", "-m", "init")

	// Completely bogus patch data
	bogus := []byte("this is not a valid patch\n")
	err := Apply(r, bogus)
	if err == nil {
		t.Fatal("expected error for invalid patch, got nil")
	}
	// git stderr should be in the error message
	if !strings.Contains(err.Error(), "git") {
		t.Errorf("expected git error message, got: %v", err)
	}
}

// Test ApplyCheck dry-run
func TestApplyCheck_Valid(t *testing.T) {
	r := initRepo(t)

	writeFile(t, r.Dir, "check.txt", "aaa\nbbb\nccc\n")
	r.Run("add", "check.txt")
	r.Run("commit", "-m", "init")

	file := diff.FileDiff{
		Path:    "check.txt",
		OldMode: "100644",
		NewMode: "100644",
	}
	hunks := []diff.Hunk{
		{Index: 0, OldStart: 2, OldCount: 1, NewStart: 2, NewCount: 1, Lines: []diff.Line{
			{Op: diff.OpDelete, Content: "bbb"},
			{Op: diff.OpAdd, Content: "BBB"},
		}},
	}

	p, err := BuildPatch(file, []int{0}, hunks)
	if err != nil {
		t.Fatal(err)
	}

	// Dry-run should succeed without changing anything
	if err := ApplyCheck(r, p); err != nil {
		t.Fatalf("ApplyCheck failed: %v", err)
	}

	// Verify nothing was actually staged
	out, err := r.Run("show", ":check.txt")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "BBB") {
		t.Error("ApplyCheck should not stage changes")
	}
}
