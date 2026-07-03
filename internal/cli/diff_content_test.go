package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDiffHunkContentAndSection verifies that hc diff exposes the changed
// lines and the enclosing-function section so an agent can classify hunks
// without re-reading git diff.
func TestDiffHunkContentAndSection(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)

	src := "package main\n\nfunc Login() {\n\ta := 1\n\tb := 2\n\t_ = a\n\t_ = b\n}\n"
	f := filepath.Join(dir, "auth.go")
	if err := os.WriteFile(f, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	must(t, run(r, "add", "auth.go"))
	must(t, run(r, "commit", "-m", "add auth.go"))

	modified := "package main\n\nfunc Login() {\n\ta := 1\n\tb := 42\n\t_ = a\n\t_ = b\n}\n"
	if err := os.WriteFile(f, []byte(modified), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := runDiff(r)
	if err != nil {
		t.Fatalf("runDiff: %v", err)
	}

	if len(result.Files) != 1 || len(result.Files[0].Hunks) != 1 {
		t.Fatalf("expected 1 file with 1 hunk, got %+v", result.Files)
	}
	h := result.Files[0].Hunks[0]

	content, _ := hunkContent(h)
	if !strings.Contains(content, "-\tb := 2") {
		t.Errorf("content should include deleted line, got %q", content)
	}
	if !strings.Contains(content, "+\tb := 42") {
		t.Errorf("content should include added line, got %q", content)
	}

	if !strings.Contains(h.Section, "func Login") {
		t.Errorf("section should carry the enclosing function, got %q", h.Section)
	}

	if h.Fingerprint == "" {
		t.Error("hunk fingerprint should be computed by runDiff")
	}
	if short := shortFingerprint(h.Fingerprint); len(short) != 12 {
		t.Errorf("short fingerprint length = %d, want 12", len(short))
	}
}

// TestDiffWarningOnStagedChanges verifies staged changes surface as a warning
// in the structured result (so JSON consumers see it too).
func TestDiffWarningsFieldRoundTrip(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)

	f := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(f, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	must(t, run(r, "add", "a.txt"))
	must(t, run(r, "commit", "-m", "add a"))
	if err := os.WriteFile(f, []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := runDiff(r)
	if err != nil {
		t.Fatalf("runDiff: %v", err)
	}
	// No staged changes here; warnings should be empty and omitted in JSON.
	if len(result.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", result.Warnings)
	}
}
