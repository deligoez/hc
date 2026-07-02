package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/deligoez/hc/internal/diff"
	"github.com/deligoez/hc/internal/git"
	"github.com/deligoez/hc/internal/output"
)

// logCommitJSON is one commit in hc log output.
type logCommitJSON struct {
	SHA     string         `json:"sha"`
	Subject string         `json:"subject"`
	Message string         `json:"message,omitempty"` // body beyond the subject
	Merge   bool           `json:"merge,omitempty"`   // merge commits cannot be rewritten
	Files   []diffFileJSON `json:"files"`
}

type logOutputJSON struct {
	Commits []logCommitJSON `json:"commits"`
	Summary struct {
		Commits int `json:"commits"`
		Hunks   int `json:"hunks"`
	} `json:"summary"`
}

func newLogCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "log <base>..<head>",
		Short: "Show per-commit indexed hunks for a range (input for hc rewrite)",
		Long: "Lists every commit in the range (oldest first) with the same indexed, content-carrying\n" +
			"hunk view as 'hc diff', diffed against each commit's first parent. Use it to write a\n" +
			"rewrite plan for 'hc rewrite'.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runner, acErr := newRepoRunner()
			if acErr != nil {
				printer.PrintError(acErr)
				return &exitError{code: acErr.Code}
			}

			result, acErr := runLog(runner, args[0])
			if acErr != nil {
				printer.PrintError(acErr)
				return &exitError{code: acErr.Code}
			}

			if printer.UseJSON() {
				return printer.PrintJSON(result)
			}
			printLogTTY(result)
			return nil
		},
	}
}

func runLog(runner *git.Runner, rangeArg string) (*logOutputJSON, *output.ACError) {
	revsOut, err := runner.Run("rev-list", "--reverse", "--first-parent", rangeArg)
	if err != nil {
		return nil, output.NewValidationError(
			fmt.Sprintf("cannot resolve range %q: %v", rangeArg, err),
			"Use a git range like main..HEAD or abc123..HEAD.",
		)
	}
	revs := strings.Fields(revsOut)
	if len(revs) == 0 {
		return nil, output.NewValidationError(
			fmt.Sprintf("range %q contains no commits", rangeArg),
			"There is nothing to list.",
		)
	}

	out := &logOutputJSON{Commits: make([]logCommitJSON, 0, len(revs))}
	for _, sha := range revs {
		ci, err := runner.ReadCommit(sha)
		if err != nil {
			return nil, output.NewExecutionError(
				fmt.Sprintf("cannot read commit %s: %v", sha, err), "")
		}

		subject, body := splitMessage(ci.Message)
		lc := logCommitJSON{
			SHA:     shortSHA(ci.SHA),
			Subject: subject,
			Message: body,
			Files:   []diffFileJSON{},
		}

		if len(ci.Parents) != 1 {
			lc.Merge = len(ci.Parents) > 1
			out.Commits = append(out.Commits, lc)
			continue
		}

		raw, err := runner.DiffCommit(sha)
		if err != nil {
			return nil, output.NewExecutionError(
				fmt.Sprintf("cannot diff commit %s: %v", sha, err), "")
		}
		files, err := diff.Parse(raw)
		if err != nil {
			return nil, output.NewExecutionError(
				fmt.Sprintf("cannot parse diff of %s: %v", sha, err), "")
		}
		for i := range files {
			for j := range files[i].Hunks {
				files[i].Hunks[j].Fingerprint = diff.Fingerprint(files[i].Hunks[j])
			}
			lc.Files = append(lc.Files, fileToJSON(files[i]))
			out.Summary.Hunks += len(files[i].Hunks)
		}
		out.Commits = append(out.Commits, lc)
	}
	out.Summary.Commits = len(out.Commits)
	return out, nil
}

// splitMessage separates a raw commit message into subject and remaining body.
func splitMessage(msg string) (string, string) {
	parts := strings.SplitN(msg, "\n", 2)
	subject := strings.TrimSpace(parts[0])
	if len(parts) == 1 {
		return subject, ""
	}
	return subject, strings.TrimSpace(parts[1])
}

func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// fileToJSON converts a parsed FileDiff into the shared JSON shape.
func fileToJSON(f diff.FileDiff) diffFileJSON {
	jf := diffFileJSON{
		Path:      f.Path,
		Hunks:     make([]diffHunkJSON, 0, len(f.Hunks)),
		IsNew:     f.IsNew,
		IsDeleted: f.IsDeleted,
		IsRenamed: f.IsRenamed,
		OldPath:   f.OldPath,
		IsBinary:  f.IsBinary,
	}
	for _, h := range f.Hunks {
		jf.Hunks = append(jf.Hunks, diffHunkJSON{
			Index:       h.Index,
			Header:      hunkHeader(h),
			Section:     h.Section,
			Added:       h.NewCount,
			Deleted:     h.OldCount,
			Fingerprint: shortFingerprint(h.Fingerprint),
			Content:     hunkContent(h),
		})
	}
	return jf
}

func printLogTTY(result *logOutputJSON) {
	for i, c := range result.Commits {
		if i > 0 {
			fmt.Fprintln(printer.Out)
		}
		marker := ""
		if c.Merge {
			marker = " (merge -- cannot be rewritten)"
		}
		fmt.Fprintf(printer.Out, "%s %s%s\n", c.SHA, c.Subject, marker)
		for _, f := range c.Files {
			fmt.Fprintf(printer.Out, "  %s (%d hunks)\n", f.Path, len(f.Hunks))
			for _, h := range f.Hunks {
				header := h.Header
				if h.Section != "" {
					header += " " + h.Section
				}
				fmt.Fprintf(printer.Out, "    [%d] %s\n", h.Index, header)
			}
		}
	}
}
