package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deligoez/hc/internal/diff"
	"github.com/deligoez/hc/internal/git"
	"github.com/deligoez/hc/internal/output"
	"github.com/deligoez/hc/internal/plan"
)

// planDraft mirrors what hc plan emits, built from the same internals.
func planDraft(t *testing.T, r *git.Runner) *plan.Plan {
	t.Helper()
	result, err := runDiff(r)
	must(t, err)
	p, _ := buildDraftPlan(result)
	return p
}

// TestIsTestFileHeuristic covers the filename and directory conventions that
// mark a draft entry as test code.
func TestIsTestFileHeuristic(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"internal/cli/plancmd_test.go", true},
		{"src/auth.spec.ts", true},
		{"src/auth.test.js", true},
		{"tests/Feature/LoginTest.php", true},
		{"app/Services/PaymentTest.php", true},
		{"test_models.py", true},
		{"pkg/testdata/fixture.json", true},
		{"spec/models/user_spec.rb", true},
		{"src/__tests__/util.js", true},
		{"internal/cli/plancmd.go", false},
		{"spec/0.2.0.md", false},
		{"docs/contest.php", false},
		{"src/attest.go", false},
		{"latest.config.js", false},
	}
	for _, c := range cases {
		if got := isTestFile(c.path); got != c.want {
			t.Errorf("isTestFile(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

// TestPlanDraftForcesReviewThenRunsGranular exercises the whole forcing
// function: the draft splits a multi-section file by section and carries TODO
// messages; hc run REFUSES the unedited draft; with real messages the exact
// same structure commits granularly.
func TestPlanDraftForcesReviewThenRunsGranular(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)

	base := "package m\n\nfunc region() {\n\ta := 1\n\t_ = a\n}\n\nfunc guard() bool {\n\treturn true\n}\n"
	must(t, os.WriteFile(filepath.Join(dir, "machine.go"), []byte(base), 0o644))
	must(t, run(r, "add", "-A"))
	must(t, run(r, "commit", "-qm", "base"))

	mut := strings.ReplaceAll(base, "a := 1", "a := 42")
	mut = strings.ReplaceAll(mut, "return true", "return false")
	must(t, os.WriteFile(filepath.Join(dir, "machine.go"), []byte(mut), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("n\n"), 0o644))

	draft := planDraft(t, r)
	// Two section groups for machine.go + one untracked entry.
	if len(draft.Commits) != 3 {
		t.Fatalf("draft = %+v, want 3 entries", draft.Commits)
	}
	if !strings.Contains(draft.Commits[0].Message, "region") || !strings.Contains(draft.Commits[1].Message, "guard") {
		t.Fatalf("section labels missing in draft: %+v", draft.Commits)
	}

	// Unedited draft is refused: TODO messages never reach git.
	raw, err := json.Marshal(draft)
	must(t, err)
	_, acErr := runPlan(raw, r, false)
	if acErr == nil || acErr.Code != 2 || !strings.Contains(acErr.Message, "unedited draft") {
		t.Fatalf("unedited draft should be refused, got %v", acErr)
	}

	// With real messages the same structure runs and commits granularly.
	draft.Commits[0].Message = "feat: bump region constant"
	draft.Commits[1].Message = "fix: flip guard"
	draft.Commits[2].Message = "docs: add notes"
	raw, err = json.Marshal(draft)
	must(t, err)
	res, acErr := runPlan(raw, r, false)
	if acErr != nil {
		t.Fatalf("edited draft failed: %v | %s", acErr, acErr.Hint)
	}
	if res.(*output.Result).Committed != 3 {
		t.Fatalf("want 3 commits, got %+v", res)
	}
	if st, _ := r.Run("status", "--porcelain"); strings.TrimSpace(st) != "" {
		t.Fatalf("tree not clean:\n%s", st)
	}
}

// TestMultiSectionBundleWarns guards the advisory backstop for hand-written
// plans that lump a multi-section file into one commit.
func TestMultiSectionBundleWarns(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)

	base := "package m\n\nfunc region() {\n\ta := 1\n\t_ = a\n}\n\nfunc guard() bool {\n\treturn true\n}\n"
	must(t, os.WriteFile(filepath.Join(dir, "machine.go"), []byte(base), 0o644))
	must(t, run(r, "add", "-A"))
	must(t, run(r, "commit", "-qm", "base"))
	mut := strings.ReplaceAll(base, "a := 1", "a := 42")
	mut = strings.ReplaceAll(mut, "return true", "return false")
	must(t, os.WriteFile(filepath.Join(dir, "machine.go"), []byte(mut), 0o644))

	res, acErr := runPlan([]byte(`{"commits":[{"message":"feat: everything at once","files":[{"path":"machine.go"}]}]}`), r, false)
	if acErr != nil {
		t.Fatalf("run failed: %v", acErr)
	}
	result := res.(*output.Result)
	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "review granularity") && strings.Contains(w, "machine.go") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a granularity warning, got %v", result.Warnings)
	}
}

// TestGapFallbackGrouping covers the signal hierarchy's second tier: files
// where sections cannot discriminate (configs, plain text) group by
// contiguity gaps instead -- exactly the "10-20 and 30-34 are probably two
// ideas, 2-8 and 12-15 probably one" intuition.
func TestGapFallbackGrouping(t *testing.T) {
	mk := func(oldStart, oldCount int64) diffHunkForTest {
		return diffHunkForTest{oldStart, oldCount}
	}
	cases := []struct {
		name   string
		hunks  []diffHunkForTest
		groups int
	}{
		{"far regions split", []diffHunkForTest{mk(10, 11), mk(30, 5)}, 2}, // gap 9 > 8
		{"near regions stay", []diffHunkForTest{mk(2, 7), mk(12, 4)}, 0},   // gap 3 <= 8 -> single group -> no proposal
		{"three regions", []diffHunkForTest{mk(5, 3), mk(40, 2), mk(90, 1)}, 3},
	}
	for _, tc := range cases {
		got := groupHunksBySection(buildSectionlessHunks(tc.hunks))
		if tc.groups == 0 {
			if got != nil {
				t.Errorf("%s: want no proposal, got %+v", tc.name, got)
			}
			continue
		}
		if len(got) != tc.groups {
			t.Errorf("%s: groups = %d, want %d (%+v)", tc.name, len(got), tc.groups, got)
		}
		if tc.groups > 0 && got != nil && !strings.HasPrefix(got[0].section, "lines ") {
			t.Errorf("%s: gap groups should be labeled by line span, got %q", tc.name, got[0].section)
		}
	}

	// Scattered-many pattern (lock files): no proposal at all.
	var many []diffHunkForTest
	for i := 0; i < 9; i++ {
		many = append(many, mk(int64(10+i*50), 2))
	}
	if got := groupHunksBySection(buildSectionlessHunks(many)); got != nil {
		t.Errorf("scattered-many file must not be gap-split, got %d groups", len(got))
	}
}

type diffHunkForTest struct{ oldStart, oldCount int64 }

func buildSectionlessHunks(specs []diffHunkForTest) []diff.Hunk {
	var hunks []diff.Hunk
	for i, s := range specs {
		hunks = append(hunks, diff.Hunk{Index: i, OldStart: s.oldStart, OldCount: s.oldCount})
	}
	return hunks
}

// TestHugeHunkContentTruncated guards the oversized-hunk cap: a giant
// contiguous replacement must not inline its full body into the JSON.
func TestHugeHunkContentTruncated(t *testing.T) {
	var lines []diff.Line
	for i := 0; i < 5000; i++ {
		lines = append(lines, diff.Line{Op: diff.OpAdd, Content: "added line\n"})
	}
	content, omitted := hunkContent(diff.Hunk{Lines: lines})
	if omitted != 5000-80-10 {
		t.Fatalf("omitted = %d", omitted)
	}
	if !strings.Contains(content, "[hc: 4910 lines omitted]") {
		t.Fatal("marker missing")
	}
	if strings.Count(content, "\n") > 100 {
		t.Fatalf("content still too large: %d lines", strings.Count(content, "\n"))
	}

	// Small hunks stay whole.
	small, om := hunkContent(diff.Hunk{Lines: lines[:5]})
	if om != 0 || strings.Contains(small, "omitted") {
		t.Fatal("small hunk must not be truncated")
	}
}

// TestPlainTextFilesUseGapFallback guards the fix for synthetic sections:
// git's default funcname makes any non-indented line a "section" for text
// files, which must NOT drive grouping -- otherwise 12 scattered edits in a
// text file explode into 12 draft entries and the scattered-many guard is
// dead code.
func TestPlainTextFilesUseGapFallback(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)

	var rows []string
	for i := 0; i < 2000; i++ {
		rows = append(rows, fmt.Sprintf("row-%05d", i))
	}
	must(t, os.WriteFile(filepath.Join(dir, "data.txt"), []byte(strings.Join(rows, "\n")+"\n"), 0o644))
	must(t, run(r, "add", "-A"))
	must(t, run(r, "commit", "-qm", "base"))

	// (a) 12 scattered edits: no split proposal, single file entry.
	mut := append([]string(nil), rows...)
	for i := 0; i < 12; i++ {
		mut[50+i*150] = fmt.Sprintf("edited-%d", i)
	}
	must(t, os.WriteFile(filepath.Join(dir, "data.txt"), []byte(strings.Join(mut, "\n")+"\n"), 0o644))
	draft := planDraft(t, r)
	if len(draft.Commits) != 1 || draft.Commits[0].Files[0].Hunks != nil {
		t.Fatalf("scattered-many text file should stay one whole-file entry, got %+v", draft.Commits)
	}
	// And its sections array must be empty (synthetic labels filtered).
	res, err := runDiff(r)
	must(t, err)
	if labels := 0; true {
		for _, h := range res.Files[0].Hunks {
			if sectionLabel(h.Section) != "" {
				labels++
			}
		}
		if labels != 0 {
			t.Fatalf("synthetic text sections should be filtered, found %d", labels)
		}
	}

	// (b) 3 far-apart edits: gap fallback proposes 3 groups labeled by span.
	mut = append([]string(nil), rows...)
	mut[100] = "far-a"
	mut[900] = "far-b"
	mut[1700] = "far-c"
	must(t, os.WriteFile(filepath.Join(dir, "data.txt"), []byte(strings.Join(mut, "\n")+"\n"), 0o644))
	draft = planDraft(t, r)
	if len(draft.Commits) != 3 {
		t.Fatalf("3 far regions should yield 3 gap groups, got %+v", draft.Commits)
	}
	if !strings.Contains(draft.Commits[0].Message, "lines ") {
		t.Fatalf("gap groups should be labeled by line span: %q", draft.Commits[0].Message)
	}
}

// TestPlanDraftLabelsTestFiles verifies test files get "TODO test" draft
// entries (tracked and untracked) while non-test files keep plain "TODO".
func TestPlanDraftLabelsTestFiles(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)

	must(t, os.WriteFile(filepath.Join(dir, "auth.go"), []byte("package m\n\nfunc a() {}\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "auth_test.go"), []byte("package m\n\nfunc TestA(t *T) {}\n"), 0o644))
	must(t, run(r, "add", "-A"))
	must(t, run(r, "commit", "-qm", "base"))

	must(t, os.WriteFile(filepath.Join(dir, "auth.go"), []byte("package m\n\nfunc a() { _ = 1 }\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "auth_test.go"), []byte("package m\n\nfunc TestA(t *T) { _ = 1 }\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "handler.spec.ts"), []byte("it('x', () => {})\n"), 0o644))

	result, err := runDiff(r)
	must(t, err)
	draft, hasTests := buildDraftPlan(result)
	if !hasTests {
		t.Fatalf("hasTestEntries = false, want true")
	}
	byPath := map[string]string{}
	for _, c := range draft.Commits {
		byPath[c.Files[0].Path] = c.Message
	}
	if !strings.HasPrefix(byPath["auth.go"], "TODO (") {
		t.Errorf("auth.go message = %q, want plain TODO", byPath["auth.go"])
	}
	if !strings.HasPrefix(byPath["auth_test.go"], "TODO test (") {
		t.Errorf("auth_test.go message = %q, want TODO test", byPath["auth_test.go"])
	}
	if !strings.HasPrefix(byPath["handler.spec.ts"], "TODO test (new file ") {
		t.Errorf("handler.spec.ts message = %q, want TODO test (new file ...)", byPath["handler.spec.ts"])
	}
}
