package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/deligoez/ac/internal/diff"
	"github.com/deligoez/ac/internal/git"
	"github.com/deligoez/ac/internal/patch"
)

func TestDebugHunkMatching(t *testing.T) {
	dir, r := setupRepo(t)

	base := "package main\n\nfunc handleLogin() {}\n\nfunc handleLogout() {}\n\nfunc handleRefresh() {}\n\nfunc handleRevoke() {}\n\nfunc handleStatus() {}\n"
	os.WriteFile(filepath.Join(dir, "handlers.go"), []byte(base), 0644)
	must(t, run(r, "add", "handlers.go"))
	must(t, run(r, "commit", "-m", "base"))

	modified := "package main\n\nimport \"fmt\"\n\nfunc handleLogin() {\n\tfmt.Println(\"login handler\")\n}\n\nfunc handleLogout() {\n\tfmt.Println(\"logout handler\")\n}\n\nfunc handleRefresh() {\n\tfmt.Println(\"refresh handler\")\n}\n\nfunc handleRevoke() {\n\tfmt.Println(\"revoke handler\")\n}\n\nfunc handleStatus() {\n\tfmt.Println(\"status handler\")\n}\n"
	os.WriteFile(filepath.Join(dir, "handlers.go"), []byte(modified), 0644)

	// Parse original diff
	rawDiff, _ := r.Diff("-U0", "--no-ext-diff", "--", "handlers.go")
	origFiles, _ := diff.Parse(rawDiff)
	origFile := origFiles[0]

	for i := range origFile.Hunks {
		origFile.Hunks[i].Fingerprint = diff.Fingerprint(origFile.Hunks[i])
	}

	t.Logf("Original hunks: %d", len(origFile.Hunks))
	for i, h := range origFile.Hunks {
		t.Logf("  [%d] old=%d,%d new=%d,%d fp=%s", i, h.OldStart, h.OldCount, h.NewStart, h.NewCount, h.Fingerprint[:16])
	}

	// Setup temp index
	gitDir := filepath.Join(dir, ".git")
	tmpIdx := filepath.Join(dir, "tmp-index")
	copyFile(filepath.Join(gitDir, "index"), tmpIdx)
	defer os.Remove(tmpIdx)
	tempRunner := &git.Runner{Dir: dir, Env: []string{"GIT_INDEX_FILE=" + tmpIdx}}

	// === Commit 0: hunks [0, 1] ===
	p01, _ := patch.BuildPatch(origFile, []int{0, 1}, origFile.Hunks)
	if err := patch.Apply(tempRunner, p01); err != nil {
		t.Fatalf("Apply commit 0: %v", err)
	}
	t.Log("Commit 0 (hunks 0,1) applied OK")

	// === Commit 1: hunk [2] ===
	// Re-diff after commit 0
	reDiff1, _ := tempRunner.Run("diff", "-U0", "--no-ext-diff", "--", "handlers.go")
	reFiles1, _ := diff.Parse(reDiff1)
	if len(reFiles1) == 0 {
		t.Fatal("re-diff after commit 0 returned no files")
	}
	for j := range reFiles1[0].Hunks {
		reFiles1[0].Hunks[j].Fingerprint = diff.Fingerprint(reFiles1[0].Hunks[j])
	}

	t.Logf("After commit 0, re-diff has %d hunks:", len(reFiles1[0].Hunks))
	for i, h := range reFiles1[0].Hunks {
		t.Logf("  [%d] old=%d,%d fp=%s", i, h.OldStart, h.OldCount, h.Fingerprint[:16])
	}

	// Match hunk 2 from original
	origSubset1 := []diff.Hunk{origFile.Hunks[2]}
	matchMap1, err := diff.MatchHunks(origSubset1, reFiles1[0].Hunks)
	if err != nil {
		t.Fatalf("MatchHunks for commit 1 FAILED: %v\n  orig fp=%s\n  current fps: %v",
			err, origSubset1[0].Fingerprint[:16],
			func() []string {
				var fps []string
				for _, h := range reFiles1[0].Hunks {
					fps = append(fps, h.Fingerprint[:16])
				}
				return fps
			}())
	}
	t.Logf("Commit 1 match: %v", matchMap1)

	// Build and apply patch for commit 1
	sel1 := []int{matchMap1[0]}
	p1, _ := patch.BuildPatch(reFiles1[0], sel1, reFiles1[0].Hunks)
	t.Logf("Commit 1 patch:\n%s", string(p1))
	if err := patch.Apply(tempRunner, p1); err != nil {
		t.Fatalf("Apply commit 1: %v", err)
	}
	t.Log("Commit 1 (hunk 2) applied OK")

	// === Commit 2: hunk [3] ===
	reDiff2, _ := tempRunner.Run("diff", "-U0", "--no-ext-diff", "--", "handlers.go")
	t.Logf("Re-diff after commit 1:\n%s", reDiff2)
	reFiles2, _ := diff.Parse(reDiff2)
	if len(reFiles2) == 0 {
		t.Fatal("re-diff after commit 1 returned no files")
	}
	for j := range reFiles2[0].Hunks {
		reFiles2[0].Hunks[j].Fingerprint = diff.Fingerprint(reFiles2[0].Hunks[j])
	}

	t.Logf("After commit 1, re-diff has %d hunks:", len(reFiles2[0].Hunks))
	for i, h := range reFiles2[0].Hunks {
		t.Logf("  [%d] old=%d,%d fp=%s", i, h.OldStart, h.OldCount, h.Fingerprint[:16])
		for _, l := range h.Lines {
			t.Logf("    op=%d %q", l.Op, l.Content)
		}
	}

	// Try to match hunk 3 from original
	origSubset2 := []diff.Hunk{origFile.Hunks[3]}
	t.Logf("Original hunk 3: fp=%s, lines:", origSubset2[0].Fingerprint[:16])
	for _, l := range origSubset2[0].Lines {
		t.Logf("  op=%d %q", l.Op, l.Content)
	}

	matchMap2, err := diff.MatchHunks(origSubset2, reFiles2[0].Hunks)
	if err != nil {
		t.Logf("MatchHunks for commit 2 FAILED: %v", err)
		t.Logf("Original hunk 3 fp: %s", origSubset2[0].Fingerprint)
		for i, h := range reFiles2[0].Hunks {
			t.Logf("Current hunk %d fp: %s", i, h.Fingerprint)
		}
		t.FailNow()
	}
	t.Logf("Commit 2 match: %v", matchMap2)
}

// TestMergedHunkSubPatch is the full end-to-end test for the merged-hunk bug.
// It simulates a 5-hunk file split across 4 commits where hunks 3 and 4 get
// merged by git after earlier commits, verifying that BuildSubPatch correctly
// extracts only the needed lines from the merged hunk.
func TestMergedHunkSubPatch(t *testing.T) {
	dir, r := setupRepo(t)

	base := "package main\n\nfunc handleLogin() {}\n\nfunc handleLogout() {}\n\nfunc handleRefresh() {}\n\nfunc handleRevoke() {}\n\nfunc handleStatus() {}\n"
	os.WriteFile(filepath.Join(dir, "handlers.go"), []byte(base), 0644)
	must(t, run(r, "add", "handlers.go"))
	must(t, run(r, "commit", "-m", "base"))

	modified := "package main\n\nimport \"fmt\"\n\nfunc handleLogin() {\n\tfmt.Println(\"login handler\")\n}\n\nfunc handleLogout() {\n\tfmt.Println(\"logout handler\")\n}\n\nfunc handleRefresh() {\n\tfmt.Println(\"refresh handler\")\n}\n\nfunc handleRevoke() {\n\tfmt.Println(\"revoke handler\")\n}\n\nfunc handleStatus() {\n\tfmt.Println(\"status handler\")\n}\n"
	os.WriteFile(filepath.Join(dir, "handlers.go"), []byte(modified), 0644)

	// Parse original diff -- should produce 5 hunks.
	rawDiff, _ := r.Diff("-U0", "--no-ext-diff", "--", "handlers.go")
	origFiles, _ := diff.Parse(rawDiff)
	origFile := origFiles[0]
	if len(origFile.Hunks) != 5 {
		t.Fatalf("expected 5 original hunks, got %d", len(origFile.Hunks))
	}

	for i := range origFile.Hunks {
		origFile.Hunks[i].Fingerprint = diff.Fingerprint(origFile.Hunks[i])
	}

	// Plan: 4 commits
	//   commit 0: hunks [0, 1]  (import + login)
	//   commit 1: hunk  [2]     (logout)
	//   commit 2: hunk  [3]     (refresh)  <-- this is the one that hits the merged-hunk bug
	//   commit 3: hunk  [4]     (status)   <-- after the fix, this should still work

	// Setup temp index
	gitDir := filepath.Join(dir, ".git")
	tmpIdx := filepath.Join(dir, "tmp-index")
	copyFile(filepath.Join(gitDir, "index"), tmpIdx)
	defer os.Remove(tmpIdx)
	tempRunner := &git.Runner{Dir: dir, Env: []string{"GIT_INDEX_FILE=" + tmpIdx}}

	// Helper to re-diff, fingerprint, match, and apply for a set of original hunks.
	applyOrigHunks := func(commitIdx int, origIndices []int) {
		t.Helper()

		reDiff, _ := tempRunner.Run("diff", "-U0", "--no-ext-diff", "--", "handlers.go")
		reFiles, err := diff.Parse(reDiff)
		if err != nil {
			t.Fatalf("commit %d: parse re-diff: %v", commitIdx, err)
		}
		if len(reFiles) == 0 {
			t.Fatalf("commit %d: re-diff returned no files (all changes already staged?)", commitIdx)
		}
		currentFile := reFiles[0]
		for j := range currentFile.Hunks {
			currentFile.Hunks[j].Fingerprint = diff.Fingerprint(currentFile.Hunks[j])
		}

		// Build original subset.
		origSubset := make([]diff.Hunk, 0, len(origIndices))
		for _, idx := range origIndices {
			origSubset = append(origSubset, origFile.Hunks[idx])
		}

		matchMap, err := diff.MatchHunks(origSubset, currentFile.Hunks)
		if err != nil {
			t.Fatalf("commit %d: MatchHunks failed: %v", commitIdx, err)
		}

		// Group originals by current hunk.
		currentToOrigs := make(map[int][]diff.Hunk)
		for i, oh := range origSubset {
			ci := matchMap[i]
			currentToOrigs[ci] = append(currentToOrigs[ci], oh)
		}

		// Collect unique selected indices.
		seen := make(map[int]bool)
		var selectedIndices []int
		for i := 0; i < len(origSubset); i++ {
			idx := matchMap[i]
			if !seen[idx] {
				selectedIndices = append(selectedIndices, idx)
				seen[idx] = true
			}
		}

		// Check for merged hunks.
		hasMerged := false
		for _, ci := range selectedIndices {
			if patch.IsMergedHunk(currentFile.Hunks[ci], currentToOrigs[ci]) {
				hasMerged = true
				break
			}
		}

		var patchData []byte
		if !hasMerged {
			patchData, err = patch.BuildPatch(currentFile, selectedIndices, currentFile.Hunks)
		} else {
			patchData, err = patch.BuildCompositePatch(currentFile, selectedIndices, currentFile.Hunks, currentToOrigs)
		}
		if err != nil {
			t.Fatalf("commit %d: build patch: %v", commitIdx, err)
		}

		t.Logf("commit %d: patch (merged=%v):\n%s", commitIdx, hasMerged, string(patchData))

		if err := patch.Apply(tempRunner, patchData); err != nil {
			t.Fatalf("commit %d: apply patch failed: %v", commitIdx, err)
		}
		t.Logf("commit %d: applied OK (origHunks=%v, merged=%v)", commitIdx, origIndices, hasMerged)
	}

	// Commit 0: hunks [0, 1] -- use BuildPatch directly (first commit, no merging)
	p0, _ := patch.BuildPatch(origFile, []int{0, 1}, origFile.Hunks)
	if err := patch.Apply(tempRunner, p0); err != nil {
		t.Fatalf("commit 0: apply failed: %v", err)
	}
	t.Log("commit 0: hunks [0,1] applied OK")

	// Commit 1: hunk [2]
	applyOrigHunks(1, []int{2})

	// Commit 2: hunk [3] -- this is the critical one; hunk 3 and 4 may be merged
	applyOrigHunks(2, []int{3})

	// Commit 3: hunk [4] -- after sub-patching commit 2, hunk 4's content must remain
	applyOrigHunks(3, []int{4})

	// After all 4 commits, re-diff should be empty (all changes staged).
	finalDiff, _ := tempRunner.Run("diff", "-U0", "--no-ext-diff", "--", "handlers.go")
	if finalDiff != "" {
		t.Fatalf("expected empty diff after all commits, got:\n%s", finalDiff)
	}
	t.Log("All 4 commits applied successfully; diff is clean")
}
