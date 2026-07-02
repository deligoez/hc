package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/deligoez/hc/internal/diff"
	"github.com/deligoez/hc/internal/git"
	"github.com/deligoez/hc/internal/output"
	"github.com/deligoez/hc/internal/plan"
)

func newSplitCmd() *cobra.Command {
	var template string

	cmd := &cobra.Command{
		Use:   "split <base>..<head>",
		Short: "Emit a draft one-file-per-commit rewrite plan for a range",
		Long: "Generates the default file-first rewrite plan: every multi-file, non-merge commit in\n" +
			"the range is split into one commit per file; single-file commits and merges are left\n" +
			"out (they stay as they are). The plan is printed, NOT applied -- review it (delete\n" +
			"rewrites that are mechanical sweeps, refine messages, add within-file hunk splits),\n" +
			"then pipe it to 'hc rewrite -'.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runner, acErr := newRepoRunner()
			if acErr != nil {
				printer.PrintError(acErr)
				return &exitError{code: acErr.Code}
			}

			rp, skipped, acErr := runSplit(runner, args[0], template)
			if acErr != nil {
				printer.PrintError(acErr)
				return &exitError{code: acErr.Code}
			}

			// Guidance goes to stderr; stdout stays a pure, pipeable plan.
			fmt.Fprintf(printer.ErrOut,
				"draft plan: %d commit(s) split; %d left as-is. Review before applying: delete rewrites that are mechanical sweeps, then pipe to 'hc rewrite -'.\n",
				len(rp.Rewrites), skipped)
			return printer.PrintJSON(rp)
		},
	}

	cmd.Flags().StringVar(&template, "message-template", "{subject} ({basename})",
		"Replacement message template; placeholders: {subject} {path} {basename} {dir}")

	return cmd
}

// runSplit builds the draft rewrite plan for a range.
func runSplit(runner *git.Runner, rangeArg, template string) (*plan.RewritePlan, int, *output.ACError) {
	revsOut, err := runner.Run("rev-list", "--reverse", "--first-parent", rangeArg)
	if err != nil {
		return nil, 0, output.NewValidationError(
			fmt.Sprintf("cannot resolve range %q: %v", rangeArg, err),
			"Use a git range like main..HEAD or abc123..HEAD.",
		)
	}
	revs := strings.Fields(revsOut)
	if len(revs) == 0 {
		return nil, 0, output.NewValidationError(
			fmt.Sprintf("range %q contains no commits", rangeArg),
			"There is nothing to split.",
		)
	}

	rp := &plan.RewritePlan{}
	skipped := 0
	for _, sha := range revs {
		ci, err := runner.ReadCommit(sha)
		if err != nil {
			return nil, 0, output.NewExecutionError(
				fmt.Sprintf("cannot read commit %s: %v", sha, err), "")
		}
		if len(ci.Parents) != 1 {
			skipped++ // merges and roots cannot be split
			continue
		}

		raw, err := runner.DiffCommit(sha)
		if err != nil {
			return nil, 0, output.NewExecutionError(
				fmt.Sprintf("cannot diff commit %s: %v", sha, err), "")
		}
		files, err := diff.Parse(raw)
		if err != nil {
			return nil, 0, output.NewExecutionError(
				fmt.Sprintf("cannot parse diff of %s: %v", sha, err), "")
		}
		if len(files) < 2 {
			skipped++ // already single-file (or empty): nothing to split
			continue
		}

		subject, _ := splitMessage(ci.Message)
		rw := plan.Rewrite{Commit: shortSHA(ci.SHA)}
		for _, fd := range files {
			rw.Commits = append(rw.Commits, plan.Commit{
				Message: renderSplitMessage(template, subject, fd.Path),
				Files:   []plan.FileEntry{{Path: fd.Path}},
			})
		}
		rp.Rewrites = append(rp.Rewrites, rw)
	}

	if len(rp.Rewrites) == 0 {
		return nil, 0, output.NewValidationError(
			"no multi-file commits to split in the range",
			"Every commit in the range is already single-file, a merge, or empty.",
		)
	}
	return rp, skipped, nil
}

// renderSplitMessage fills the message template. Messages stay opaque: the
// only placeholders are the original subject and the file path pieces.
func renderSplitMessage(template, subject, path string) string {
	msg := strings.ReplaceAll(template, "{subject}", subject)
	msg = strings.ReplaceAll(msg, "{path}", path)
	msg = strings.ReplaceAll(msg, "{basename}", filepath.Base(path))
	msg = strings.ReplaceAll(msg, "{dir}", filepath.Dir(path))
	return msg
}
