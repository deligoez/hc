package patch

import (
	"strings"
	"testing"

	"github.com/deligoez/ac/internal/diff"
)

// fourHunks returns the worked example from spec Section 3.7:
// H0(@@ -5,2 +5,5 @@), H1(@@ -20,3 +23,0 @@), H2(@@ -30,1 +27,4 @@), H3(@@ -50,0 +53,2 @@)
func fourHunks() []diff.Hunk {
	return []diff.Hunk{
		{Index: 0, OldStart: 5, OldCount: 2, NewStart: 5, NewCount: 5, Lines: []diff.Line{
			{Op: diff.OpDelete, Content: "old1"},
			{Op: diff.OpDelete, Content: "old2"},
			{Op: diff.OpAdd, Content: "new1"},
			{Op: diff.OpAdd, Content: "new2"},
			{Op: diff.OpAdd, Content: "new3"},
			{Op: diff.OpAdd, Content: "new4"},
			{Op: diff.OpAdd, Content: "new5"},
		}},
		{Index: 1, OldStart: 20, OldCount: 3, NewStart: 23, NewCount: 0, Lines: []diff.Line{
			{Op: diff.OpDelete, Content: "d1"},
			{Op: diff.OpDelete, Content: "d2"},
			{Op: diff.OpDelete, Content: "d3"},
		}},
		{Index: 2, OldStart: 30, OldCount: 1, NewStart: 27, NewCount: 4, Lines: []diff.Line{
			{Op: diff.OpDelete, Content: "x"},
			{Op: diff.OpAdd, Content: "a"},
			{Op: diff.OpAdd, Content: "b"},
			{Op: diff.OpAdd, Content: "c"},
			{Op: diff.OpAdd, Content: "d"},
		}},
		{Index: 3, OldStart: 50, OldCount: 0, NewStart: 53, NewCount: 2, Lines: []diff.Line{
			{Op: diff.OpAdd, Content: "e1"},
			{Op: diff.OpAdd, Content: "e2"},
		}},
	}
}

func baseFile() diff.FileDiff {
	return diff.FileDiff{
		Path:    "foo.go",
		OldMode: "100644",
		NewMode: "100644",
	}
}

// Test 22: Select all hunks -- no adjustment needed, delta stays 0
func TestBuildPatch_SelectAll(t *testing.T) {
	hunks := fourHunks()
	file := baseFile()
	file.Hunks = hunks

	patch, err := BuildPatch(file, []int{0, 1, 2, 3}, hunks)
	if err != nil {
		t.Fatal(err)
	}
	s := string(patch)

	// All hunks should appear with their original new_start values
	expect := []string{
		"@@ -5,2 +5,5 @@",
		"@@ -20,3 +23,0 @@",
		"@@ -30,1 +27,4 @@",
		"@@ -50,0 +53,2 @@",
	}
	for _, e := range expect {
		if !strings.Contains(s, e) {
			t.Errorf("expected %q in patch, got:\n%s", e, s)
		}
	}
}

// Test 23: Select first hunk only -- no adjustment
func TestBuildPatch_SelectFirst(t *testing.T) {
	hunks := fourHunks()
	file := baseFile()
	file.Hunks = hunks

	patch, err := BuildPatch(file, []int{0}, hunks)
	if err != nil {
		t.Fatal(err)
	}
	s := string(patch)

	if !strings.Contains(s, "@@ -5,2 +5,5 @@") {
		t.Errorf("expected H0 unchanged, got:\n%s", s)
	}
	// Should not contain other hunks
	if strings.Contains(s, "@@ -20") || strings.Contains(s, "@@ -30") || strings.Contains(s, "@@ -50") {
		t.Errorf("should not contain skipped hunks, got:\n%s", s)
	}
}

// Test 24: Select last hunk only -- delta accumulated from all skipped
func TestBuildPatch_SelectLast(t *testing.T) {
	hunks := fourHunks()
	file := baseFile()
	file.Hunks = hunks

	patch, err := BuildPatch(file, []int{3}, hunks)
	if err != nil {
		t.Fatal(err)
	}
	s := string(patch)

	// Delta from skipping H0: 2-5=-3, H1: 3-0=3, H2: 1-4=-3 => total delta = -3
	// H3 adjusted: 53 + (-3) = 50
	if !strings.Contains(s, "@@ -50,0 +50,2 @@") {
		t.Errorf("expected H3 at +50, got:\n%s", s)
	}
}

// Test 25: Select [0,2] from 4 hunks -- hunk 2 adjusted
func TestBuildPatch_Select02(t *testing.T) {
	hunks := fourHunks()
	file := baseFile()
	file.Hunks = hunks

	patch, err := BuildPatch(file, []int{0, 2}, hunks)
	if err != nil {
		t.Fatal(err)
	}
	s := string(patch)

	// H0 selected first, no delta yet: @@ -5,2 +5,5 @@
	if !strings.Contains(s, "@@ -5,2 +5,5 @@") {
		t.Errorf("expected H0 at +5, got:\n%s", s)
	}

	// After H0 (selected), delta = 0. Then skip H1: delta += 3-0 = 3.
	// H2 adjusted: 27 + 3 = 30
	if !strings.Contains(s, "@@ -30,1 +30,4 @@") {
		t.Errorf("expected H2 at +30, got:\n%s", s)
	}
}

// Test 26: Select [1,3] from 4 hunks -- both adjusted
func TestBuildPatch_Select13(t *testing.T) {
	hunks := fourHunks()
	file := baseFile()
	file.Hunks = hunks

	patch, err := BuildPatch(file, []int{1, 3}, hunks)
	if err != nil {
		t.Fatal(err)
	}
	s := string(patch)

	// Skip H0: delta += 2-5 = -3
	// H1 adjusted: 23 + (-3) = 20
	if !strings.Contains(s, "@@ -20,3 +20,0 @@") {
		t.Errorf("expected H1 at +20, got:\n%s", s)
	}

	// After H1 (selected), delta still -3. Skip H2: delta += 1-4 = -3, total = -6.
	// H3 adjusted: 53 + (-6) = 47
	if !strings.Contains(s, "@@ -50,0 +47,2 @@") {
		t.Errorf("expected H3 at +47, got:\n%s", s)
	}
}

// Test: new file header
func TestBuildPatch_NewFile(t *testing.T) {
	file := diff.FileDiff{
		Path:    "new.go",
		IsNew:   true,
		NewMode: "100644",
		Hunks: []diff.Hunk{
			{Index: 0, OldStart: 0, OldCount: 0, NewStart: 1, NewCount: 1, Lines: []diff.Line{
				{Op: diff.OpAdd, Content: "package main"},
			}},
		},
	}
	patch, err := BuildPatch(file, []int{0}, file.Hunks)
	if err != nil {
		t.Fatal(err)
	}
	s := string(patch)
	if !strings.Contains(s, "new file mode 100644") {
		t.Errorf("missing new file mode, got:\n%s", s)
	}
	if !strings.Contains(s, "--- /dev/null") {
		t.Errorf("missing /dev/null for old, got:\n%s", s)
	}
	if !strings.Contains(s, "+++ b/new.go") {
		t.Errorf("missing +++ header, got:\n%s", s)
	}
}

// Test: deleted file header
func TestBuildPatch_DeletedFile(t *testing.T) {
	file := diff.FileDiff{
		Path:      "old.go",
		IsDeleted: true,
		OldMode:   "100644",
		Hunks: []diff.Hunk{
			{Index: 0, OldStart: 1, OldCount: 1, NewStart: 0, NewCount: 0, Lines: []diff.Line{
				{Op: diff.OpDelete, Content: "package main"},
			}},
		},
	}
	patch, err := BuildPatch(file, []int{0}, file.Hunks)
	if err != nil {
		t.Fatal(err)
	}
	s := string(patch)
	if !strings.Contains(s, "deleted file mode 100644") {
		t.Errorf("missing deleted file mode, got:\n%s", s)
	}
	if !strings.Contains(s, "+++ /dev/null") {
		t.Errorf("missing /dev/null for new, got:\n%s", s)
	}
}

// Test: rename header
func TestBuildPatch_Rename(t *testing.T) {
	file := diff.FileDiff{
		Path:      "new_name.go",
		OldPath:   "old_name.go",
		IsRenamed: true,
		OldMode:   "100644",
		NewMode:   "100644",
		Hunks: []diff.Hunk{
			{Index: 0, OldStart: 1, OldCount: 1, NewStart: 1, NewCount: 1, Lines: []diff.Line{
				{Op: diff.OpDelete, Content: "old"},
				{Op: diff.OpAdd, Content: "new"},
			}},
		},
	}
	patch, err := BuildPatch(file, []int{0}, file.Hunks)
	if err != nil {
		t.Fatal(err)
	}
	s := string(patch)
	if !strings.Contains(s, "diff --git a/old_name.go b/new_name.go") {
		t.Errorf("expected rename in diff header, got:\n%s", s)
	}
	if !strings.Contains(s, "rename from old_name.go") {
		t.Errorf("missing rename from, got:\n%s", s)
	}
	if !strings.Contains(s, "rename to new_name.go") {
		t.Errorf("missing rename to, got:\n%s", s)
	}
	if !strings.Contains(s, "--- a/old_name.go") {
		t.Errorf("expected old path in --- line, got:\n%s", s)
	}
}

// Test 27: Skip hunk that adds lines (new_count > old_count) -- delta negative
func TestBuildPatch_SkipAdditionHunk_DeltaNegative(t *testing.T) {
	hunks := []diff.Hunk{
		{Index: 0, OldStart: 10, OldCount: 2, NewStart: 10, NewCount: 5, Lines: []diff.Line{
			{Op: diff.OpDelete, Content: "a"},
			{Op: diff.OpDelete, Content: "b"},
			{Op: diff.OpAdd, Content: "x"},
			{Op: diff.OpAdd, Content: "y"},
			{Op: diff.OpAdd, Content: "z"},
			{Op: diff.OpAdd, Content: "w"},
			{Op: diff.OpAdd, Content: "v"},
		}},
		{Index: 1, OldStart: 20, OldCount: 1, NewStart: 23, NewCount: 1, Lines: []diff.Line{
			{Op: diff.OpDelete, Content: "old"},
			{Op: diff.OpAdd, Content: "new"},
		}},
	}
	file := baseFile()
	file.Hunks = hunks

	// Skip H0 (OldCount=2, NewCount=5 -> delta += 2-5 = -3), select H1
	patch, err := BuildPatch(file, []int{1}, hunks)
	if err != nil {
		t.Fatal(err)
	}
	s := string(patch)

	// H1 adjusted: 23 + (-3) = 20
	if !strings.Contains(s, "@@ -20,1 +20,1 @@") {
		t.Errorf("expected H1 at +20 after negative delta, got:\n%s", s)
	}
	// H0 should not appear
	if strings.Contains(s, "@@ -10") {
		t.Errorf("skipped hunk H0 should not appear, got:\n%s", s)
	}
}

// Test 28: Skip hunk that deletes lines (old_count > new_count) -- delta positive
func TestBuildPatch_SkipDeletionHunk_DeltaPositive(t *testing.T) {
	hunks := []diff.Hunk{
		{Index: 0, OldStart: 10, OldCount: 5, NewStart: 10, NewCount: 2, Lines: []diff.Line{
			{Op: diff.OpDelete, Content: "a"},
			{Op: diff.OpDelete, Content: "b"},
			{Op: diff.OpDelete, Content: "c"},
			{Op: diff.OpDelete, Content: "d"},
			{Op: diff.OpDelete, Content: "e"},
			{Op: diff.OpAdd, Content: "x"},
			{Op: diff.OpAdd, Content: "y"},
		}},
		{Index: 1, OldStart: 30, OldCount: 1, NewStart: 27, NewCount: 1, Lines: []diff.Line{
			{Op: diff.OpDelete, Content: "old"},
			{Op: diff.OpAdd, Content: "new"},
		}},
	}
	file := baseFile()
	file.Hunks = hunks

	// Skip H0 (OldCount=5, NewCount=2 -> delta += 5-2 = 3), select H1
	patch, err := BuildPatch(file, []int{1}, hunks)
	if err != nil {
		t.Fatal(err)
	}
	s := string(patch)

	// H1 adjusted: 27 + 3 = 30
	if !strings.Contains(s, "@@ -30,1 +30,1 @@") {
		t.Errorf("expected H1 at +30 after positive delta, got:\n%s", s)
	}
	// H0 should not appear
	if strings.Contains(s, "@@ -10") {
		t.Errorf("skipped hunk H0 should not appear, got:\n%s", s)
	}
}

// Test 29: Skip hunk with equal old/new count -- delta unchanged
func TestBuildPatch_SkipEqualCountHunk_DeltaZero(t *testing.T) {
	hunks := []diff.Hunk{
		{Index: 0, OldStart: 10, OldCount: 3, NewStart: 10, NewCount: 3, Lines: []diff.Line{
			{Op: diff.OpDelete, Content: "a"},
			{Op: diff.OpDelete, Content: "b"},
			{Op: diff.OpDelete, Content: "c"},
			{Op: diff.OpAdd, Content: "x"},
			{Op: diff.OpAdd, Content: "y"},
			{Op: diff.OpAdd, Content: "z"},
		}},
		{Index: 1, OldStart: 25, OldCount: 2, NewStart: 25, NewCount: 2, Lines: []diff.Line{
			{Op: diff.OpDelete, Content: "old1"},
			{Op: diff.OpDelete, Content: "old2"},
			{Op: diff.OpAdd, Content: "new1"},
			{Op: diff.OpAdd, Content: "new2"},
		}},
	}
	file := baseFile()
	file.Hunks = hunks

	// Skip H0 (OldCount=3, NewCount=3 -> delta += 3-3 = 0), select H1
	patch, err := BuildPatch(file, []int{1}, hunks)
	if err != nil {
		t.Fatal(err)
	}
	s := string(patch)

	// H1 adjusted: 25 + 0 = 25 (unchanged)
	if !strings.Contains(s, "@@ -25,2 +25,2 @@") {
		t.Errorf("expected H1 at +25 (unchanged delta), got:\n%s", s)
	}
	// H0 should not appear
	if strings.Contains(s, "@@ -10") {
		t.Errorf("skipped hunk H0 should not appear, got:\n%s", s)
	}
}

// Test: BuildPatch emits "no newline" marker for lines without trailing \n
func TestBuildPatch_NoNewlineAtEndOfFile(t *testing.T) {
	hunks := []diff.Hunk{
		{Index: 0, OldStart: 11, OldCount: 1, NewStart: 11, NewCount: 1, Lines: []diff.Line{
			{Op: diff.OpDelete, Content: "func Handle5() { return }"},
			{Op: diff.OpAdd, Content: "func Handle5() { fmt.Println(\"h5\") }"},
		}},
	}
	file := baseFile()
	file.Hunks = hunks

	p, err := BuildPatch(file, []int{0}, hunks)
	if err != nil {
		t.Fatal(err)
	}
	s := string(p)

	// Both delete and add lines lack trailing \n, so both should have the marker
	if !strings.Contains(s, "\\ No newline at end of file") {
		t.Errorf("expected 'No newline at end of file' marker, got:\n%s", s)
	}

	// Count occurrences -- should appear twice (after delete and after add)
	count := strings.Count(s, "\\ No newline at end of file")
	if count != 2 {
		t.Errorf("expected 2 'No newline' markers, got %d in:\n%s", count, s)
	}
}

// Test: BuildPatch does NOT emit "no newline" for lines WITH trailing \n
func TestBuildPatch_WithNewline(t *testing.T) {
	hunks := []diff.Hunk{
		{Index: 0, OldStart: 4, OldCount: 1, NewStart: 4, NewCount: 1, Lines: []diff.Line{
			{Op: diff.OpDelete, Content: "\t// old\n"},
			{Op: diff.OpAdd, Content: "\t// new\n"},
		}},
	}
	file := baseFile()
	file.Hunks = hunks

	p, err := BuildPatch(file, []int{0}, hunks)
	if err != nil {
		t.Fatal(err)
	}
	s := string(p)

	if strings.Contains(s, "No newline") {
		t.Errorf("should NOT contain 'No newline' marker for lines with \\n, got:\n%s", s)
	}
}

// Test 30: Single-line hunk -- count=1 handled correctly
func TestBuildPatch_SingleLineHunk(t *testing.T) {
	hunks := []diff.Hunk{
		{Index: 0, OldStart: 1, OldCount: 1, NewStart: 1, NewCount: 1, Lines: []diff.Line{
			{Op: diff.OpDelete, Content: "old line"},
			{Op: diff.OpAdd, Content: "new line"},
		}},
	}
	file := baseFile()
	file.Hunks = hunks

	patch, err := BuildPatch(file, []int{0}, hunks)
	if err != nil {
		t.Fatal(err)
	}
	s := string(patch)

	if !strings.Contains(s, "@@ -1,1 +1,1 @@") {
		t.Errorf("expected single-line hunk header @@ -1,1 +1,1 @@, got:\n%s", s)
	}
	if !strings.Contains(s, "-old line") {
		t.Errorf("expected delete line, got:\n%s", s)
	}
	if !strings.Contains(s, "+new line") {
		t.Errorf("expected add line, got:\n%s", s)
	}
}

// Test: IsMergedHunk returns false for exact fingerprint match
func TestIsMergedHunk_ExactMatch(t *testing.T) {
	h := diff.Hunk{
		Index: 0, OldStart: 10, OldCount: 1, NewStart: 10, NewCount: 2,
		Fingerprint: "abc123",
		Lines: []diff.Line{
			{Op: diff.OpDelete, Content: "old\n"},
			{Op: diff.OpAdd, Content: "new1\n"},
			{Op: diff.OpAdd, Content: "new2\n"},
		},
	}
	origs := []diff.Hunk{{Fingerprint: "abc123"}}
	if IsMergedHunk(h, origs) {
		t.Error("expected false for exact fingerprint match")
	}
}

// Test: IsMergedHunk returns true when fingerprints differ
func TestIsMergedHunk_Merged(t *testing.T) {
	h := diff.Hunk{
		Index: 0, OldStart: 10, OldCount: 2, NewStart: 10, NewCount: 4,
		Fingerprint: "merged_fp",
		Lines: []diff.Line{
			{Op: diff.OpDelete, Content: "old1\n"},
			{Op: diff.OpDelete, Content: "old2\n"},
			{Op: diff.OpAdd, Content: "new1\n"},
			{Op: diff.OpAdd, Content: "new2\n"},
			{Op: diff.OpAdd, Content: "new3\n"},
			{Op: diff.OpAdd, Content: "new4\n"},
		},
	}
	origs := []diff.Hunk{
		{Fingerprint: "orig_fp1"},
		{Fingerprint: "orig_fp2"},
	}
	if !IsMergedHunk(h, origs) {
		t.Error("expected true for merged hunk (different fingerprints)")
	}
}

// Test: BuildCompositePatch with a merged hunk extracts correct sub-lines
func TestBuildCompositePatch_MergedHunk(t *testing.T) {
	// Simulate a merged hunk containing lines from two original hunks.
	// The merged current hunk has: delete "A", add "B", "C", "D", "E"
	// Original hunk 0 (commit's hunk): delete "A", add "B", "C"
	// Original hunk 1 (other commit's hunk): add "D", "E"
	mergedHunk := diff.Hunk{
		Index: 0, OldStart: 10, OldCount: 1, NewStart: 10, NewCount: 5,
		Fingerprint: "merged_fp",
		Lines: []diff.Line{
			{Op: diff.OpDelete, Content: "A\n"},
			{Op: diff.OpAdd, Content: "B\n"},
			{Op: diff.OpAdd, Content: "C\n"},
			{Op: diff.OpAdd, Content: "D\n"},
			{Op: diff.OpAdd, Content: "E\n"},
		},
	}

	file := diff.FileDiff{
		Path:    "test.go",
		OldMode: "100644",
		NewMode: "100644",
		Hunks:   []diff.Hunk{mergedHunk},
	}

	// This commit only wants hunk 0's lines.
	origForThisCommit := []diff.Hunk{
		{
			Fingerprint: "orig_fp_0",
			Lines: []diff.Line{
				{Op: diff.OpDelete, Content: "A\n"},
				{Op: diff.OpAdd, Content: "B\n"},
				{Op: diff.OpAdd, Content: "C\n"},
			},
		},
	}

	currentToOrigs := map[int][]diff.Hunk{
		0: origForThisCommit,
	}

	patchBytes, err := BuildCompositePatch(file, []int{0}, file.Hunks, currentToOrigs)
	if err != nil {
		t.Fatal(err)
	}
	s := string(patchBytes)

	// Should have old_count=1 (one delete), new_count=2 (two adds: B, C)
	if !strings.Contains(s, "@@ -10,1 +10,2 @@") {
		t.Errorf("expected sub-hunk header @@ -10,1 +10,2 @@, got:\n%s", s)
	}

	// Should contain the delete and the two adds from orig hunk 0
	if !strings.Contains(s, "-A") {
		t.Errorf("expected delete line -A, got:\n%s", s)
	}
	if !strings.Contains(s, "+B") {
		t.Errorf("expected add line +B, got:\n%s", s)
	}
	if !strings.Contains(s, "+C") {
		t.Errorf("expected add line +C, got:\n%s", s)
	}

	// Should NOT contain the adds from the other original hunk
	if strings.Contains(s, "+D") {
		t.Errorf("should NOT contain +D (belongs to other commit), got:\n%s", s)
	}
	if strings.Contains(s, "+E") {
		t.Errorf("should NOT contain +E (belongs to other commit), got:\n%s", s)
	}
}

// Test: BuildCompositePatch with mixed exact and merged hunks
func TestBuildCompositePatch_MixedCase(t *testing.T) {
	// Hunk 0: exact match (not merged)
	exactHunk := diff.Hunk{
		Index: 0, OldStart: 5, OldCount: 1, NewStart: 5, NewCount: 1,
		Fingerprint: "exact_fp",
		Lines: []diff.Line{
			{Op: diff.OpDelete, Content: "old\n"},
			{Op: diff.OpAdd, Content: "new\n"},
		},
	}
	// Hunk 1: merged hunk
	mergedHunk := diff.Hunk{
		Index: 1, OldStart: 20, OldCount: 2, NewStart: 20, NewCount: 4,
		Fingerprint: "merged_fp",
		Lines: []diff.Line{
			{Op: diff.OpDelete, Content: "X\n"},
			{Op: diff.OpDelete, Content: "Y\n"},
			{Op: diff.OpAdd, Content: "A\n"},
			{Op: diff.OpAdd, Content: "B\n"},
			{Op: diff.OpAdd, Content: "C\n"},
			{Op: diff.OpAdd, Content: "D\n"},
		},
	}

	file := diff.FileDiff{
		Path:    "test.go",
		OldMode: "100644",
		NewMode: "100644",
		Hunks:   []diff.Hunk{exactHunk, mergedHunk},
	}

	currentToOrigs := map[int][]diff.Hunk{
		0: {{Fingerprint: "exact_fp"}}, // exact match
		1: {{
			Fingerprint: "orig_merged_fp",
			Lines: []diff.Line{
				{Op: diff.OpDelete, Content: "X\n"},
				{Op: diff.OpAdd, Content: "A\n"},
				{Op: diff.OpAdd, Content: "B\n"},
			},
		}},
	}

	patchBytes, err := BuildCompositePatch(file, []int{0, 1}, file.Hunks, currentToOrigs)
	if err != nil {
		t.Fatal(err)
	}
	s := string(patchBytes)

	// Exact hunk should be emitted in full
	if !strings.Contains(s, "@@ -5,1 +5,1 @@") {
		t.Errorf("expected exact hunk header, got:\n%s", s)
	}

	// Merged hunk should only have sub-lines: 1 delete (X), 2 adds (A, B)
	if !strings.Contains(s, "@@ -20,1 +20,2 @@") {
		t.Errorf("expected merged sub-hunk header @@ -20,1 +20,2 @@, got:\n%s", s)
	}

	// Should NOT contain +C or +D or -Y (those belong to other commit)
	if strings.Contains(s, "+C") || strings.Contains(s, "+D") || strings.Contains(s, "-Y") {
		t.Errorf("should not contain lines from other commit, got:\n%s", s)
	}
}
