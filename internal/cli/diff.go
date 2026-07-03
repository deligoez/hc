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
	Path  string         `json:"path"`
	Hunks []diffHunkJSON `json:"hunks"`
	// HunkCount replaces hunk bodies in hc log --files-only output.
	HunkCount int `json:"hunk_count,omitempty"`
	// Sections lists the distinct enclosing sections the file's hunks touch,
	// in order. More than one usually means more than one idea: plan
	// hunk-level splits instead of bundling the whole file.
	Sections  []string `json:"sections,omitempty"`
	IsNew     bool     `json:"is_new,omitempty"`
	IsDeleted bool     `json:"is_deleted,omitempty"`
	IsRenamed bool     `json:"is_renamed,omitempty"`
	OldPath   string   `json:"old_path,omitempty"`
	IsBinary  bool     `json:"is_binary,omitempty"`
	// IsIntentToAdd marks files that appear in the diff only because of a
	// git add -N index entry. hc run skips them from coverage validation
	// unless the plan references them. Plain untracked files are listed in
	// the top-level untracked array instead.
	IsIntentToAdd bool `json:"is_intent_to_add,omitempty"`
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
	Files   int   `json:"files"`
	Hunks   int   `json:"hunks"`
	Added   int64 `json:"added"`
	Deleted int64 `json:"deleted"`
}

// diffOutputJSON is the top-level JSON output for the diff command.
type diffOutputJSON struct {
	Files []diffFileJSON `json:"files"`
	// Untracked lists plain untracked paths compactly. They carry no hunks,
	// never enter coverage validation, and are committed only when a plan
	// references their path -- so they are kept out of files to avoid
	// reading as "changes that must be planned".
	Untracked []string        `json:"untracked,omitempty"`
	Summary   diffSummaryJSON `json:"summary"`
	Warnings  []string        `json:"warnings,omitempty"`
}

// diffResult is the internal result of computing the diff.
type diffResult struct {
	Files     []diff.FileDiff
	Untracked []string
	Warnings  []string
}

func newDiffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "diff",
		Short: "Show uncommitted changes with indexed hunks and content",
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
	raw, err := runner.Diff("-U0", "--no-renames", "--no-ext-diff")
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
	result := &diffResult{Files: files}

	// List untracked files (compact: paths only; they carry no hunks and
	// never enter coverage validation)
	untrackedOut, err := runner.Run("ls-files", "--others", "--exclude-standard")
	if err != nil {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("cannot list untracked files: %v", err))
	} else {
		for _, line := range strings.Split(strings.TrimSpace(untrackedOut), "\n") {
			if line == "" {
				continue
			}
			result.Untracked = append(result.Untracked, line)
		}
	}

	if len(result.Files) == 0 && len(result.Untracked) == 0 {
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

	for i, p := range result.Untracked {
		if i == 0 && len(result.Files) > 0 {
			fmt.Fprintln(printer.Out)
		}
		fmt.Fprintf(printer.Out, "%s (untracked, new file)\n", p)
	}
}

func printDiffJSON(result *diffResult) error {
	out := diffOutputJSON{
		Files:     make([]diffFileJSON, 0, len(result.Files)),
		Untracked: result.Untracked,
		Warnings:  result.Warnings,
	}

	for _, f := range result.Files {
		jf := diffFileJSON{
			Path:      f.Path,
			Hunks:     make([]diffHunkJSON, 0, len(f.Hunks)),
			IsNew:     f.IsNew,
			IsDeleted: f.IsDeleted,
			IsRenamed: f.IsRenamed,
			OldPath:   f.OldPath,
			IsBinary:  f.IsBinary,
			// In the unstaged diff, a tracked-new file can only appear via
			// an intent-to-add index entry (plain untracked files are listed
			// in the top-level untracked array instead).
			IsIntentToAdd: f.IsNew,
		}

		seenSections := map[string]bool{}
		for _, h := range f.Hunks {
			if label := sectionLabel(h.Section); label != "" && !seenSections[label] {
				seenSections[label] = true
				jf.Sections = append(jf.Sections, label)
			}
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
