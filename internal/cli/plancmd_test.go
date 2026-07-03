package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deligoez/hc/internal/git"
	"github.com/deligoez/hc/internal/output"
	"github.com/deligoez/hc/internal/plan"
)

// planDraft mirrors what hc plan emits, built from the same internals.
func planDraft(t *testing.T, r *git.Runner) *plan.Plan {
	t.Helper()
	result, err := runDiff(r)
	must(t, err)
	p := &plan.Plan{}
	for _, f := range result.Files {
		groups := groupHunksBySection(f.Hunks)
		if len(groups) > 1 && !f.IsBinary && !f.IsDeleted {
			for _, g := range groups {
				p.Commits = append(p.Commits, plan.Commit{
					Message: "TODO (" + f.Path + ": " + g.section + ")",
					Files:   []plan.FileEntry{{Path: f.Path, Hunks: g.indices}},
				})
			}
			continue
		}
		p.Commits = append(p.Commits, plan.Commit{
			Message: "TODO (" + f.Path + ")",
			Files:   []plan.FileEntry{{Path: f.Path}},
		})
	}
	for _, path := range result.Untracked {
		p.Commits = append(p.Commits, plan.Commit{
			Message: "TODO (new file " + path + ")",
			Files:   []plan.FileEntry{{Path: path}},
		})
	}
	return p
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
