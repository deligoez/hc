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
