package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/deligoez/hc/internal/git"
	"github.com/deligoez/hc/internal/output"
)

// initRepo creates a git repo in dir with an initial commit.
func initRepo(t *testing.T, dir string) *git.Runner {
	t.Helper()
	r := git.NewRunner(dir)
	must(t, run(r, "init"))
	must(t, run(r, "config", "user.email", "test@test.com"))
	must(t, run(r, "config", "user.name", "Test"))
	// Create initial commit so HEAD exists
	initial := filepath.Join(dir, ".gitkeep")
	if err := os.WriteFile(initial, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	must(t, run(r, "add", "."))
	must(t, run(r, "commit", "-m", "initial"))
	return r
}

func run(r *git.Runner, args ...string) error {
	_, err := r.Run(args...)
	return err
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// Test 68: Modified file -- hunks indexed correctly
func TestDiffModifiedFile(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)

	// Create and commit a file
	f := filepath.Join(dir, "main.go")
	if err := os.WriteFile(f, []byte("line1\nline2\nline3\nline4\nline5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	must(t, run(r, "add", "main.go"))
	must(t, run(r, "commit", "-m", "add main.go"))

	// Modify the file in two separate regions
	if err := os.WriteFile(f, []byte("CHANGED1\nline2\nline3\nline4\nCHANGED5\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := runDiff(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Find the main.go file
	var found *diffFileResult
	for i := range result.Files {
		if result.Files[i].Path == "main.go" {
			found = &result.Files[i]
			break
		}
	}
	if found == nil {
		t.Fatal("main.go not found in diff output")
	}

	if len(found.Hunks) != 2 {
		t.Fatalf("expected 2 hunks, got %d", len(found.Hunks))
	}
	if found.Hunks[0].Index != 0 {
		t.Errorf("first hunk index = %d, want 0", found.Hunks[0].Index)
	}
	if found.Hunks[1].Index != 1 {
		t.Errorf("second hunk index = %d, want 1", found.Hunks[1].Index)
	}
	// Verify fingerprints are set
	if found.Hunks[0].Fingerprint == "" {
		t.Error("first hunk fingerprint is empty")
	}
	if found.Hunks[1].Fingerprint == "" {
		t.Error("second hunk fingerprint is empty")
	}
}

// Test 69: New file -- marked as is_new
func TestDiffNewFile(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)

	// Create a new file (untracked)
	f := filepath.Join(dir, "new.go")
	if err := os.WriteFile(f, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := runDiff(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found *diffFileResult
	for i := range result.Files {
		if result.Files[i].Path == "new.go" {
			found = &result.Files[i]
			break
		}
	}
	if found == nil {
		t.Fatal("new.go not found in diff output")
	}
	if !found.IsNew {
		t.Error("expected IsNew=true for untracked file")
	}
	if !found.IsUntracked {
		t.Error("expected IsUntracked=true for untracked file")
	}
}

// Test 70: Deleted file -- marked as is_deleted
func TestDiffDeletedFile(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)

	// Create, commit, then delete a file
	f := filepath.Join(dir, "remove.go")
	if err := os.WriteFile(f, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	must(t, run(r, "add", "remove.go"))
	must(t, run(r, "commit", "-m", "add remove.go"))

	// Delete the file
	if err := os.Remove(f); err != nil {
		t.Fatal(err)
	}

	result, err := runDiff(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found *diffFileResult
	for i := range result.Files {
		if result.Files[i].Path == "remove.go" {
			found = &result.Files[i]
			break
		}
	}
	if found == nil {
		t.Fatal("remove.go not found in diff output")
	}
	if !found.IsDeleted {
		t.Error("expected IsDeleted=true")
	}
}

// Test 71: Binary file -- reported without hunks
func TestDiffBinaryFile(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)

	// Create and commit a binary file
	f := filepath.Join(dir, "image.png")
	binaryData := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00}
	if err := os.WriteFile(f, binaryData, 0o644); err != nil {
		t.Fatal(err)
	}
	must(t, run(r, "add", "image.png"))
	must(t, run(r, "commit", "-m", "add image.png"))

	// Modify the binary file
	binaryData2 := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0xFF, 0xFE}
	if err := os.WriteFile(f, binaryData2, 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := runDiff(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found *diffFileResult
	for i := range result.Files {
		if result.Files[i].Path == "image.png" {
			found = &result.Files[i]
			break
		}
	}
	if found == nil {
		t.Fatal("image.png not found in diff output")
	}
	if !found.IsBinary {
		t.Error("expected IsBinary=true")
	}
	if len(found.Hunks) != 0 {
		t.Errorf("expected 0 hunks for binary file, got %d", len(found.Hunks))
	}
}

// Test 72: Multiple files -- all listed with correct indices
func TestDiffMultipleFiles(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)

	// Create and commit multiple files
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		f := filepath.Join(dir, name)
		if err := os.WriteFile(f, []byte("package main\nfunc init() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must(t, run(r, "add", "."))
	must(t, run(r, "commit", "-m", "add files"))

	// Modify all three
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		f := filepath.Join(dir, name)
		if err := os.WriteFile(f, []byte("package main\nfunc init() { /* modified */ }\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := runDiff(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have at least 3 files
	diffFileCount := 0
	for _, f := range result.Files {
		if f.Path == "a.go" || f.Path == "b.go" || f.Path == "c.go" {
			diffFileCount++
			// Each file should have hunks with index 0
			if len(f.Hunks) == 0 {
				t.Errorf("file %s has no hunks", f.Path)
				continue
			}
			if f.Hunks[0].Index != 0 {
				t.Errorf("file %s first hunk index = %d, want 0", f.Path, f.Hunks[0].Index)
			}
		}
	}
	if diffFileCount != 3 {
		t.Errorf("expected 3 modified files, found %d", diffFileCount)
	}
}

// Test 73: JSON output -- valid JSON with summary object
func TestDiffJSONOutput(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)

	// Create, commit, then modify a file
	f := filepath.Join(dir, "handler.go")
	if err := os.WriteFile(f, []byte("line1\nline2\nline3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	must(t, run(r, "add", "handler.go"))
	must(t, run(r, "commit", "-m", "add handler.go"))

	if err := os.WriteFile(f, []byte("line1\nMODIFIED\nline3\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := runDiff(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Build JSON output manually (same as printDiffJSON but to a buffer)
	out := diffOutputJSON{
		Files: make([]diffFileJSON, 0, len(result.Files)),
	}
	for _, fd := range result.Files {
		jf := diffFileJSON{
			Path:        fd.Path,
			Hunks:       make([]diffHunkJSON, 0, len(fd.Hunks)),
			IsNew:       fd.IsNew,
			IsDeleted:   fd.IsDeleted,
			IsRenamed:   fd.IsRenamed,
			OldPath:     fd.OldPath,
			IsBinary:    fd.IsBinary,
			IsUntracked: fd.IsUntracked,
		}
		for _, h := range fd.Hunks {
			jf.Hunks = append(jf.Hunks, diffHunkJSON{
				Index:   h.Index,
				Header:  hunkHeader(h),
				Added:   h.NewCount,
				Deleted: h.OldCount,
			})
		}
		out.Files = append(out.Files, jf)
		out.Summary.Files++
		out.Summary.Hunks += len(fd.Hunks)
		for _, h := range fd.Hunks {
			out.Summary.Added += h.NewCount
			out.Summary.Deleted += h.OldCount
		}
	}

	data, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("failed to marshal JSON: %v", err)
	}

	// Verify it's valid JSON by unmarshalling
	var parsed diffOutputJSON
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}

	// Verify summary
	if parsed.Summary.Files == 0 {
		t.Error("summary.files should be > 0")
	}
	if parsed.Summary.Hunks == 0 {
		t.Error("summary.hunks should be > 0")
	}

	// Verify handler.go is present
	foundHandler := false
	for _, f := range parsed.Files {
		if f.Path == "handler.go" {
			foundHandler = true
			if len(f.Hunks) == 0 {
				t.Error("handler.go should have hunks")
			}
			for _, h := range f.Hunks {
				if h.Header == "" {
					t.Error("hunk header should not be empty")
				}
			}
		}
	}
	if !foundHandler {
		t.Error("handler.go not found in JSON output")
	}
}

// Bug regression: ac diff should detect not-a-git-repo with clean error
func TestDiffNotAGitRepo(t *testing.T) {
	dir := t.TempDir() // not a git repo
	r := git.NewRunner(dir)

	_, err := runDiff(r)
	if err == nil {
		t.Fatal("expected error for non-git directory")
	}

	acErr, ok := err.(*output.ACError)
	if !ok {
		t.Fatalf("expected ACError, got %T: %v", err, err)
	}
	if acErr.Code != 2 {
		t.Errorf("expected exit code 2, got %d", acErr.Code)
	}
	if acErr.Message != "not a git repository" {
		t.Errorf("unexpected error: %s", acErr.Message)
	}
}
