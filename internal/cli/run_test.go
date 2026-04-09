package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deligoez/hc/internal/git"
	"github.com/deligoez/hc/internal/output"
)

// asResult type-asserts the runPlan result to *output.Result (for non-dry-run).
func asResult(t *testing.T, v any) *output.Result {
	t.Helper()
	r, ok := v.(*output.Result)
	if !ok {
		t.Fatalf("expected *output.Result, got %T", v)
	}
	return r
}

// asDryRun type-asserts the runPlan result to *output.DryRunResult.
func asDryRun(t *testing.T, v any) *output.DryRunResult {
	t.Helper()
	r, ok := v.(*output.DryRunResult)
	if !ok {
		t.Fatalf("expected *output.DryRunResult, got %T", v)
	}
	return r
}

// --- Helpers ---

func gitHelper(t *testing.T, dir string, args ...string) string {
	t.Helper()
	r := git.NewRunner(dir)
	out, err := r.Run(args...)
	if err != nil {
		t.Fatalf("git %s failed: %v", strings.Join(args, " "), err)
	}
	return out
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readGitLog(t *testing.T, dir string) []string {
	t.Helper()
	out := gitHelper(t, dir, "log", "--oneline", "--format=%s")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	// Reverse so oldest is first.
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	return lines
}

func getCommitDiff(t *testing.T, dir, sha string) string {
	t.Helper()
	return gitHelper(t, dir, "diff-tree", "-p", sha)
}

func setupRepo(t *testing.T) (string, *git.Runner) {
	t.Helper()
	dir := t.TempDir()
	r := initRepo(t, dir)
	return dir, r
}

func makePlanJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// --- Tests ---

// Test 56: Single commit, single full file
func TestRunSingleCommitSingleFullFile(t *testing.T) {
	dir, r := setupRepo(t)

	// Create base file and commit
	writeFile(t, dir, "base.go", "package main\n\nfunc old() {}\n")
	must(t, run(r, "add", "base.go"))
	must(t, run(r, "commit", "-m", "add base"))

	// Modify
	writeFile(t, dir, "base.go", "package main\n\nfunc old() {}\n\nfunc newFunc() {}\n")

	plan := map[string]any{
		"commits": []map[string]any{
			{
				"message": "feat: add newFunc",
				"files":   []map[string]any{{"path": "base.go"}},
			},
		},
	}

	rawResult, acErr := runPlan(makePlanJSON(t, plan), r, false)
	if acErr != nil {
		t.Fatalf("runPlan failed: %v", acErr)
	}
	result := asResult(t, rawResult)

	if result.Committed != 1 {
		t.Fatalf("expected 1 committed, got %d", result.Committed)
	}
	if result.Commits[0].SHA == "" {
		t.Fatal("expected non-empty SHA")
	}
	if result.Commits[0].Message != "feat: add newFunc" {
		t.Fatalf("wrong message: %s", result.Commits[0].Message)
	}

	logs := readGitLog(t, dir)
	found := false
	for _, l := range logs {
		if l == "feat: add newFunc" {
			found = true
		}
	}
	if !found {
		t.Fatalf("commit message not found in log: %v", logs)
	}
}

// Test 57: Single commit, multiple full files
func TestRunSingleCommitMultipleFullFiles(t *testing.T) {
	dir, r := setupRepo(t)

	writeFile(t, dir, "a.go", "package a\n")
	writeFile(t, dir, "b.go", "package b\n")
	must(t, run(r, "add", "."))
	must(t, run(r, "commit", "-m", "add files"))

	writeFile(t, dir, "a.go", "package a\n\nfunc A() {}\n")
	writeFile(t, dir, "b.go", "package b\n\nfunc B() {}\n")

	plan := map[string]any{
		"commits": []map[string]any{
			{
				"message": "feat: add A and B",
				"files": []map[string]any{
					{"path": "a.go"},
					{"path": "b.go"},
				},
			},
		},
	}

	rawResult, acErr := runPlan(makePlanJSON(t, plan), r, false)
	if acErr != nil {
		t.Fatalf("runPlan failed: %v", acErr)
	}
	result := asResult(t, rawResult)

	if result.Committed != 1 {
		t.Fatalf("expected 1 committed, got %d", result.Committed)
	}

	diff := getCommitDiff(t, dir, result.Commits[0].SHA)
	if !strings.Contains(diff, "a.go") {
		t.Error("a.go not in commit diff")
	}
	if !strings.Contains(diff, "b.go") {
		t.Error("b.go not in commit diff")
	}
}

// Test 58: Two commits, same file split by hunks
func TestRunTwoCommitsSameFileSplitByHunks(t *testing.T) {
	dir, r := setupRepo(t)

	// Create a file with multiple functions separated by blank lines.
	// Each function body is long enough that modifications produce separate hunks in -U0.
	original := `package main

func existing1() {
	// line1
	// line2
	// line3
	// line4
	// line5
}

func existing2() {
	// line1
	// line2
	// line3
	// line4
	// line5
}

func existing3() {
	// line1
	// line2
	// line3
	// line4
	// line5
}
`
	writeFile(t, dir, "multi.go", original)
	must(t, run(r, "add", "multi.go"))
	must(t, run(r, "commit", "-m", "add multi"))

	// Modify two separate regions to get 2 distinct hunks.
	modified := `package main

func existing1() {
	// MODIFIED1
	// line2
	// line3
	// line4
	// line5
}

func existing2() {
	// line1
	// line2
	// line3
	// line4
	// line5
}

func existing3() {
	// MODIFIED3
	// line2
	// line3
	// line4
	// line5
}
`
	writeFile(t, dir, "multi.go", modified)

	plan := map[string]any{
		"commits": []map[string]any{
			{
				"message": "fix: update existing1",
				"files":   []map[string]any{{"path": "multi.go", "hunks": []int{0}}},
			},
			{
				"message": "fix: update existing3",
				"files":   []map[string]any{{"path": "multi.go", "hunks": []int{1}}},
			},
		},
	}

	rawResult, acErr := runPlan(makePlanJSON(t, plan), r, false)
	if acErr != nil {
		t.Fatalf("runPlan failed: %v", acErr)
	}
	result := asResult(t, rawResult)

	if result.Committed != 2 {
		t.Fatalf("expected 2 committed, got %d", result.Committed)
	}

	// First commit should contain MODIFIED1
	diff1 := getCommitDiff(t, dir, result.Commits[0].SHA)
	if !strings.Contains(diff1, "MODIFIED1") {
		t.Error("first commit should contain MODIFIED1")
	}
	if strings.Contains(diff1, "MODIFIED3") {
		t.Error("first commit should NOT contain MODIFIED3")
	}

	// Second commit should contain MODIFIED3
	diff2 := getCommitDiff(t, dir, result.Commits[1].SHA)
	if !strings.Contains(diff2, "MODIFIED3") {
		t.Error("second commit should contain MODIFIED3")
	}
	if strings.Contains(diff2, "MODIFIED1") {
		t.Error("second commit should NOT contain MODIFIED1")
	}
}

// Test 59: Three commits, mixed full-file and hunk-select
func TestRunThreeCommitsMixedStrategies(t *testing.T) {
	dir, r := setupRepo(t)

	// File A with content that will produce 2 hunks when modified
	fileA := `package a

func funcA1() {
	// line1
	// line2
	// line3
	// line4
	// line5
}

func funcA2() {
	// line1
	// line2
	// line3
	// line4
	// line5
}
`
	writeFile(t, dir, "fileA.go", fileA)
	writeFile(t, dir, "fileB.go", "package b\n\nfunc B1() {}\n")
	must(t, run(r, "add", "."))
	must(t, run(r, "commit", "-m", "add files"))

	// Modify fileA in two regions
	fileAMod := `package a

func funcA1() {
	// CHANGED_A1
	// line2
	// line3
	// line4
	// line5
}

func funcA2() {
	// CHANGED_A2
	// line2
	// line3
	// line4
	// line5
}
`
	writeFile(t, dir, "fileA.go", fileAMod)
	writeFile(t, dir, "fileB.go", "package b\n\nfunc B1() {}\n\nfunc B2() {}\n")

	plan := map[string]any{
		"commits": []map[string]any{
			{
				"message": "fix: update funcA1",
				"files":   []map[string]any{{"path": "fileA.go", "hunks": []int{0}}},
			},
			{
				"message": "fix: update funcA2",
				"files":   []map[string]any{{"path": "fileA.go", "hunks": []int{1}}},
			},
			{
				"message": "feat: update fileB",
				"files":   []map[string]any{{"path": "fileB.go"}},
			},
		},
	}

	rawResult, acErr := runPlan(makePlanJSON(t, plan), r, false)
	if acErr != nil {
		t.Fatalf("runPlan failed: %v", acErr)
	}
	result := asResult(t, rawResult)

	if result.Committed != 3 {
		t.Fatalf("expected 3 committed, got %d", result.Committed)
	}

	// Verify commit 0 has CHANGED_A1
	d0 := getCommitDiff(t, dir, result.Commits[0].SHA)
	if !strings.Contains(d0, "CHANGED_A1") {
		t.Error("commit 0 should contain CHANGED_A1")
	}
	if strings.Contains(d0, "CHANGED_A2") {
		t.Error("commit 0 should NOT contain CHANGED_A2")
	}

	// Verify commit 1 has CHANGED_A2
	d1 := getCommitDiff(t, dir, result.Commits[1].SHA)
	if !strings.Contains(d1, "CHANGED_A2") {
		t.Error("commit 1 should contain CHANGED_A2")
	}

	// Verify commit 2 has fileB changes
	d2 := getCommitDiff(t, dir, result.Commits[2].SHA)
	if !strings.Contains(d2, "fileB.go") {
		t.Error("commit 2 should contain fileB.go")
	}
	if !strings.Contains(d2, "B2") {
		t.Error("commit 2 should contain B2")
	}
}

// Test 60: Five commits, one hunk per commit
func TestRunFiveCommitsOneHunkEach(t *testing.T) {
	dir, r := setupRepo(t)

	// Create a file with 5 functions, each with enough lines to separate hunks
	original := `package main

func f1() {
	// f1 line1
	// f1 line2
	// f1 line3
}

func f2() {
	// f2 line1
	// f2 line2
	// f2 line3
}

func f3() {
	// f3 line1
	// f3 line2
	// f3 line3
}

func f4() {
	// f4 line1
	// f4 line2
	// f4 line3
}

func f5() {
	// f5 line1
	// f5 line2
	// f5 line3
}
`
	writeFile(t, dir, "funcs.go", original)
	must(t, run(r, "add", "funcs.go"))
	must(t, run(r, "commit", "-m", "add funcs"))

	// Modify each function in a distinct way
	modified := `package main

func f1() {
	// f1 MODIFIED
	// f1 line2
	// f1 line3
}

func f2() {
	// f2 MODIFIED
	// f2 line2
	// f2 line3
}

func f3() {
	// f3 MODIFIED
	// f3 line2
	// f3 line3
}

func f4() {
	// f4 MODIFIED
	// f4 line2
	// f4 line3
}

func f5() {
	// f5 MODIFIED
	// f5 line2
	// f5 line3
}
`
	writeFile(t, dir, "funcs.go", modified)

	commits := make([]map[string]any, 5)
	for i := 0; i < 5; i++ {
		commits[i] = map[string]any{
			"message": strings.ReplaceAll("fix: update fN", "N", string(rune('1'+i))),
			"files":   []map[string]any{{"path": "funcs.go", "hunks": []int{i}}},
		}
	}

	plan := map[string]any{"commits": commits}

	rawResult, acErr := runPlan(makePlanJSON(t, plan), r, false)
	if acErr != nil {
		t.Fatalf("runPlan failed: %v", acErr)
	}
	result := asResult(t, rawResult)

	if result.Committed != 5 {
		t.Fatalf("expected 5 committed, got %d", result.Committed)
	}

	for i := 0; i < 5; i++ {
		marker := strings.ReplaceAll("fN MODIFIED", "N", string(rune('1'+i)))
		d := getCommitDiff(t, dir, result.Commits[i].SHA)
		if !strings.Contains(d, marker) {
			t.Errorf("commit %d should contain %q", i, marker)
		}
		// Ensure other markers are NOT present
		for j := 0; j < 5; j++ {
			if j == i {
				continue
			}
			other := strings.ReplaceAll("fN MODIFIED", "N", string(rune('1'+j)))
			if strings.Contains(d, other) {
				t.Errorf("commit %d should NOT contain %q", i, other)
			}
		}
	}

	logs := readGitLog(t, dir)
	// Last 5 logs should be the 5 commits (oldest first)
	runLogs := logs[len(logs)-5:]
	for i := 0; i < 5; i++ {
		expected := strings.ReplaceAll("fix: update fN", "N", string(rune('1'+i)))
		if runLogs[i] != expected {
			t.Errorf("log[%d] = %q, want %q", i, runLogs[i], expected)
		}
	}
}

// Test 61: New file (untracked)
func TestRunNewFileUntracked(t *testing.T) {
	dir, r := setupRepo(t)

	// Just add a new file in the working tree (never committed before)
	writeFile(t, dir, "brand_new.go", "package main\n\nfunc BrandNew() {}\n")

	plan := map[string]any{
		"commits": []map[string]any{
			{
				"message": "feat: add brand_new.go",
				"files":   []map[string]any{{"path": "brand_new.go"}},
			},
		},
	}

	rawResult, acErr := runPlan(makePlanJSON(t, plan), r, false)
	if acErr != nil {
		t.Fatalf("runPlan failed: %v", acErr)
	}
	result := asResult(t, rawResult)

	if result.Committed != 1 {
		t.Fatalf("expected 1 committed, got %d", result.Committed)
	}

	d := getCommitDiff(t, dir, result.Commits[0].SHA)
	if !strings.Contains(d, "brand_new.go") {
		t.Error("commit should contain brand_new.go")
	}
	if !strings.Contains(d, "BrandNew") {
		t.Error("commit should contain BrandNew function")
	}
}

// Test 62: Deleted file
func TestRunDeletedFile(t *testing.T) {
	dir, r := setupRepo(t)

	writeFile(t, dir, "to_delete.go", "package main\n\nfunc ToDelete() {}\n")
	must(t, run(r, "add", "to_delete.go"))
	must(t, run(r, "commit", "-m", "add to_delete"))

	// Delete the file
	if err := os.Remove(filepath.Join(dir, "to_delete.go")); err != nil {
		t.Fatal(err)
	}

	plan := map[string]any{
		"commits": []map[string]any{
			{
				"message": "chore: remove to_delete.go",
				"files":   []map[string]any{{"path": "to_delete.go"}},
			},
		},
	}

	rawResult, acErr := runPlan(makePlanJSON(t, plan), r, false)
	if acErr != nil {
		t.Fatalf("runPlan failed: %v", acErr)
	}
	result := asResult(t, rawResult)

	if result.Committed != 1 {
		t.Fatalf("expected 1 committed, got %d", result.Committed)
	}

	d := getCommitDiff(t, dir, result.Commits[0].SHA)
	if !strings.Contains(d, "to_delete.go") {
		t.Error("commit should reference to_delete.go")
	}
}

// Test 63: Renamed file -- SKIP
func TestRunRenamedFile(t *testing.T) {
	t.Skip("rename detection varies by git version")
}

// Test 64: allow_unplanned
func TestRunAllowUnplanned(t *testing.T) {
	dir, r := setupRepo(t)

	writeFile(t, dir, "planned.go", "package p\n")
	writeFile(t, dir, "unplanned.go", "package u\n")
	must(t, run(r, "add", "."))
	must(t, run(r, "commit", "-m", "add files"))

	writeFile(t, dir, "planned.go", "package p\n\nfunc P() {}\n")
	writeFile(t, dir, "unplanned.go", "package u\n\nfunc U() {}\n")

	plan := map[string]any{
		"commits": []map[string]any{
			{
				"message": "feat: add P",
				"files":   []map[string]any{{"path": "planned.go"}},
			},
		},
		"allow_unplanned": []string{"unplanned.go"},
	}

	rawResult, acErr := runPlan(makePlanJSON(t, plan), r, false)
	if acErr != nil {
		t.Fatalf("runPlan failed: %v", acErr)
	}
	result := asResult(t, rawResult)

	if result.Committed != 1 {
		t.Fatalf("expected 1 committed, got %d", result.Committed)
	}

	// Verify unplanned.go still has uncommitted changes
	statusOut := gitHelper(t, dir, "status", "--porcelain")
	if !strings.Contains(statusOut, "unplanned.go") {
		t.Error("unplanned.go should still have uncommitted changes")
	}
}

// Test 65: --dry-run
func TestRunDryRun(t *testing.T) {
	dir, r := setupRepo(t)

	writeFile(t, dir, "dry.go", "package d\n")
	must(t, run(r, "add", "dry.go"))
	must(t, run(r, "commit", "-m", "add dry"))

	writeFile(t, dir, "dry.go", "package d\n\nfunc Dry() {}\n")

	plan := map[string]any{
		"commits": []map[string]any{
			{
				"message": "feat: add Dry",
				"files":   []map[string]any{{"path": "dry.go"}},
			},
		},
	}

	// Get HEAD before
	headBefore := strings.TrimSpace(gitHelper(t, dir, "rev-parse", "HEAD"))

	rawDryResult, acErr := runPlan(makePlanJSON(t, plan), r, true)
	if acErr != nil {
		t.Fatalf("runPlan failed: %v", acErr)
	}
	result := asDryRun(t, rawDryResult)

	// Verify DryRunResult fields
	if !result.Valid {
		t.Fatal("expected valid=true")
	}
	if result.Commits != 1 {
		t.Fatalf("expected commits=1, got %d", result.Commits)
	}
	if result.Files < 1 {
		t.Fatalf("expected files>=1, got %d", result.Files)
	}

	// HEAD should not have moved
	headAfter := strings.TrimSpace(gitHelper(t, dir, "rev-parse", "HEAD"))
	if headBefore != headAfter {
		t.Fatalf("dry-run should not create commits: HEAD moved from %s to %s", headBefore, headAfter)
	}
}

// Test 66: Stdin input -- end-to-end test via runPlan with []byte (same path as stdin)
func TestRunStdinInput(t *testing.T) {
	dir, r := setupRepo(t)

	// Create base file and commit
	writeFile(t, dir, "stdin.go", "package main\n\nfunc Old() {}\n")
	must(t, run(r, "add", "stdin.go"))
	must(t, run(r, "commit", "-m", "add stdin"))

	// Modify
	writeFile(t, dir, "stdin.go", "package main\n\nfunc Old() {}\n\nfunc New() {}\n")

	planJSON := makePlanJSON(t, map[string]any{
		"commits": []map[string]any{
			{
				"message": "feat: add New via stdin path",
				"files":   []map[string]any{{"path": "stdin.go"}},
			},
		},
	})

	// Call runPlan directly with []byte -- this is the same path used by stdin
	rawResult, acErr := runPlan(planJSON, r, false)
	if acErr != nil {
		t.Fatalf("runPlan failed: %v", acErr)
	}
	result := asResult(t, rawResult)

	if result.Committed != 1 {
		t.Fatalf("expected 1 committed, got %d", result.Committed)
	}

	logs := readGitLog(t, dir)
	found := false
	for _, l := range logs {
		if l == "feat: add New via stdin path" {
			found = true
		}
	}
	if !found {
		t.Fatalf("commit message not found in log: %v", logs)
	}
}

// Test 67: TTY vs pipe output -- unit test of Printer.UseJSON logic
func TestRunTTYVsPipeOutput(t *testing.T) {
	// When ForceJSON is set, UseJSON should return true regardless of IsTTY
	p := &output.Printer{ForceJSON: true, IsTTY: true}
	if !p.UseJSON() {
		t.Error("expected UseJSON()=true when ForceJSON is set")
	}

	p = &output.Printer{ForceJSON: true, IsTTY: false}
	if !p.UseJSON() {
		t.Error("expected UseJSON()=true when ForceJSON is set and not TTY")
	}

	// When IsTTY is true and ForceJSON is false, UseJSON should return false
	p = &output.Printer{IsTTY: true, ForceJSON: false}
	if p.UseJSON() {
		t.Error("expected UseJSON()=false when IsTTY is true and ForceJSON is false")
	}

	// When neither IsTTY nor ForceJSON, UseJSON should return true (pipe mode)
	p = &output.Printer{IsTTY: false, ForceJSON: false}
	if !p.UseJSON() {
		t.Error("expected UseJSON()=true when not TTY (pipe mode)")
	}
}

// Bug regression: untracked file commit should leave clean staging area
func TestRunUntrackedFileCleanStaging(t *testing.T) {
	dir, r := setupRepo(t)

	// Create a tracked file with changes AND an untracked new file
	writeFile(t, dir, "existing.go", "package main\n\nfunc Existing() {}\n")
	must(t, run(r, "add", "existing.go"))
	must(t, run(r, "commit", "-m", "add existing"))
	writeFile(t, dir, "existing.go", "package main\n\nfunc Existing() {}\n\nfunc Modified() {}\n")
	writeFile(t, dir, "brand_new.go", "package main\n\nfunc BrandNew() {}\n")

	// Commit only the new file, allow_unplanned the existing changes
	planData := makePlanJSON(t, map[string]any{
		"commits": []map[string]any{
			{
				"message": "feat: add brand_new",
				"files":   []map[string]any{{"path": "brand_new.go"}},
			},
		},
		"allow_unplanned": []string{"existing.go"},
	})

	rawResult, acErr := runPlan(planData, r, false)
	if acErr != nil {
		t.Fatalf("runPlan failed: %v", acErr)
	}
	result := asResult(t, rawResult)
	if result.Committed != 1 {
		t.Fatalf("expected 1 committed, got %d", result.Committed)
	}

	// Key assertion: staging area must be clean after successful run
	cachedOut, err := r.Run("diff", "--cached", "--stat")
	if err != nil {
		t.Fatalf("git diff --cached failed: %v", err)
	}
	if strings.TrimSpace(cachedOut) != "" {
		t.Fatalf("staging area is not clean after run: %s", cachedOut)
	}

	// The unplanned file should still have uncommitted changes
	statusOut, _ := r.Run("status", "--porcelain")
	if !strings.Contains(statusOut, "existing.go") {
		t.Error("existing.go should still have uncommitted changes")
	}
}

// Bug regression: 5 hunks split across 4 commits (2+1+1+1) should work
func TestRunFiveHunksFourCommits(t *testing.T) {
	dir, r := setupRepo(t)

	// Create a file with 5 functions, each with multiple lines
	original := `package handlers

func Handle1() {
	// h1 line1
	// h1 line2
	// h1 line3
	// h1 line4
	// h1 line5
}

func Handle2() {
	// h2 line1
	// h2 line2
	// h2 line3
	// h2 line4
	// h2 line5
}

func Handle3() {
	// h3 line1
	// h3 line2
	// h3 line3
	// h3 line4
	// h3 line5
}

func Handle4() {
	// h4 line1
	// h4 line2
	// h4 line3
	// h4 line4
	// h4 line5
}

func Handle5() {
	// h5 line1
	// h5 line2
	// h5 line3
	// h5 line4
	// h5 line5
}
`
	writeFile(t, dir, "handlers.go", original)
	must(t, run(r, "add", "handlers.go"))
	must(t, run(r, "commit", "-m", "add handlers"))

	// Modify all 5 functions (creating 5 hunks)
	modified := `package handlers

func Handle1() {
	// h1 MODIFIED
	// h1 line2
	// h1 line3
	// h1 line4
	// h1 line5
}

func Handle2() {
	// h2 MODIFIED
	// h2 line2
	// h2 line3
	// h2 line4
	// h2 line5
}

func Handle3() {
	// h3 MODIFIED
	// h3 line2
	// h3 line3
	// h3 line4
	// h3 line5
}

func Handle4() {
	// h4 MODIFIED
	// h4 line2
	// h4 line3
	// h4 line4
	// h4 line5
}

func Handle5() {
	// h5 MODIFIED
	// h5 line2
	// h5 line3
	// h5 line4
	// h5 line5
}
`
	writeFile(t, dir, "handlers.go", modified)

	// Plan: commit 0 gets hunks [0,1], commit 1 gets [2], commit 2 gets [3], commit 3 gets [4]
	plan := map[string]any{
		"commits": []map[string]any{
			{
				"message": "fix: update Handle1 and Handle2",
				"files":   []map[string]any{{"path": "handlers.go", "hunks": []int{0, 1}}},
			},
			{
				"message": "fix: update Handle3",
				"files":   []map[string]any{{"path": "handlers.go", "hunks": []int{2}}},
			},
			{
				"message": "fix: update Handle4",
				"files":   []map[string]any{{"path": "handlers.go", "hunks": []int{3}}},
			},
			{
				"message": "fix: update Handle5",
				"files":   []map[string]any{{"path": "handlers.go", "hunks": []int{4}}},
			},
		},
	}

	rawResult, acErr := runPlan(makePlanJSON(t, plan), r, false)
	if acErr != nil {
		t.Fatalf("runPlan failed: %v", acErr)
	}
	result := asResult(t, rawResult)

	if result.Committed != 4 {
		t.Fatalf("expected 4 committed, got %d", result.Committed)
	}

	// Verify commit 0 has Handle1 and Handle2 changes
	d0 := getCommitDiff(t, dir, result.Commits[0].SHA)
	if !strings.Contains(d0, "h1 MODIFIED") {
		t.Error("commit 0 should contain h1 MODIFIED")
	}
	if !strings.Contains(d0, "h2 MODIFIED") {
		t.Error("commit 0 should contain h2 MODIFIED")
	}

	// Verify commit 1 has Handle3 changes
	d1 := getCommitDiff(t, dir, result.Commits[1].SHA)
	if !strings.Contains(d1, "h3 MODIFIED") {
		t.Error("commit 1 should contain h3 MODIFIED")
	}

	// Verify commit 2 has Handle4 changes
	d2 := getCommitDiff(t, dir, result.Commits[2].SHA)
	if !strings.Contains(d2, "h4 MODIFIED") {
		t.Error("commit 2 should contain h4 MODIFIED")
	}

	// Verify commit 3 has Handle5 changes
	d3 := getCommitDiff(t, dir, result.Commits[3].SHA)
	if !strings.Contains(d3, "h5 MODIFIED") {
		t.Error("commit 3 should contain h5 MODIFIED")
	}
}

// Bug regression: 5 hunks across 4 commits, file without trailing newline
func TestRunFiveHunksFourCommitsNoTrailingNewline(t *testing.T) {
	dir, r := setupRepo(t)

	// File WITHOUT trailing newline
	original := "package handlers\n\nfunc Handle1() {\n\t// h1 line1\n\t// h1 line2\n\t// h1 line3\n\t// h1 line4\n\t// h1 line5\n}\n\nfunc Handle2() {\n\t// h2 line1\n\t// h2 line2\n\t// h2 line3\n\t// h2 line4\n\t// h2 line5\n}\n\nfunc Handle3() {\n\t// h3 line1\n\t// h3 line2\n\t// h3 line3\n\t// h3 line4\n\t// h3 line5\n}\n\nfunc Handle4() {\n\t// h4 line1\n\t// h4 line2\n\t// h4 line3\n\t// h4 line4\n\t// h4 line5\n}\n\nfunc Handle5() {\n\t// h5 line1\n\t// h5 line2\n\t// h5 line3\n\t// h5 line4\n\t// h5 line5\n}"

	writeFile(t, dir, "handlers.go", original)
	must(t, run(r, "add", "handlers.go"))
	must(t, run(r, "commit", "-m", "add handlers"))

	// Modified, also without trailing newline
	modified := "package handlers\n\nfunc Handle1() {\n\t// h1 MODIFIED\n\t// h1 line2\n\t// h1 line3\n\t// h1 line4\n\t// h1 line5\n}\n\nfunc Handle2() {\n\t// h2 MODIFIED\n\t// h2 line2\n\t// h2 line3\n\t// h2 line4\n\t// h2 line5\n}\n\nfunc Handle3() {\n\t// h3 MODIFIED\n\t// h3 line2\n\t// h3 line3\n\t// h3 line4\n\t// h3 line5\n}\n\nfunc Handle4() {\n\t// h4 MODIFIED\n\t// h4 line2\n\t// h4 line3\n\t// h4 line4\n\t// h4 line5\n}\n\nfunc Handle5() {\n\t// h5 MODIFIED\n\t// h5 line2\n\t// h5 line3\n\t// h5 line4\n\t// h5 line5\n}"

	writeFile(t, dir, "handlers.go", modified)

	plan := map[string]any{
		"commits": []map[string]any{
			{
				"message": "fix: update Handle1 and Handle2",
				"files":   []map[string]any{{"path": "handlers.go", "hunks": []int{0, 1}}},
			},
			{
				"message": "fix: update Handle3",
				"files":   []map[string]any{{"path": "handlers.go", "hunks": []int{2}}},
			},
			{
				"message": "fix: update Handle4",
				"files":   []map[string]any{{"path": "handlers.go", "hunks": []int{3}}},
			},
			{
				"message": "fix: update Handle5",
				"files":   []map[string]any{{"path": "handlers.go", "hunks": []int{4}}},
			},
		},
	}

	rawResult, acErr := runPlan(makePlanJSON(t, plan), r, false)
	if acErr != nil {
		t.Fatalf("runPlan failed: %v", acErr)
	}
	result := asResult(t, rawResult)

	if result.Committed != 4 {
		t.Fatalf("expected 4 committed, got %d", result.Committed)
	}

	d0 := getCommitDiff(t, dir, result.Commits[0].SHA)
	if !strings.Contains(d0, "h1 MODIFIED") || !strings.Contains(d0, "h2 MODIFIED") {
		t.Error("commit 0 should contain h1 and h2 MODIFIED")
	}
	d1 := getCommitDiff(t, dir, result.Commits[1].SHA)
	if !strings.Contains(d1, "h3 MODIFIED") {
		t.Error("commit 1 should contain h3 MODIFIED")
	}
	d2 := getCommitDiff(t, dir, result.Commits[2].SHA)
	if !strings.Contains(d2, "h4 MODIFIED") {
		t.Error("commit 2 should contain h4 MODIFIED")
	}
	d3 := getCommitDiff(t, dir, result.Commits[3].SHA)
	if !strings.Contains(d3, "h5 MODIFIED") {
		t.Error("commit 3 should contain h5 MODIFIED")
	}
}

// Bug regression: 5 hunks with multi-line changes, split 2+1+1+1
func TestRunFiveHunksMultiLineChanges(t *testing.T) {
	dir, r := setupRepo(t)

	// Each function has lines that will ALL be changed (multi-line hunks)
	original := `package handlers

func Handle1() {
	oldA1 := "a1"
	oldB1 := "b1"
	_ = oldA1
	_ = oldB1
}

func Handle2() {
	oldA2 := "a2"
	oldB2 := "b2"
	_ = oldA2
	_ = oldB2
}

func Handle3() {
	oldA3 := "a3"
	oldB3 := "b3"
	_ = oldA3
	_ = oldB3
}

func Handle4() {
	oldA4 := "a4"
	oldB4 := "b4"
	_ = oldA4
	_ = oldB4
}

func Handle5() {
	oldA5 := "a5"
	oldB5 := "b5"
	_ = oldA5
	_ = oldB5
}
`
	writeFile(t, dir, "handlers.go", original)
	must(t, run(r, "add", "handlers.go"))
	must(t, run(r, "commit", "-m", "add handlers"))

	// Replace multiple lines in each function
	modified := `package handlers

func Handle1() {
	newA1 := "a1_new"
	newB1 := "b1_new"
	_ = newA1
	_ = newB1
}

func Handle2() {
	newA2 := "a2_new"
	newB2 := "b2_new"
	_ = newA2
	_ = newB2
}

func Handle3() {
	newA3 := "a3_new"
	newB3 := "b3_new"
	_ = newA3
	_ = newB3
}

func Handle4() {
	newA4 := "a4_new"
	newB4 := "b4_new"
	_ = newA4
	_ = newB4
}

func Handle5() {
	newA5 := "a5_new"
	newB5 := "b5_new"
	_ = newA5
	_ = newB5
}
`
	writeFile(t, dir, "handlers.go", modified)

	plan := map[string]any{
		"commits": []map[string]any{
			{
				"message": "fix: update Handle1 and Handle2",
				"files":   []map[string]any{{"path": "handlers.go", "hunks": []int{0, 1}}},
			},
			{
				"message": "fix: update Handle3",
				"files":   []map[string]any{{"path": "handlers.go", "hunks": []int{2}}},
			},
			{
				"message": "fix: update Handle4",
				"files":   []map[string]any{{"path": "handlers.go", "hunks": []int{3}}},
			},
			{
				"message": "fix: update Handle5",
				"files":   []map[string]any{{"path": "handlers.go", "hunks": []int{4}}},
			},
		},
	}

	rawResult, acErr := runPlan(makePlanJSON(t, plan), r, false)
	if acErr != nil {
		t.Fatalf("runPlan failed: %v", acErr)
	}
	result := asResult(t, rawResult)

	if result.Committed != 4 {
		t.Fatalf("expected 4 committed, got %d", result.Committed)
	}

	d0 := getCommitDiff(t, dir, result.Commits[0].SHA)
	if !strings.Contains(d0, "a1_new") || !strings.Contains(d0, "a2_new") {
		t.Error("commit 0 should contain Handle1 and Handle2 changes")
	}
	d1 := getCommitDiff(t, dir, result.Commits[1].SHA)
	if !strings.Contains(d1, "a3_new") {
		t.Error("commit 1 should contain Handle3 changes")
	}
	d2 := getCommitDiff(t, dir, result.Commits[2].SHA)
	if !strings.Contains(d2, "a4_new") {
		t.Error("commit 2 should contain Handle4 changes")
	}
	d3 := getCommitDiff(t, dir, result.Commits[3].SHA)
	if !strings.Contains(d3, "a5_new") {
		t.Error("commit 3 should contain Handle5 changes")
	}
}

// Bug regression: 5 hunks with net line-count changes (additions > deletions)
func TestRunFiveHunksNetLineChanges(t *testing.T) {
	dir, r := setupRepo(t)

	// Each function body is short
	original := `package handlers

func Handle1() {
	// h1 old
}

func Handle2() {
	// h2 old
}

func Handle3() {
	// h3 old
}

func Handle4() {
	// h4 old
}

func Handle5() {
	// h5 old
}
`
	writeFile(t, dir, "handlers.go", original)
	must(t, run(r, "add", "handlers.go"))
	must(t, run(r, "commit", "-m", "add handlers"))

	// Each modification ADDS extra lines (net +2 lines per hunk)
	modified := `package handlers

func Handle1() {
	// h1 new line1
	// h1 new line2
	// h1 new line3
}

func Handle2() {
	// h2 new line1
	// h2 new line2
	// h2 new line3
}

func Handle3() {
	// h3 new line1
	// h3 new line2
	// h3 new line3
}

func Handle4() {
	// h4 new line1
	// h4 new line2
	// h4 new line3
}

func Handle5() {
	// h5 new line1
	// h5 new line2
	// h5 new line3
}
`
	writeFile(t, dir, "handlers.go", modified)

	plan := map[string]any{
		"commits": []map[string]any{
			{
				"message": "fix: update Handle1 and Handle2",
				"files":   []map[string]any{{"path": "handlers.go", "hunks": []int{0, 1}}},
			},
			{
				"message": "fix: update Handle3",
				"files":   []map[string]any{{"path": "handlers.go", "hunks": []int{2}}},
			},
			{
				"message": "fix: update Handle4",
				"files":   []map[string]any{{"path": "handlers.go", "hunks": []int{3}}},
			},
			{
				"message": "fix: update Handle5",
				"files":   []map[string]any{{"path": "handlers.go", "hunks": []int{4}}},
			},
		},
	}

	rawResult, acErr := runPlan(makePlanJSON(t, plan), r, false)
	if acErr != nil {
		t.Fatalf("runPlan failed: %v", acErr)
	}
	result := asResult(t, rawResult)

	if result.Committed != 4 {
		t.Fatalf("expected 4 committed, got %d", result.Committed)
	}

	d0 := getCommitDiff(t, dir, result.Commits[0].SHA)
	if !strings.Contains(d0, "h1 new") || !strings.Contains(d0, "h2 new") {
		t.Error("commit 0 should contain Handle1 and Handle2 changes")
	}
	d1 := getCommitDiff(t, dir, result.Commits[1].SHA)
	if !strings.Contains(d1, "h3 new") {
		t.Error("commit 1 should contain Handle3 changes")
	}
	d2 := getCommitDiff(t, dir, result.Commits[2].SHA)
	if !strings.Contains(d2, "h4 new") {
		t.Error("commit 2 should contain Handle4 changes")
	}
	d3 := getCommitDiff(t, dir, result.Commits[3].SHA)
	if !strings.Contains(d3, "h5 new") {
		t.Error("commit 3 should contain Handle5 changes")
	}
}

// Bug regression: net line changes + no trailing newline
func TestRunFiveHunksNetLineChangesNoNewline(t *testing.T) {
	dir, r := setupRepo(t)

	// File WITHOUT trailing newline, each function has 1 line body
	original := "package handlers\n\nfunc Handle1() {\n\t// h1 old\n}\n\nfunc Handle2() {\n\t// h2 old\n}\n\nfunc Handle3() {\n\t// h3 old\n}\n\nfunc Handle4() {\n\t// h4 old\n}\n\nfunc Handle5() {\n\t// h5 old\n}"

	writeFile(t, dir, "handlers.go", original)
	must(t, run(r, "add", "handlers.go"))
	must(t, run(r, "commit", "-m", "add handlers"))

	// Each modification adds extra lines; file still has no trailing newline
	modified := "package handlers\n\nfunc Handle1() {\n\t// h1 new line1\n\t// h1 new line2\n\t// h1 new line3\n}\n\nfunc Handle2() {\n\t// h2 new line1\n\t// h2 new line2\n\t// h2 new line3\n}\n\nfunc Handle3() {\n\t// h3 new line1\n\t// h3 new line2\n\t// h3 new line3\n}\n\nfunc Handle4() {\n\t// h4 new line1\n\t// h4 new line2\n\t// h4 new line3\n}\n\nfunc Handle5() {\n\t// h5 new line1\n\t// h5 new line2\n\t// h5 new line3\n}"

	writeFile(t, dir, "handlers.go", modified)

	plan := map[string]any{
		"commits": []map[string]any{
			{
				"message": "fix: update Handle1 and Handle2",
				"files":   []map[string]any{{"path": "handlers.go", "hunks": []int{0, 1}}},
			},
			{
				"message": "fix: update Handle3",
				"files":   []map[string]any{{"path": "handlers.go", "hunks": []int{2}}},
			},
			{
				"message": "fix: update Handle4",
				"files":   []map[string]any{{"path": "handlers.go", "hunks": []int{3}}},
			},
			{
				"message": "fix: update Handle5",
				"files":   []map[string]any{{"path": "handlers.go", "hunks": []int{4}}},
			},
		},
	}

	rawResult, acErr := runPlan(makePlanJSON(t, plan), r, false)
	if acErr != nil {
		t.Fatalf("runPlan failed: %v", acErr)
	}
	result := asResult(t, rawResult)

	if result.Committed != 4 {
		t.Fatalf("expected 4 committed, got %d", result.Committed)
	}
}

// Bug regression: no trailing newline in file -- BuildPatch must emit
// "\ No newline at end of file" markers for lines missing \n.
func TestRunFiveHunksNoTrailingNewlineFile(t *testing.T) {
	dir, r := setupRepo(t)

	original := "package handlers\n\nfunc Handle1() { return }\n\nfunc Handle2() { return }\n\nfunc Handle3() { return }\n\nfunc Handle4() { return }\n\nfunc Handle5() { return }"
	writeFile(t, dir, "handlers.go", original)
	must(t, run(r, "add", "handlers.go"))
	must(t, run(r, "commit", "-m", "add handlers"))

	modified := "package handlers\n\nfunc Handle1() { fmt.Println(\"h1\") }\n\nfunc Handle2() { fmt.Println(\"h2\") }\n\nfunc Handle3() { fmt.Println(\"h3\") }\n\nfunc Handle4() { fmt.Println(\"h4\") }\n\nfunc Handle5() { fmt.Println(\"h5\") }"
	writeFile(t, dir, "handlers.go", modified)

	plan := map[string]any{
		"commits": []map[string]any{
			{
				"message": "fix: update Handle1 and Handle2",
				"files":   []map[string]any{{"path": "handlers.go", "hunks": []int{0, 1}}},
			},
			{
				"message": "fix: update Handle3",
				"files":   []map[string]any{{"path": "handlers.go", "hunks": []int{2}}},
			},
			{
				"message": "fix: update Handle4",
				"files":   []map[string]any{{"path": "handlers.go", "hunks": []int{3}}},
			},
			{
				"message": "fix: update Handle5",
				"files":   []map[string]any{{"path": "handlers.go", "hunks": []int{4}}},
			},
		},
	}

	rawResult, acErr := runPlan(makePlanJSON(t, plan), r, false)
	if acErr != nil {
		t.Fatalf("runPlan failed: %v", acErr)
	}
	result := asResult(t, rawResult)

	if result.Committed != 4 {
		t.Fatalf("expected 4 committed, got %d", result.Committed)
	}

	d0 := getCommitDiff(t, dir, result.Commits[0].SHA)
	if !strings.Contains(d0, "h1") || !strings.Contains(d0, "h2") {
		t.Error("commit 0 should contain h1 and h2")
	}
	d1 := getCommitDiff(t, dir, result.Commits[1].SHA)
	if !strings.Contains(d1, "h3") {
		t.Error("commit 1 should contain h3")
	}
	d2 := getCommitDiff(t, dir, result.Commits[2].SHA)
	if !strings.Contains(d2, "h4") {
		t.Error("commit 2 should contain h4")
	}
	d3 := getCommitDiff(t, dir, result.Commits[3].SHA)
	if !strings.Contains(d3, "h5") {
		t.Error("commit 3 should contain h5")
	}
}

// Variant: original has trailing newline but modified doesn't
func TestRunFiveHunksModifiedNoTrailingNewline(t *testing.T) {
	dir, r := setupRepo(t)

	original := "package handlers\n\nfunc Handle1() { return }\n\nfunc Handle2() { return }\n\nfunc Handle3() { return }\n\nfunc Handle4() { return }\n\nfunc Handle5() { return }\n"
	writeFile(t, dir, "handlers.go", original)
	must(t, run(r, "add", "handlers.go"))
	must(t, run(r, "commit", "-m", "add handlers"))

	modified := "package handlers\n\nfunc Handle1() { fmt.Println(\"h1\") }\n\nfunc Handle2() { fmt.Println(\"h2\") }\n\nfunc Handle3() { fmt.Println(\"h3\") }\n\nfunc Handle4() { fmt.Println(\"h4\") }\n\nfunc Handle5() { fmt.Println(\"h5\") }"
	writeFile(t, dir, "handlers.go", modified)

	plan := map[string]any{
		"commits": []map[string]any{
			{
				"message": "fix: update Handle1 and Handle2",
				"files":   []map[string]any{{"path": "handlers.go", "hunks": []int{0, 1}}},
			},
			{
				"message": "fix: update Handle3",
				"files":   []map[string]any{{"path": "handlers.go", "hunks": []int{2}}},
			},
			{
				"message": "fix: update Handle4",
				"files":   []map[string]any{{"path": "handlers.go", "hunks": []int{3}}},
			},
			{
				"message": "fix: update Handle5",
				"files":   []map[string]any{{"path": "handlers.go", "hunks": []int{4}}},
			},
		},
	}

	rawResult, acErr := runPlan(makePlanJSON(t, plan), r, false)
	if acErr != nil {
		t.Fatalf("runPlan failed: %v", acErr)
	}
	result := asResult(t, rawResult)

	if result.Committed != 4 {
		t.Fatalf("expected 4 committed, got %d", result.Committed)
	}
}

// Bug regression: two sequential ac run calls should both succeed
func TestRunSequentialCalls(t *testing.T) {
	dir, r := setupRepo(t)

	writeFile(t, dir, "a.go", "package main\n\nfunc A() {}\n")
	must(t, run(r, "add", "a.go"))
	must(t, run(r, "commit", "-m", "add a"))

	// First run: commit a change
	writeFile(t, dir, "a.go", "package main\n\nfunc A() {}\n\nfunc A2() {}\n")
	writeFile(t, dir, "b.go", "package main\n\nfunc B() {}\n")

	plan1 := makePlanJSON(t, map[string]any{
		"commits": []map[string]any{
			{"message": "feat: add A2", "files": []map[string]any{{"path": "a.go"}}},
		},
		"allow_unplanned": []string{"b.go"},
	})

	_, acErr := runPlan(plan1, r, false)
	if acErr != nil {
		t.Fatalf("first runPlan failed: %v", acErr)
	}

	// Second run: commit the other file (should not fail due to dirty staging)
	plan2 := makePlanJSON(t, map[string]any{
		"commits": []map[string]any{
			{"message": "feat: add B", "files": []map[string]any{{"path": "b.go"}}},
		},
	})

	_, acErr = runPlan(plan2, r, false)
	if acErr != nil {
		t.Fatalf("second runPlan failed: %v", acErr)
	}

	// Verify both commits exist
	logs := readGitLog(t, dir)
	foundA2, foundB := false, false
	for _, l := range logs {
		if l == "feat: add A2" {
			foundA2 = true
		}
		if l == "feat: add B" {
			foundB = true
		}
	}
	if !foundA2 || !foundB {
		t.Fatalf("expected both commits in log, got: %v", logs)
	}
}
