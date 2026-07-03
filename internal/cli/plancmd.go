package cli

import (
	"fmt"

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

			p := &plan.Plan{}
			for _, f := range result.Files {
				groups := groupHunksBySection(f.Hunks)
				if len(groups) > 1 && !f.IsBinary && !f.IsDeleted {
					for _, g := range groups {
						p.Commits = append(p.Commits, plan.Commit{
							Message: fmt.Sprintf("TODO (%s: %s)", f.Path, g.section),
							Files:   []plan.FileEntry{{Path: f.Path, Hunks: g.indices}},
						})
					}
					continue
				}
				p.Commits = append(p.Commits, plan.Commit{
					Message: fmt.Sprintf("TODO (%s)", f.Path),
					Files:   []plan.FileEntry{{Path: f.Path}},
				})
			}
			for _, path := range result.Untracked {
				p.Commits = append(p.Commits, plan.Commit{
					Message: fmt.Sprintf("TODO (new file %s)", path),
					Files:   []plan.FileEntry{{Path: path}},
				})
			}

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
			fmt.Fprintf(printer.ErrOut,
				"draft plan: %d commit(s). Review it: write a real message for every TODO, MERGE entries that are one idea (sweeps, inseparable changes), drop untracked files you don't want. Then run: hc run <plan>\n",
				len(p.Commits))
			return printer.PrintJSON(p)
		},
	}
}
