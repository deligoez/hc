package diff

import (
	"testing"
)

// Test 1: Parse @@ -10,3 +10,5 @@ header -- correct old/new start/count
func TestParseHunkHeader(t *testing.T) {
	raw := `diff --git a/file.go b/file.go
index 1234567..abcdefg 100644
--- a/file.go
+++ b/file.go
@@ -10,3 +10,5 @@
-old line 1
-old line 2
-old line 3
+new line 1
+new line 2
+new line 3
+new line 4
+new line 5
`
	files, err := Parse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if len(files[0].Hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(files[0].Hunks))
	}
	h := files[0].Hunks[0]
	if h.OldStart != 10 {
		t.Errorf("OldStart = %d, want 10", h.OldStart)
	}
	if h.OldCount != 3 {
		t.Errorf("OldCount = %d, want 3", h.OldCount)
	}
	if h.NewStart != 10 {
		t.Errorf("NewStart = %d, want 10", h.NewStart)
	}
	if h.NewCount != 5 {
		t.Errorf("NewCount = %d, want 5", h.NewCount)
	}
	if h.Index != 0 {
		t.Errorf("Index = %d, want 0", h.Index)
	}
}

// Test 2: Parse @@ -5 +10 @@ header -- omitted count defaults to 1
func TestParseOmittedCount(t *testing.T) {
	raw := `diff --git a/file.go b/file.go
index 1234567..abcdefg 100644
--- a/file.go
+++ b/file.go
@@ -5 +10 @@
-old line
+new line
`
	files, err := Parse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	h := files[0].Hunks[0]
	if h.OldStart != 5 {
		t.Errorf("OldStart = %d, want 5", h.OldStart)
	}
	if h.OldCount != 1 {
		t.Errorf("OldCount = %d, want 1", h.OldCount)
	}
	if h.NewStart != 10 {
		t.Errorf("NewStart = %d, want 10", h.NewStart)
	}
	if h.NewCount != 1 {
		t.Errorf("NewCount = %d, want 1", h.NewCount)
	}
}

// Test 3: Parse @@ -15,0 +16,2 @@ header -- pure addition (0 old lines)
func TestParsePureAddition(t *testing.T) {
	raw := `diff --git a/file.go b/file.go
index 1234567..abcdefg 100644
--- a/file.go
+++ b/file.go
@@ -15,0 +16,2 @@
+added line 1
+added line 2
`
	files, err := Parse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	h := files[0].Hunks[0]
	if h.OldStart != 15 {
		t.Errorf("OldStart = %d, want 15", h.OldStart)
	}
	if h.OldCount != 0 {
		t.Errorf("OldCount = %d, want 0", h.OldCount)
	}
	if h.NewStart != 16 {
		t.Errorf("NewStart = %d, want 16", h.NewStart)
	}
	if h.NewCount != 2 {
		t.Errorf("NewCount = %d, want 2", h.NewCount)
	}
}

// Test 4: Parse @@ -15,3 +15,0 @@ header -- pure deletion (0 new lines)
func TestParsePureDeletion(t *testing.T) {
	raw := `diff --git a/file.go b/file.go
index 1234567..abcdefg 100644
--- a/file.go
+++ b/file.go
@@ -15,3 +15,0 @@
-deleted line 1
-deleted line 2
-deleted line 3
`
	files, err := Parse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	h := files[0].Hunks[0]
	if h.OldStart != 15 {
		t.Errorf("OldStart = %d, want 15", h.OldStart)
	}
	if h.OldCount != 3 {
		t.Errorf("OldCount = %d, want 3", h.OldCount)
	}
	if h.NewStart != 15 {
		t.Errorf("NewStart = %d, want 15", h.NewStart)
	}
	if h.NewCount != 0 {
		t.Errorf("NewCount = %d, want 0", h.NewCount)
	}
}

// Test 5: Parse multi-file diff -- correct file boundaries
func TestParseMultiFile(t *testing.T) {
	raw := `diff --git a/a.go b/a.go
index 1234567..abcdefg 100644
--- a/a.go
+++ b/a.go
@@ -1,2 +1,3 @@
-line 1
-line 2
+line 1 modified
+line 2 modified
+line 3 added
diff --git a/b.go b/b.go
index 1234567..abcdefg 100644
--- a/b.go
+++ b/b.go
@@ -5,1 +5,1 @@
-old b
+new b
`
	files, err := Parse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	if files[0].Path != "a.go" {
		t.Errorf("files[0].Path = %q, want %q", files[0].Path, "a.go")
	}
	if files[1].Path != "b.go" {
		t.Errorf("files[1].Path = %q, want %q", files[1].Path, "b.go")
	}
	if len(files[0].Hunks) != 1 {
		t.Errorf("files[0] hunks = %d, want 1", len(files[0].Hunks))
	}
	if len(files[1].Hunks) != 1 {
		t.Errorf("files[1] hunks = %d, want 1", len(files[1].Hunks))
	}
	// Verify hunk indices are per-file (0-based)
	if files[1].Hunks[0].Index != 0 {
		t.Errorf("files[1].Hunks[0].Index = %d, want 0", files[1].Hunks[0].Index)
	}
}

// Test 6: Parse new file diff (--- /dev/null) -- IsNew=true
func TestParseNewFile(t *testing.T) {
	raw := `diff --git a/newfile.go b/newfile.go
new file mode 100644
index 0000000..abcdefg
--- /dev/null
+++ b/newfile.go
@@ -0,0 +1,3 @@
+line 1
+line 2
+line 3
`
	files, err := Parse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if !files[0].IsNew {
		t.Error("expected IsNew=true")
	}
	if files[0].Path != "newfile.go" {
		t.Errorf("Path = %q, want %q", files[0].Path, "newfile.go")
	}
}

// Test 7: Parse deleted file diff (+++ /dev/null) -- IsDeleted=true
func TestParseDeletedFile(t *testing.T) {
	raw := `diff --git a/old.go b/old.go
deleted file mode 100644
index abcdefg..0000000
--- a/old.go
+++ /dev/null
@@ -1,3 +0,0 @@
-line 1
-line 2
-line 3
`
	files, err := Parse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if !files[0].IsDeleted {
		t.Error("expected IsDeleted=true")
	}
	if files[0].Path != "old.go" {
		t.Errorf("Path = %q, want %q", files[0].Path, "old.go")
	}
}

// Test 8: Parse binary file diff -- IsBinary flag set
func TestParseBinaryFile(t *testing.T) {
	raw := `diff --git a/image.png b/image.png
index 1234567..abcdefg 100644
Binary files a/image.png and b/image.png differ
`
	files, err := Parse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if !files[0].IsBinary {
		t.Error("expected IsBinary=true")
	}
	if len(files[0].Hunks) != 0 {
		t.Errorf("expected 0 hunks for binary file, got %d", len(files[0].Hunks))
	}
}

// Test 9: Parse rename diff -- old/new paths captured, IsRenamed=true
func TestParseRenameDiff(t *testing.T) {
	raw := `diff --git a/old_name.go b/new_name.go
similarity index 100%
rename from old_name.go
rename to new_name.go
`
	files, err := Parse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if !files[0].IsRenamed {
		t.Error("expected IsRenamed=true")
	}
	if files[0].OldPath != "old_name.go" {
		t.Errorf("OldPath = %q, want %q", files[0].OldPath, "old_name.go")
	}
	if files[0].Path != "new_name.go" {
		t.Errorf("Path = %q, want %q", files[0].Path, "new_name.go")
	}
}

// Test 10: Parse mode change diff -- OldMode/NewMode captured
func TestParseModeChange(t *testing.T) {
	raw := `diff --git a/script.sh b/script.sh
old mode 100644
new mode 100755
`
	files, err := Parse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].OldMode != "100644" {
		t.Errorf("OldMode = %q, want %q", files[0].OldMode, "100644")
	}
	if files[0].NewMode != "100755" {
		t.Errorf("NewMode = %q, want %q", files[0].NewMode, "100755")
	}
}

// Test 11: Handle "\ No newline at end of file" marker -- not counted in line counts
func TestParseNoNewlineMarker(t *testing.T) {
	raw := `diff --git a/file.go b/file.go
index 1234567..abcdefg 100644
--- a/file.go
+++ b/file.go
@@ -1,1 +1,1 @@
-old line
\ No newline at end of file
+new line
\ No newline at end of file
`
	files, err := Parse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	h := files[0].Hunks[0]
	// Should only have 2 lines (1 delete + 1 add), not 4
	lineCount := len(h.Lines)
	if lineCount != 2 {
		t.Errorf("expected 2 lines (no newline markers excluded), got %d", lineCount)
		for i, l := range h.Lines {
			t.Logf("  line[%d]: op=%d content=%q", i, l.Op, l.Content)
		}
	}
	// Verify the lines are the actual content
	if h.Lines[0].Op != OpDelete {
		t.Errorf("line[0].Op = %d, want OpDelete (%d)", h.Lines[0].Op, OpDelete)
	}
	if h.Lines[1].Op != OpAdd {
		t.Errorf("line[1].Op = %d, want OpAdd (%d)", h.Lines[1].Op, OpAdd)
	}
}

// Test: Fingerprint consistency -- same hunk content parsed from different diff
// positions must produce identical fingerprints.
func TestFingerprintConsistency(t *testing.T) {
	// Simulate: original diff has 5 hunks. After applying hunks 0-2,
	// re-diff produces hunks 3 and 4 with shifted line numbers.
	// The fingerprint of original hunk 3 must match the re-diffed hunk for the same content.

	// Original diff: 5 hunks, each deleting 1 line and adding 3 lines.
	origDiff := `diff --git a/handlers.go b/handlers.go
index 1234567..abcdefg 100644
--- a/handlers.go
+++ b/handlers.go
@@ -4 +4,3 @@
-	// h1 old
+	// h1 new1
+	// h1 new2
+	// h1 new3
@@ -8 +10,3 @@
-	// h2 old
+	// h2 new1
+	// h2 new2
+	// h2 new3
@@ -12 +16,3 @@
-	// h3 old
+	// h3 new1
+	// h3 new2
+	// h3 new3
@@ -16 +22,3 @@
-	// h4 old
+	// h4 new1
+	// h4 new2
+	// h4 new3
@@ -20 +28,3 @@
-	// h5 old
+	// h5 new1
+	// h5 new2
+	// h5 new3
`

	// After applying hunks 0-2, re-diff shows hunks 3 and 4 with shifted positions
	reDiff := `diff --git a/handlers.go b/handlers.go
index abcdefg..1234567 100644
--- a/handlers.go
+++ b/handlers.go
@@ -20 +22,3 @@
-	// h4 old
+	// h4 new1
+	// h4 new2
+	// h4 new3
@@ -24 +28,3 @@
-	// h5 old
+	// h5 new1
+	// h5 new2
+	// h5 new3
`

	origFiles, err := Parse(origDiff)
	if err != nil {
		t.Fatalf("parse original: %v", err)
	}
	reFiles, err := Parse(reDiff)
	if err != nil {
		t.Fatalf("parse re-diff: %v", err)
	}

	// Original hunk 3 (h4) should match re-diffed hunk 0
	origHunk3 := origFiles[0].Hunks[3]
	reHunk0 := reFiles[0].Hunks[0]

	origFP := Fingerprint(origHunk3)
	reFP := Fingerprint(reHunk0)

	if origFP != reFP {
		t.Errorf("fingerprint mismatch between original hunk 3 and re-diff hunk 0")
		t.Logf("original hunk 3 lines:")
		for i, l := range origHunk3.Lines {
			t.Logf("  [%d] op=%d content=%q", i, l.Op, l.Content)
		}
		t.Logf("re-diff hunk 0 lines:")
		for i, l := range reHunk0.Lines {
			t.Logf("  [%d] op=%d content=%q", i, l.Op, l.Content)
		}
	}

	// Original hunk 4 (h5) should match re-diffed hunk 1
	origHunk4 := origFiles[0].Hunks[4]
	reHunk1 := reFiles[0].Hunks[1]

	origFP4 := Fingerprint(origHunk4)
	reFP1 := Fingerprint(reHunk1)

	if origFP4 != reFP1 {
		t.Errorf("fingerprint mismatch between original hunk 4 and re-diff hunk 1")
		t.Logf("original hunk 4 lines:")
		for i, l := range origHunk4.Lines {
			t.Logf("  [%d] op=%d content=%q", i, l.Op, l.Content)
		}
		t.Logf("re-diff hunk 1 lines:")
		for i, l := range reHunk1.Lines {
			t.Logf("  [%d] op=%d content=%q", i, l.Op, l.Content)
		}
	}
}

// Test: Fingerprint consistency with "no newline at end of file"
func TestFingerprintConsistencyNoNewline(t *testing.T) {
	// When the last hunk in a diff is the LAST thing in the stream,
	// and there's a "no newline" marker, check fingerprint consistency.

	// Original diff: last hunk has "no newline" marker
	origDiff := "diff --git a/handlers.go b/handlers.go\nindex 1234567..abcdefg 100644\n--- a/handlers.go\n+++ b/handlers.go\n@@ -4 +4,3 @@\n-\t// h1 old\n+\t// h1 new1\n+\t// h1 new2\n+\t// h1 new3\n@@ -8 +10 @@\n-\t// h2 old\n\\ No newline at end of file\n+\t// h2 new\n\\ No newline at end of file\n"

	// Re-diff: same hunk is the ONLY hunk, still has "no newline"
	reDiff := "diff --git a/handlers.go b/handlers.go\nindex abcdefg..1234567 100644\n--- a/handlers.go\n+++ b/handlers.go\n@@ -8 +10 @@\n-\t// h2 old\n\\ No newline at end of file\n+\t// h2 new\n\\ No newline at end of file\n"

	origFiles, err := Parse(origDiff)
	if err != nil {
		t.Fatalf("parse original: %v", err)
	}
	reFiles, err := Parse(reDiff)
	if err != nil {
		t.Fatalf("parse re-diff: %v", err)
	}

	origHunk1 := origFiles[0].Hunks[1]
	reHunk0 := reFiles[0].Hunks[0]

	origFP := Fingerprint(origHunk1)
	reFP := Fingerprint(reHunk0)

	if origFP != reFP {
		t.Errorf("fingerprint mismatch with no-newline marker")
		t.Logf("original hunk 1 lines:")
		for i, l := range origHunk1.Lines {
			t.Logf("  [%d] op=%d content=%q", i, l.Op, l.Content)
		}
		t.Logf("re-diff hunk 0 lines:")
		for i, l := range reHunk0.Lines {
			t.Logf("  [%d] op=%d content=%q", i, l.Op, l.Content)
		}
	}
}

// Test: Line.Content trailing newline consistency across parse positions
func TestLineContentTrailingNewline(t *testing.T) {
	// Two diffs with identical hunk content, but the hunk appears at
	// different positions (middle vs end of diff stream).

	// Hunk at MIDDLE of diff (more hunks follow)
	middleDiff := `diff --git a/f.go b/f.go
index 1234567..abcdefg 100644
--- a/f.go
+++ b/f.go
@@ -4 +4 @@
-	old target
+	new target
@@ -10 +10 @@
-	old other
+	new other
`

	// Same hunk at END of diff (last hunk, nothing follows)
	endDiff := `diff --git a/f.go b/f.go
index 1234567..abcdefg 100644
--- a/f.go
+++ b/f.go
@@ -4 +4 @@
-	old target
+	new target
`

	middleFiles, err := Parse(middleDiff)
	if err != nil {
		t.Fatalf("parse middle: %v", err)
	}
	endFiles, err := Parse(endDiff)
	if err != nil {
		t.Fatalf("parse end: %v", err)
	}

	mHunk := middleFiles[0].Hunks[0]
	eHunk := endFiles[0].Hunks[0]

	// Check that line content is identical regardless of position
	if len(mHunk.Lines) != len(eHunk.Lines) {
		t.Fatalf("line count mismatch: middle=%d, end=%d", len(mHunk.Lines), len(eHunk.Lines))
	}

	for i := range mHunk.Lines {
		if mHunk.Lines[i].Content != eHunk.Lines[i].Content {
			t.Errorf("line %d content differs: middle=%q end=%q", i, mHunk.Lines[i].Content, eHunk.Lines[i].Content)
		}
	}

	mFP := Fingerprint(mHunk)
	eFP := Fingerprint(eHunk)
	if mFP != eFP {
		t.Errorf("fingerprint mismatch: middle=%s end=%s", mFP[:16], eFP[:16])
	}
}

// Test 12: Handle empty diff -- returns empty slice, no error
func TestParseEmptyDiff(t *testing.T) {
	files, err := Parse("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if files == nil {
		t.Fatal("expected non-nil slice")
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files, got %d", len(files))
	}

	// Also test whitespace-only
	files, err = Parse("   \n\n  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files for whitespace-only input, got %d", len(files))
	}
}
