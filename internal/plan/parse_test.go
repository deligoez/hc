package plan

import (
	"testing"

	"github.com/deligoez/hc/internal/output"
)

// Test 35: Valid plan parsed correctly.
func TestParse_ValidPlan(t *testing.T) {
	data := []byte(`{
		"commits": [
			{
				"message": "feat: add login",
				"files": [
					{"path": "auth/login.go", "hunks": [1, 3]},
					{"path": "auth/login_test.go"}
				]
			}
		]
	}`)

	p, err := parseOrFail(t, data)
	if err != nil {
		return
	}

	if len(p.Commits) != 1 {
		t.Fatalf("expected 1 commit, got %d", len(p.Commits))
	}
	if p.Commits[0].Message != "feat: add login" {
		t.Fatalf("unexpected message: %s", p.Commits[0].Message)
	}
	if len(p.Commits[0].Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(p.Commits[0].Files))
	}
	f0 := p.Commits[0].Files[0]
	if f0.Path != "auth/login.go" || len(f0.Hunks) != 2 {
		t.Fatalf("unexpected first file: %+v", f0)
	}
}

// Test 36: Plan with full-file entries (no hunks) -- Hunks is nil.
func TestParse_FullFileNoHunks(t *testing.T) {
	data := []byte(`{
		"commits": [
			{
				"message": "docs: update readme",
				"files": [{"path": "README.md"}]
			}
		]
	}`)

	p, err := parseOrFail(t, data)
	if err != nil {
		return
	}

	if p.Commits[0].Files[0].Hunks != nil {
		t.Fatalf("expected nil Hunks for full-file entry, got %v", p.Commits[0].Files[0].Hunks)
	}
}

// Test 37: Plan with empty hunks array -- treated as full-file (same as nil).
func TestParse_EmptyHunksNormalized(t *testing.T) {
	data := []byte(`{
		"commits": [
			{
				"message": "chore: cleanup",
				"files": [{"path": "main.go", "hunks": []}]
			}
		]
	}`)

	p, err := parseOrFail(t, data)
	if err != nil {
		return
	}

	f := p.Commits[0].Files[0]
	if f.Hunks != nil {
		t.Fatalf("expected nil Hunks after normalization, got %v", f.Hunks)
	}
	if !f.IsFullFile() {
		t.Fatal("expected IsFullFile() to return true after normalization")
	}
}

// Test 38: Invalid JSON -> error with hint.
func TestParse_InvalidJSON(t *testing.T) {
	data := []byte(`{not valid json}`)

	_, err := Parse(data)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	assertHasHint(t, err)
}

// Test 39: Missing `commits` field -> error with hint.
func TestParse_MissingCommits(t *testing.T) {
	data := []byte(`{}`)

	_, err := Parse(data)
	if err == nil {
		t.Fatal("expected error for missing commits field")
	}
	assertHasHint(t, err)
}

// Test 40: Empty commits array -> error with hint.
func TestParse_EmptyCommits(t *testing.T) {
	data := []byte(`{"commits": []}`)

	_, err := Parse(data)
	if err == nil {
		t.Fatal("expected error for empty commits array")
	}
	assertHasHint(t, err)
}

// Test 41: Missing file `path` -> error with hint.
func TestParse_MissingFilePath(t *testing.T) {
	data := []byte(`{
		"commits": [
			{
				"message": "fix: something",
				"files": [{"hunks": [1]}]
			}
		]
	}`)

	_, err := Parse(data)
	if err == nil {
		t.Fatal("expected error for missing file path")
	}
	assertHasHint(t, err)
}

// --- helpers ---

func parseOrFail(t *testing.T, data []byte) (*Plan, error) {
	t.Helper()
	p, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse failed unexpectedly: %v", err)
	}
	return p, nil
}

func assertHasHint(t *testing.T, err error) {
	t.Helper()
	acErr, ok := err.(*output.ACError)
	if !ok {
		t.Fatalf("expected *output.ACError, got %T", err)
	}
	if acErr.Hint == "" {
		t.Fatal("expected non-empty hint on ACError")
	}
}
