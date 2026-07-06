package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const newFileGoTests = `package m

import "testing"

func TestCreate(t *testing.T) {
	if 1 != 1 {
		t.Fatal("a")
	}
}

func TestUpdate(t *testing.T) {
	if 2 != 2 {
		t.Fatal("b")
	}
}

func TestDelete(t *testing.T) {
	if 3 != 3 {
		t.Fatal("c")
	}
}
`

// TestNewFileLogSplitsPerTestFunction verifies that hc log expands a brand-new
// test file's single whole-file hunk into one synthetic hunk per test
// function, with the preamble riding on the first one.
func TestNewFileLogSplitsPerTestFunction(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)
	must(t, os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("s\n"), 0o644))
	must(t, run(r, "add", "-A"))
	must(t, run(r, "commit", "-qm", "base"))
	must(t, os.WriteFile(filepath.Join(dir, "order_test.go"), []byte(newFileGoTests), 0o644))
	must(t, run(r, "add", "-A"))
	must(t, run(r, "commit", "-qm", "test: add order tests"))

	result, acErr := runLog(r, "HEAD~1..HEAD", false)
	if acErr != nil {
		t.Fatalf("runLog: %v", acErr)
	}
	hunks := result.Commits[0].Files[0].Hunks
	if len(hunks) != 3 {
		t.Fatalf("want 3 per-test hunks, got %d: %+v", len(hunks), hunks)
	}
	for i, want := range []string{"TestCreate", "TestUpdate", "TestDelete"} {
		if !strings.Contains(hunks[i].Section, want) {
			t.Errorf("hunk %d section = %q, want %s", i, hunks[i].Section, want)
		}
	}
	if !strings.Contains(hunks[0].Content, "package m") {
		t.Errorf("preamble should ride with the first test, content:\n%s", hunks[0].Content)
	}
	if hunks[0].Added+hunks[1].Added+hunks[2].Added != 21 {
		t.Errorf("expanded hunks must cover all 21 lines, got %d+%d+%d",
			hunks[0].Added, hunks[1].Added, hunks[2].Added)
	}
}
