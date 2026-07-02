package diff

import "testing"

func delHunk(oldStart int64, lines ...string) Hunk {
	h := Hunk{OldStart: oldStart}
	for _, l := range lines {
		h.Lines = append(h.Lines, Line{Op: OpDelete, Content: l})
	}
	return h
}

// TestMatchHunksRotatedDeletion reproduces git sliding an ambiguous hunk
// window across repeated content: deleting one of two identical blocks can
// be reported starting mid-block, rotating the line order. The multiset
// fallback must still match it.
func TestMatchHunksRotatedDeletion(t *testing.T) {
	original := []Hunk{delHunk(10, "l1", "l2", "l3", "l4", "l5", "l6", "l7")}
	// Same 7 lines, rotated: git chose a window starting 5 lines later.
	current := []Hunk{delHunk(15, "l6", "l7", "l1", "l2", "l3", "l4", "l5")}

	m, err := MatchHunks(original, current)
	if err != nil {
		t.Fatalf("rotated deletion should match via multiset fallback: %v", err)
	}
	if m[0] != 0 {
		t.Errorf("match map = %v, want {0:0}", m)
	}
}

func TestEqualContentMultiset(t *testing.T) {
	a := delHunk(10, "x", "y", "x")
	b := delHunk(20, "x", "x", "y")
	if !EqualContentMultiset(a, b) {
		t.Error("same lines with same counts should be multiset-equal")
	}

	c := delHunk(20, "x", "y", "y")
	if EqualContentMultiset(a, c) {
		t.Error("different counts should not be multiset-equal")
	}
}

func TestIsMultisetSubset(t *testing.T) {
	if !isMultisetSubset(nil, []string{"a"}) {
		t.Error("empty needle is always a subset")
	}
	if !isMultisetSubset([]string{"a", "a"}, []string{"a", "b", "a"}) {
		t.Error("counts satisfied should be subset")
	}
	if isMultisetSubset([]string{"a", "a"}, []string{"a", "b"}) {
		t.Error("insufficient count should not be subset")
	}
}
