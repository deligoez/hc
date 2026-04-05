package plan

import (
	"strings"
	"testing"

	"github.com/deligoez/ac/internal/diff"
	"github.com/deligoez/ac/internal/output"
)

// --- Field Validation Tests ---

// Test 42: Negative hunk index -- error with hint.
func TestValidateFields_NegativeHunkIndex(t *testing.T) {
	p := &Plan{
		Commits: []Commit{
			{
				Message: "fix: something",
				Files: []FileEntry{
					{Path: "foo.go", Hunks: []int{-1}},
				},
			},
		},
	}

	err := ValidateFields(p)
	if err == nil {
		t.Fatal("expected error for negative hunk index")
	}
	assertACError(t, err, "hunk index -1 is invalid for foo.go")
	assertHasHint(t, err)
}

// Test 43: Duplicate hunk index in same commit -- error with hint.
func TestValidateFields_DuplicateHunkInSameFile(t *testing.T) {
	p := &Plan{
		Commits: []Commit{
			{
				Message: "fix: something",
				Files: []FileEntry{
					{Path: "foo.go", Hunks: []int{0, 1, 0}},
				},
			},
		},
	}

	err := ValidateFields(p)
	if err == nil {
		t.Fatal("expected error for duplicate hunk in same file entry")
	}
	assertACError(t, err, "hunk index 0 duplicated in foo.go (commit 0)")
	assertHasHint(t, err)
}

// Test 44: Same hunk assigned to multiple commits -- error with hint.
func TestValidateFields_SameHunkInMultipleCommits(t *testing.T) {
	p := &Plan{
		Commits: []Commit{
			{
				Message: "feat: part 1",
				Files: []FileEntry{
					{Path: "foo.go", Hunks: []int{0, 1}},
				},
			},
			{
				Message: "feat: part 2",
				Files: []FileEntry{
					{Path: "foo.go", Hunks: []int{1, 2}},
				},
			},
		},
	}

	err := ValidateFields(p)
	if err == nil {
		t.Fatal("expected error for same hunk in multiple commits")
	}
	assertACError(t, err, "hunk 1 of foo.go assigned to both commit 0 and commit 1")
	assertHasHint(t, err)
}

// Test: Full-file in multiple commits -- error.
func TestValidateFields_FullFileMultipleCommits(t *testing.T) {
	p := &Plan{
		Commits: []Commit{
			{
				Message: "feat: first",
				Files:   []FileEntry{{Path: "foo.go"}},
			},
			{
				Message: "feat: second",
				Files:   []FileEntry{{Path: "bar.go"}},
			},
			{
				Message: "feat: third",
				Files:   []FileEntry{{Path: "foo.go"}},
			},
		},
	}

	err := ValidateFields(p)
	if err == nil {
		t.Fatal("expected error for full-file in multiple commits")
	}
	assertACError(t, err, "foo.go appears in full-file mode in commits 0 and 2")
	assertHasHint(t, err)
}

// Test: Mixed full-file/hunk-select for same file -- error.
func TestValidateFields_MixedModes(t *testing.T) {
	p := &Plan{
		Commits: []Commit{
			{
				Message: "feat: first",
				Files:   []FileEntry{{Path: "foo.go"}},
			},
			{
				Message: "feat: second",
				Files:   []FileEntry{{Path: "foo.go", Hunks: []int{0}}},
			},
		},
	}

	err := ValidateFields(p)
	if err == nil {
		t.Fatal("expected error for mixed modes")
	}
	assertACError(t, err, "foo.go uses full-file mode in commit 0 and hunk-select in commit 1")
	assertHasHint(t, err)
}

// Test: Empty commit message in ValidateFields.
func TestValidateFields_EmptyMessage(t *testing.T) {
	p := &Plan{
		Commits: []Commit{
			{
				Message: "",
				Files:   []FileEntry{{Path: "foo.go"}},
			},
		},
	}

	err := ValidateFields(p)
	if err == nil {
		t.Fatal("expected error for empty message")
	}
	assertACError(t, err, "commit 0 has empty message")
}

// Test: Empty files array in ValidateFields.
func TestValidateFields_EmptyFiles(t *testing.T) {
	p := &Plan{
		Commits: []Commit{
			{
				Message: "fix: something",
				Files:   []FileEntry{},
			},
		},
	}

	err := ValidateFields(p)
	if err == nil {
		t.Fatal("expected error for empty files")
	}
	assertACError(t, err, "commit 0 has no files")
}

// Test: Path safety -- absolute path.
func TestValidateFields_AbsolutePath(t *testing.T) {
	p := &Plan{
		Commits: []Commit{
			{
				Message: "fix: something",
				Files:   []FileEntry{{Path: "/etc/foo"}},
			},
		},
	}

	err := ValidateFields(p)
	if err == nil {
		t.Fatal("expected error for absolute path")
	}
	assertACError(t, err, "must be relative to repo root")
}

// Test: Path safety -- ".." traversal.
func TestValidateFields_PathTraversal(t *testing.T) {
	p := &Plan{
		Commits: []Commit{
			{
				Message: "fix: something",
				Files:   []FileEntry{{Path: "../../etc/passwd"}},
			},
		},
	}

	err := ValidateFields(p)
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
	assertACError(t, err, "contains \"..\" traversal")
}

// Test: Valid plan passes field validation.
func TestValidateFields_Valid(t *testing.T) {
	p := &Plan{
		Commits: []Commit{
			{
				Message: "feat: add login",
				Files: []FileEntry{
					{Path: "auth/login.go", Hunks: []int{0, 1}},
					{Path: "auth/login_test.go"},
				},
			},
			{
				Message: "feat: add logout",
				Files: []FileEntry{
					{Path: "auth/login.go", Hunks: []int{2}},
				},
			},
		},
	}

	if err := ValidateFields(p); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

// --- Coverage Validation Tests ---

func makeDiffFiles() []diff.FileDiff {
	return []diff.FileDiff{
		{
			Path: "auth/login.go",
			Hunks: []diff.Hunk{
				{Index: 0},
				{Index: 1},
				{Index: 2},
			},
		},
		{
			Path: "auth/login_test.go",
			Hunks: []diff.Hunk{
				{Index: 0},
			},
		},
	}
}

// Test 45: All files and hunks assigned -- valid.
func TestValidateCoverage_AllAssigned(t *testing.T) {
	p := &Plan{
		Commits: []Commit{
			{
				Message: "feat: add login",
				Files: []FileEntry{
					{Path: "auth/login.go", Hunks: []int{0, 1}},
					{Path: "auth/login_test.go"},
				},
			},
			{
				Message: "feat: add logout",
				Files: []FileEntry{
					{Path: "auth/login.go", Hunks: []int{2}},
				},
			},
		},
	}

	err := ValidateCoverage(p, makeDiffFiles())
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

// Test 46: File not in diff -- error with file name.
func TestValidateCoverage_FileNotInDiff(t *testing.T) {
	p := &Plan{
		Commits: []Commit{
			{
				Message: "fix: something",
				Files: []FileEntry{
					{Path: "nonexistent.go", Hunks: []int{0}},
				},
			},
		},
	}

	err := ValidateCoverage(p, makeDiffFiles())
	if err == nil {
		t.Fatal("expected error for file not in diff")
	}
	assertACError(t, err, "nonexistent.go")
	assertACError(t, err, "has no changes in the working tree")
	assertHasHint(t, err)
}

// Test 47: Hunk index out of range -- error with available range.
func TestValidateCoverage_HunkOutOfRange(t *testing.T) {
	p := &Plan{
		Commits: []Commit{
			{
				Message: "fix: something",
				Files: []FileEntry{
					{Path: "auth/login.go", Hunks: []int{0, 5}},
					{Path: "auth/login_test.go"},
				},
			},
		},
	}

	err := ValidateCoverage(p, makeDiffFiles())
	if err == nil {
		t.Fatal("expected error for hunk out of range")
	}
	assertACError(t, err, "hunk index 5 out of range for auth/login.go (has 3 hunks, indices 0-2)")
	assertHasHint(t, err)
}

// Test 48: Binary file with hunks -- error with hint.
func TestValidateCoverage_BinaryWithHunks(t *testing.T) {
	files := []diff.FileDiff{
		{
			Path:     "logo.png",
			IsBinary: true,
		},
	}

	p := &Plan{
		Commits: []Commit{
			{
				Message: "fix: update logo",
				Files: []FileEntry{
					{Path: "logo.png", Hunks: []int{0}},
				},
			},
		},
	}

	err := ValidateCoverage(p, files)
	if err == nil {
		t.Fatal("expected error for binary file with hunks")
	}
	assertACError(t, err, "logo.png is a binary file and cannot be split into hunks")
	assertHasHint(t, err)
}

// Test 49: Untracked new file without hunks -- valid (full-file mode).
func TestValidateCoverage_UntrackedFullFile(t *testing.T) {
	files := []diff.FileDiff{
		{
			Path:  "new_file.go",
			IsNew: true,
			Hunks: []diff.Hunk{{Index: 0}},
		},
	}

	p := &Plan{
		Commits: []Commit{
			{
				Message: "feat: add new file",
				Files:   []FileEntry{{Path: "new_file.go"}},
			},
		},
	}

	err := ValidateCoverage(p, files)
	if err != nil {
		t.Fatalf("expected no error for untracked full-file, got: %v", err)
	}
}

// Test 50: Untracked new file with hunks -- valid after intent-to-add.
func TestValidateCoverage_UntrackedWithHunks(t *testing.T) {
	// After intent-to-add, the file appears in the diff with hunks.
	files := []diff.FileDiff{
		{
			Path:  "new_file.go",
			IsNew: true,
			Hunks: []diff.Hunk{{Index: 0}, {Index: 1}},
		},
	}

	p := &Plan{
		Commits: []Commit{
			{
				Message: "feat: add part 1",
				Files:   []FileEntry{{Path: "new_file.go", Hunks: []int{0}}},
			},
			{
				Message: "feat: add part 2",
				Files:   []FileEntry{{Path: "new_file.go", Hunks: []int{1}}},
			},
		},
	}

	err := ValidateCoverage(p, files)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

// Test 51: Missing file (in diff but not in plan) -- error: incomplete coverage.
func TestValidateCoverage_MissingFile(t *testing.T) {
	// Plan only covers one of two diff files.
	p := &Plan{
		Commits: []Commit{
			{
				Message: "feat: add login",
				Files:   []FileEntry{{Path: "auth/login.go"}},
			},
		},
	}

	err := ValidateCoverage(p, makeDiffFiles())
	if err == nil {
		t.Fatal("expected error for missing file in plan")
	}
	assertACError(t, err, "auth/login_test.go has changes but is not in the plan")
	assertHasHint(t, err)
}

// Test 52: Missing hunks (some hunks unassigned) -- error: incomplete coverage.
func TestValidateCoverage_MissingHunks(t *testing.T) {
	// Plan only assigns hunk 0 of auth/login.go, missing hunks 1 and 2.
	p := &Plan{
		Commits: []Commit{
			{
				Message: "feat: add login",
				Files: []FileEntry{
					{Path: "auth/login.go", Hunks: []int{0}},
					{Path: "auth/login_test.go"},
				},
			},
		},
	}

	err := ValidateCoverage(p, makeDiffFiles())
	if err == nil {
		t.Fatal("expected error for missing hunks")
	}
	assertACError(t, err, "auth/login.go hunks [1, 2] not assigned to any commit")
	assertHasHint(t, err)
}

// Test 53: File in allow_unplanned -- excluded from coverage check.
func TestValidateCoverage_AllowUnplannedExact(t *testing.T) {
	p := &Plan{
		Commits: []Commit{
			{
				Message: "feat: add login",
				Files:   []FileEntry{{Path: "auth/login.go"}},
			},
		},
		AllowUnplanned: []string{"auth/login_test.go"},
	}

	err := ValidateCoverage(p, makeDiffFiles())
	if err != nil {
		t.Fatalf("expected no error with allow_unplanned, got: %v", err)
	}
}

// Test 54: Glob pattern in allow_unplanned -- matches correctly.
func TestValidateCoverage_AllowUnplannedGlob(t *testing.T) {
	files := []diff.FileDiff{
		{
			Path:  "auth/login.go",
			Hunks: []diff.Hunk{{Index: 0}},
		},
		{
			Path:  "auth/login_test.go",
			Hunks: []diff.Hunk{{Index: 0}},
		},
		{
			Path:  "auth/logout_test.go",
			Hunks: []diff.Hunk{{Index: 0}},
		},
	}

	p := &Plan{
		Commits: []Commit{
			{
				Message: "feat: add login",
				Files:   []FileEntry{{Path: "auth/login.go"}},
			},
		},
		AllowUnplanned: []string{"auth/*_test.go"},
	}

	err := ValidateCoverage(p, files)
	if err != nil {
		t.Fatalf("expected no error with glob allow_unplanned, got: %v", err)
	}
}

// Test 55: Duplicate hunk across commits -- error: duplicate assignment.
func TestValidateCoverage_DuplicateHunkAcrossCommits(t *testing.T) {
	files := []diff.FileDiff{
		{
			Path: "foo.go",
			Hunks: []diff.Hunk{
				{Index: 0},
				{Index: 1},
				{Index: 2},
			},
		},
	}

	p := &Plan{
		Commits: []Commit{
			{
				Message: "feat: part 1",
				Files:   []FileEntry{{Path: "foo.go", Hunks: []int{0, 1}}},
			},
			{
				Message: "feat: part 2",
				Files:   []FileEntry{{Path: "foo.go", Hunks: []int{1, 2}}},
			},
		},
	}

	err := ValidateCoverage(p, files)
	if err == nil {
		t.Fatal("expected error for duplicate hunk across commits")
	}
	assertACError(t, err, "hunk 1 of foo.go assigned to both commit 0 and commit 1")
	assertHasHint(t, err)
}

// --- helpers ---

func assertACError(t *testing.T, err error, substr string) {
	t.Helper()
	acErr, ok := err.(*output.ACError)
	if !ok {
		t.Fatalf("expected *output.ACError, got %T: %v", err, err)
	}
	if !strings.Contains(acErr.Message, substr) {
		t.Fatalf("expected error message to contain %q, got: %s", substr, acErr.Message)
	}
}
