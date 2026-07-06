package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/deligoez/hc/internal/output"
	"github.com/deligoez/hc/internal/plan"
)

func newPlanCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "plan",
		Short: "Emit a draft commit plan for the working tree (file-first, section-split)",
		Long: "Generates the default granular plan for the current working tree: one commit per\n" +
			"file, further split by enclosing section when a file's hunks span several. Every\n" +
			"message is a TODO placeholder -- 'hc run' refuses TODO messages, so each draft entry\n" +
			"must be reviewed and given a real message. Merging entries that belong together\n" +
			"(mechanical sweeps, inseparable changes, one idea across sections) is the review's\n" +
			"job; the draft only proposes the finest mechanical split.",
		RunE: func(cmd *cobra.Command, args []string) error {
			runner, acErr := newRepoRunner()
			if acErr != nil {
				printer.PrintError(acErr)
				return &exitError{code: acErr.Code}
			}

			result, err := runDiff(runner)
			if err != nil {
				if acErr, ok := err.(*output.ACError); ok {
					printer.PrintError(acErr)
					return &exitError{code: acErr.Code}
				}
				return err
			}

			p, hasTestEntries := buildDraftPlan(result)

			if len(p.Commits) == 0 {
				acErr := output.NewValidationError(
					"no uncommitted changes",
					"There is nothing to plan.",
				)
				printer.PrintError(acErr)
				return &exitError{code: acErr.Code}
			}

			// Guidance on stderr; stdout stays a pure, editable plan. Every
			// TODO must be replaced -- hc run enforces it.
			guidance := "draft plan: %d commit(s). Review it: write a real message for every TODO, MERGE entries that are one idea (sweeps, inseparable changes), drop untracked files you don't want. Then run: hc run <plan>\n"
			if hasTestEntries {
				guidance = "draft plan: %d commit(s). Review it: write a real message for every TODO, MERGE entries that are one idea (sweeps, inseparable changes), drop untracked files you don't want. 'TODO test' entries are test code: keep one commit per NEW test; merge only edits to existing tests driven by one context. Then run: hc run <plan>\n"
			}
			fmt.Fprintf(printer.ErrOut, guidance, len(p.Commits))
			return printer.PrintJSON(p)
		},
	}
}

// buildDraftPlan turns a diff result into the draft plan hc plan emits: one
// commit per file, section-split when a file's hunks span several, TODO
// placeholder messages. Test files (per isTestFile) are labeled "TODO test";
// the second return reports whether any such entry exists.
func buildDraftPlan(result *diffResult) (*plan.Plan, bool) {
	p := &plan.Plan{}
	hasTestEntries := false
	todoFor := func(path string) string {
		if isTestFile(path) {
			hasTestEntries = true
			return "TODO test"
		}
		return "TODO"
	}
	for _, f := range result.Files {
		todo := todoFor(f.Path)
		groups := groupHunksBySection(f.Hunks)
		if len(groups) > 1 && !f.IsBinary && !f.IsDeleted {
			for _, g := range groups {
				p.Commits = append(p.Commits, plan.Commit{
					Message: fmt.Sprintf("%s (%s: %s)", todo, f.Path, g.section),
					Files:   []plan.FileEntry{{Path: f.Path, Hunks: g.indices}},
				})
			}
			continue
		}
		p.Commits = append(p.Commits, plan.Commit{
			Message: fmt.Sprintf("%s (%s)", todo, f.Path),
			Files:   []plan.FileEntry{{Path: f.Path}},
		})
	}
	for _, path := range result.Untracked {
		p.Commits = append(p.Commits, plan.Commit{
			Message: fmt.Sprintf("%s (new file %s)", todoFor(path), path),
			Files:   []plan.FileEntry{{Path: path}},
		})
	}
	return p, hasTestEntries
}

// isTestFile reports whether a repo-relative path looks like test code, by
// filename convention or by living under a conventional test directory.
// "spec/" dirs are deliberately absent: Ruby specs match via *_spec.rb, while
// a docs "spec/" directory (like hc's own) is not test code.
func isTestFile(path string) bool {
	slashed := "/" + strings.ToLower(filepath.ToSlash(path))
	for _, dir := range []string{"/__tests__/", "/tests/", "/test/", "/testdata/"} {
		if strings.Contains(slashed, dir) {
			return true
		}
	}
	base := path[strings.LastIndex(path, "/")+1:] // original case
	if strings.HasSuffix(base, "Test.php") || strings.HasSuffix(base, "Tests.php") {
		return true
	}
	lower := strings.ToLower(base)
	for _, suffix := range []string{"_test.go", "_test.py", "_test.rb", "_spec.rb", "_test.exs", ".t.sol"} {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	if strings.HasPrefix(lower, "test_") && strings.HasSuffix(lower, ".py") {
		return true
	}
	for _, marker := range []string{".test.", ".spec."} { // foo.test.ts, foo.spec.js
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
