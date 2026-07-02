package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestResplitOverRepeatedContent guards the content-reconstruction staging
// design. With repetitive file content, git can decompose the SAME remaining
// change into different hunk boundaries on a re-diff after an earlier commit
// (e.g. +[A,B,C]@33 +[A]@35 becomes +[A,B]@33 +[C,A]@35). Any strategy that
// re-matches original hunks against re-diffed hunks fails there; rebuilding
// staged content from original diff coordinates does not.
//
// Distilled from property-fuzz seed 34.
func TestResplitOverRepeatedContent(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)

	tok := []string{"tok0", "tok1", "tok2", "tok3", "tok4"}
	var base []string
	for i := 0; i < 60; i++ {
		base = append(base, tok[i%5])
	}
	f := filepath.Join(dir, "f.txt")
	must(t, os.WriteFile(f, []byte(strings.Join(base, "\n")+"\n"), 0o644))
	must(t, run(r, "add", "f.txt"))
	must(t, run(r, "commit", "-m", "base"))

	// Insert ambiguous content near repeated tokens at two close positions,
	// plus one far-away insertion committed FIRST so the near hunks get
	// re-diffed (and potentially re-split) during later commits.
	var mut []string
	mut = append(mut, base[:33]...)
	mut = append(mut, "tok4", "tok0", "tok1") // hunk 0 (after old line 33)
	mut = append(mut, base[33:35]...)
	mut = append(mut, "tok4") // hunk 1 (after old line 35)
	mut = append(mut, base[35:53]...)
	mut = append(mut, "tok3", "tok2") // hunk 2 (after old line 53)
	mut = append(mut, base[53:]...)
	must(t, os.WriteFile(f, []byte(strings.Join(mut, "\n")+"\n"), 0o644))

	_, acErr := runPlan([]byte(`{"commits":[
		{"message":"far","files":[{"path":"f.txt","hunks":[2]}]},
		{"message":"near a","files":[{"path":"f.txt","hunks":[0]}]},
		{"message":"near b","files":[{"path":"f.txt","hunks":[1]}]}]}`), r, false)
	if acErr != nil {
		t.Fatalf("re-split scenario must succeed with reconstruction staging: %v", acErr)
	}

	if out, _ := r.Run("status", "--porcelain"); strings.TrimSpace(out) != "" {
		t.Fatalf("tree not clean:\n%s", out)
	}
	got, err := os.ReadFile(f)
	must(t, err)
	if string(got) != strings.Join(mut, "\n")+"\n" {
		t.Fatal("final content corrupted")
	}
	count, _ := r.Run("rev-list", "--count", "HEAD")
	if strings.TrimSpace(count) != "5" { // initial + base + 3 plan commits
		t.Fatalf("commit count = %s, want 5", strings.TrimSpace(count))
	}
}

// TestRotatedDuplicateBlockDeletion guards the same design against git
// sliding a deletion window across duplicate blocks (rotated line order).
func TestRotatedDuplicateBlockDeletion(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)

	base := "A\nB\nC\nD\nA\nB\nC\nD\n1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n"
	f := filepath.Join(dir, "f.txt")
	must(t, os.WriteFile(f, []byte(base), 0o644))
	must(t, run(r, "add", "f.txt"))
	must(t, run(r, "commit", "-m", "base"))

	// Delete one duplicate block AND edit a line further down.
	mut := "A\nB\nC\nD\n1\n2\n3\n4\nEDITED\n6\n7\n8\n9\n10\n"
	must(t, os.WriteFile(f, []byte(mut), 0o644))

	// Commit the edit FIRST so the deletion is re-staged afterwards.
	_, acErr := runPlan([]byte(`{"commits":[
		{"message":"edit","files":[{"path":"f.txt","hunks":[1]}]},
		{"message":"dedup","files":[{"path":"f.txt","hunks":[0]}]}]}`), r, false)
	if acErr != nil {
		t.Fatalf("rotated duplicate-block deletion must succeed: %v", acErr)
	}
	got, err := os.ReadFile(f)
	must(t, err)
	if string(got) != mut {
		t.Fatal("final content corrupted")
	}
	if out, _ := r.Run("status", "--porcelain"); strings.TrimSpace(out) != "" {
		t.Fatalf("tree not clean:\n%s", out)
	}
}
