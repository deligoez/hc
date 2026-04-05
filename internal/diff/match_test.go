package diff

import (
	"testing"
)

// helper to build a hunk with the given lines and OldStart.
func mkHunk(oldStart int64, lines []Line) Hunk {
	h := Hunk{OldStart: oldStart, Lines: lines}
	h.Fingerprint = Fingerprint(h)
	return h
}

func TestMatchHunks_AllPresent(t *testing.T) {
	// Test 18: All hunks present -- 1:1 mapping (same fingerprints, same count).
	linesA := []Line{{Op: OpDelete, Content: "old A"}, {Op: OpAdd, Content: "new A"}}
	linesB := []Line{{Op: OpDelete, Content: "old B"}, {Op: OpAdd, Content: "new B"}}
	linesC := []Line{{Op: OpDelete, Content: "old C"}, {Op: OpAdd, Content: "new C"}}

	original := []Hunk{mkHunk(10, linesA), mkHunk(20, linesB), mkHunk(30, linesC)}
	current := []Hunk{mkHunk(10, linesA), mkHunk(20, linesB), mkHunk(30, linesC)}

	m, err := MatchHunks(original, current)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m) != 3 {
		t.Fatalf("expected 3 mappings, got %d", len(m))
	}
	for i := 0; i < 3; i++ {
		if m[i] != i {
			t.Errorf("expected original[%d] -> current[%d], got current[%d]", i, i, m[i])
		}
	}
}

func TestMatchHunks_SomeCommitted(t *testing.T) {
	// Test 19: Some hunks committed (fewer in current) -- remaining matched by fingerprint.
	linesA := []Line{{Op: OpDelete, Content: "old A"}, {Op: OpAdd, Content: "new A"}}
	linesB := []Line{{Op: OpDelete, Content: "old B"}, {Op: OpAdd, Content: "new B"}}
	linesC := []Line{{Op: OpDelete, Content: "old C"}, {Op: OpAdd, Content: "new C"}}

	original := []Hunk{mkHunk(10, linesA), mkHunk(30, linesC)}
	// Current still has all three; original only references A and C.
	current := []Hunk{mkHunk(10, linesA), mkHunk(20, linesB), mkHunk(30, linesC)}

	m, err := MatchHunks(original, current)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m) != 2 {
		t.Fatalf("expected 2 mappings, got %d", len(m))
	}
	if m[0] != 0 {
		t.Errorf("expected original[0] -> current[0], got current[%d]", m[0])
	}
	if m[1] != 2 {
		t.Errorf("expected original[1] -> current[2], got current[%d]", m[1])
	}
}

func TestMatchHunks_IdenticalContentDisambiguation(t *testing.T) {
	// Test 20: Identical content hunks -- disambiguated by old_start proximity.
	lines := []Line{{Op: OpDelete, Content: "dup"}, {Op: OpAdd, Content: "dup new"}}

	// Two original hunks with identical content at lines 10 and 50.
	original := []Hunk{mkHunk(10, lines), mkHunk(50, lines)}

	// Three current hunks with the same content at lines 12, 48, 100.
	current := []Hunk{mkHunk(12, lines), mkHunk(48, lines), mkHunk(100, lines)}

	m, err := MatchHunks(original, current)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m) != 2 {
		t.Fatalf("expected 2 mappings, got %d", len(m))
	}
	// original[0] (OldStart=10) should match current[0] (OldStart=12, dist=2).
	if m[0] != 0 {
		t.Errorf("expected original[0] -> current[0], got current[%d]", m[0])
	}
	// original[1] (OldStart=50) should match current[1] (OldStart=48, dist=2)
	// since current[0] was already taken.
	if m[1] != 1 {
		t.Errorf("expected original[1] -> current[1], got current[%d]", m[1])
	}
}

func TestMatchHunks_NoMatchError(t *testing.T) {
	// Test 21: No matching hunk found -- error with descriptive message.
	linesA := []Line{{Op: OpDelete, Content: "old A"}, {Op: OpAdd, Content: "new A"}}
	linesB := []Line{{Op: OpDelete, Content: "old B"}, {Op: OpAdd, Content: "new B"}}

	original := []Hunk{mkHunk(10, linesA), mkHunk(20, linesB)}
	// Current only has hunk A; hunk B is missing.
	current := []Hunk{mkHunk(10, linesA)}

	_, err := MatchHunks(original, current)
	if err == nil {
		t.Fatal("expected error for missing hunk, got nil")
	}
	t.Logf("got expected error: %v", err)
}
