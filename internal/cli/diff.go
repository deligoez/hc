package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/deligoez/hc/internal/diff"
	"github.com/deligoez/hc/internal/git"
	"github.com/deligoez/hc/internal/output"
)

// diffFileJSON is the JSON representation of a file in the diff output.
type diffFileJSON struct {
	Path        string         `json:"path"`
	Hunks       []diffHunkJSON `json:"hunks"`
	IsNew       bool           `json:"is_new,omitempty"`
	IsDeleted   bool           `json:"is_deleted,omitempty"`
	IsRenamed   bool           `json:"is_renamed,omitempty"`
	OldPath     string         `json:"old_path,omitempty"`
	IsBinary    bool           `json:"is_binary,omitempty"`
	IsUntracked bool           `json:"is_untracked,omitempty"`
}

// diffHunkJSON is the JSON representation of a hunk.
type diffHunkJSON struct {
	Index       int    `json:"index"`
	Header      string `json:"header"`
	Section     string `json:"section,omitempty"`
	Added       int64  `json:"added"`
	Deleted     int64  `json:"deleted"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Content     string `json:"content"`
}

// diffSummaryJSON is the JSON summary of the diff.
type diffSummaryJSON struct {
	Files int   `json:"files"`
	Hunks int   `json:"hunks"`
	Added int64 `json:"added"`
	Deleted int64 `json:"deleted"`
}

// diffOutputJSON is the top-level JSON output for the diff command.
type diffOutputJSON struct {
	Files    []diffFileJSON  `json:"files"`
	Summary  diffSummaryJSON `json:"summary"`
	Warnings []string        `json:"warnings,omitempty"`
}

// diffResult is the internal result of computing the diff.
type diffResult struct {
	Files    []diffFileResult
	Warnings []string
}

type diffFileResult struct {
	diff.FileDiff
	IsUntracked bool
}

func newDiffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "diff",
		Short: "Show uncommitted changes with indexed hunks and content",
		RunE: func(cmd *cobra.Command, args []string) error {
			runner := git.NewRunner(".")
			result, err := runDiff(runner)
			if err != nil {
				if acErr, ok := err.(*output.ACError); ok {
					printer.PrintError(acErr)
					return &exitError{code: acErr.Code}
				}
				return err
			}

			// Check for staged changes and warn
			if err := runner.EnsureCleanStaging(); err != nil {
				result.Warnings = append(result.Warnings,
					"staged changes exist; hc diff shows only unstaged changes and hc run will reject them -- run 'git reset HEAD' first")
			}

			if printer.UseJSON() {
				return printDiffJSON(result)
			}
			printDiffTTY(result)
			return nil
		},
	}
}

// runDiff executes the core diff logic and returns the result.
func runDiff(runner *git.Runner) (*diffResult, error) {
	// Ensure we're in a git repository
	if err := runner.EnsureRepo(); err != nil {
		return nil, output.NewValidationError(
			"not a git repository",
			"Run hc from inside a git repository.",
		)
	}

	// Get unstaged diff
	raw, err := runner.Diff("-U0", "-M", "--no-ext-diff")
	if err != nil {
		return nil, output.NewExecutionError(
			fmt.Sprintf("failed to run git diff: %v", err),
			"ensure you are in a git repository",
		)
	}

	files, err := diff.Parse(raw)
	if err != nil {
		return nil, output.NewExecutionError(
			fmt.Sprintf("failed to parse diff: %v", err),
			"",
		)
	}

	// Compute fingerprints for all hunks
	for i := range files {
		for j := range files[i].Hunks {
			files[i].Hunks[j].Fingerprint = diff.Fingerprint(files[i].Hunks[j])
		}
	}

	// Build result from parsed files
	result := &diffResult{}
	for _, f := range files {
		result.Files = append(result.Files, diffFileResult{FileDiff: f})
	}

	// List untracked files
	untrackedOut, err := runner.Run("ls-files", "--others", "--exclude-standard")
	if err != nil {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("cannot list untracked files: %v", err))
	} else {
		for _, line := range strings.Split(strings.TrimSpace(untrackedOut), "\n") {
			if line == "" {
				continue
			}
			result.Files = append(result.Files, diffFileResult{
				FileDiff: diff.FileDiff{
					Path:  line,
					IsNew: true,
				},
				IsUntracked: true,
			})
		}
	}

	if len(result.Files) == 0 {
		return nil, output.NewValidationError(
			"no uncommitted changes",
			"There is nothing to commit.",
		)
	}

	return result, nil
}

func hunkHeader(h diff.Hunk) string {
	return fmt.Sprintf("@@ -%d,%d +%d,%d @@", h.OldStart, h.OldCount, h.NewStart, h.NewCount)
}

// hunkContent renders a hunk's changed lines as a compact diff body:
// one "+"/"-" prefixed line per change, joined with newlines. With -U0
// there are no context lines, so this is exactly the changed content.
func hunkContent(h diff.Hunk) string {
	var b strings.Builder
	for i, l := range h.Lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		switch l.Op {
		case diff.OpAdd:
			b.WriteByte('+')
		case diff.OpDelete:
			b.WriteByte('-')
		default:
			b.WriteByte(' ')
		}
		b.WriteString(strings.TrimSuffix(l.Content, "\n"))
	}
	return b.String()
}

// shortFingerprint truncates a full SHA-256 hex fingerprint for display.
func shortFingerprint(fp string) string {
	if len(fp) > 12 {
		return fp[:12]
	}
	return fp
}

func hunkLineSummary(h diff.Hunk) string {
	if h.OldCount == 0 {
		return fmt.Sprintf("(+%d lines)", h.NewCount)
	}
	if h.NewCount == 0 {
		return fmt.Sprintf("(-%d lines)", h.OldCount)
	}
	return fmt.Sprintf("(-%d +%d lines)", h.OldCount, h.NewCount)
}

func printDiffTTY(result *diffResult) {
	for _, w := range result.Warnings {
		fmt.Fprintln(printer.ErrOut, "warning:", w)
	}

	for i, f := range result.Files {
		if i > 0 {
			fmt.Fprintln(printer.Out)
		}

		suffix := ""
		if f.IsNew {
			suffix = ", new file"
		}
		if f.IsDeleted {
			suffix = ", deleted"
		}

		if f.IsUntracked {
			fmt.Fprintf(printer.Out, "%s (untracked, new file)\n", f.Path)
			continue
		}

		if f.IsBinary {
			fmt.Fprintf(printer.Out, "%s (binary%s)\n", f.Path, suffix)
			continue
		}

		fmt.Fprintf(printer.Out, "%s (%d hunks%s):\n", f.Path, len(f.Hunks), suffix)
		for _, h := range f.Hunks {
			header := hunkHeader(h)
			if h.Section != "" {
				header += " " + h.Section
			}
			fmt.Fprintf(printer.Out, "  [%d] %s  %s\n", h.Index, header, hunkLineSummary(h))
		}
	}
}

func printDiffJSON(result *diffResult) error {
	out := diffOutputJSON{
		Files:    make([]diffFileJSON, 0, len(result.Files)),
		Warnings: result.Warnings,
	}

	for _, f := range result.Files {
		jf := diffFileJSON{
			Path:        f.Path,
			Hunks:       make([]diffHunkJSON, 0, len(f.Hunks)),
			IsNew:       f.IsNew,
			IsDeleted:   f.IsDeleted,
			IsRenamed:   f.IsRenamed,
			OldPath:     f.OldPath,
			IsBinary:    f.IsBinary,
			IsUntracked: f.IsUntracked,
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

		out.Files = append(out.Files, jf)
		out.Summary.Files++
		out.Summary.Hunks += len(f.Hunks)
		for _, h := range f.Hunks {
			out.Summary.Added += h.NewCount
			out.Summary.Deleted += h.OldCount
		}
	}

	return printer.PrintJSON(out)
}
