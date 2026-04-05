package diff

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

// Test 13: Same content, different line numbers -> same fingerprint.
func TestFingerprint_SameContentDifferentLineNumbers(t *testing.T) {
	lines := []Line{
		{Op: OpDelete, Content: "old line"},
		{Op: OpAdd, Content: "new line"},
	}

	h1 := Hunk{OldStart: 1, OldCount: 1, NewStart: 1, NewCount: 1, Lines: lines}
	h2 := Hunk{OldStart: 100, OldCount: 1, NewStart: 200, NewCount: 1, Lines: lines}

	f1 := Fingerprint(h1)
	f2 := Fingerprint(h2)

	if f1 != f2 {
		t.Fatalf("expected same fingerprint for same content at different positions, got %s vs %s", f1, f2)
	}
}

// Test 14: Different content -> different fingerprint.
func TestFingerprint_DifferentContent(t *testing.T) {
	h1 := Hunk{Lines: []Line{
		{Op: OpAdd, Content: "alpha"},
	}}
	h2 := Hunk{Lines: []Line{
		{Op: OpAdd, Content: "beta"},
	}}

	if Fingerprint(h1) == Fingerprint(h2) {
		t.Fatal("expected different fingerprints for different content")
	}
}

// Test 15: Same additions, different deletions -> different fingerprint.
func TestFingerprint_SameAddsDifferentDeletes(t *testing.T) {
	h1 := Hunk{Lines: []Line{
		{Op: OpDelete, Content: "old A"},
		{Op: OpAdd, Content: "new line"},
	}}
	h2 := Hunk{Lines: []Line{
		{Op: OpDelete, Content: "old B"},
		{Op: OpAdd, Content: "new line"},
	}}

	if Fingerprint(h1) == Fingerprint(h2) {
		t.Fatal("expected different fingerprints when deletions differ")
	}
}

// Test 16: Order-dependent -- shuffled lines produce DIFFERENT fingerprint.
func TestFingerprint_OrderDependent(t *testing.T) {
	h1 := Hunk{Lines: []Line{
		{Op: OpAdd, Content: "first"},
		{Op: OpAdd, Content: "second"},
	}}
	h2 := Hunk{Lines: []Line{
		{Op: OpAdd, Content: "second"},
		{Op: OpAdd, Content: "first"},
	}}

	if Fingerprint(h1) == Fingerprint(h2) {
		t.Fatal("expected different fingerprints for shuffled line order")
	}
}

// Test 17: Empty hunk -> stable fingerprint (hash of just "||").
func TestFingerprint_EmptyHunk(t *testing.T) {
	h := Hunk{}
	got := Fingerprint(h)

	expected := fmt.Sprintf("%x", sha256.Sum256([]byte("||")))
	if got != expected {
		t.Fatalf("expected fingerprint of empty hunk to be hash of %q, got %s vs %s", "||", got, expected)
	}
}
