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
	var hunksMode bool

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

			rp, skipped, acErr := runSplit(runner, args[0], template, hunksMode)
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

	cmd.Flags().StringVar(&template, "message-template", "",
		"Replacement message template; placeholders: {subject} {path} {basename} {dir} {section}")
	cmd.Flags().BoolVar(&hunksMode, "hunks", false,
		"Also propose within-file splits: hunks grouped by their enclosing section (draft heuristic -- review it)")

	return cmd
}

// runSplit builds the draft rewrite plan for a range. In hunksMode, files
// whose hunks span multiple sections are additionally split hunk-by-section.
func runSplit(runner *git.Runner, rangeArg, template string, hunksMode bool) (*plan.RewritePlan, int, *output.ACError) {
	if template == "" {
		template = "{subject} ({basename})"
		if hunksMode {
			template = "{subject} ({basename}: {section})"
		}
	}
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
		subject, _ := splitMessage(ci.Message)
		rw := plan.Rewrite{Commit: shortSHA(ci.SHA)}
		for _, fd := range files {
			if hunksMode && !fd.IsBinary && !fd.IsDeleted {
				if groups := groupHunksBySection(fd.Hunks); len(groups) > 1 {
					for _, g := range groups {
						rw.Commits = append(rw.Commits, plan.Commit{
							Message: renderSplitMessage(template, subject, fd.Path, g.section),
							Files:   []plan.FileEntry{{Path: fd.Path, Hunks: g.indices}},
						})
					}
					continue
				}
			}
			rw.Commits = append(rw.Commits, plan.Commit{
				Message: renderSplitMessage(template, subject, fd.Path, ""),
				Files:   []plan.FileEntry{{Path: fd.Path}},
			})
		}
		if len(rw.Commits) < 2 {
			skipped++ // nothing worth splitting in this commit
			continue
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
// only placeholders are the original subject, file path pieces and the
// (possibly empty) section label.
func renderSplitMessage(template, subject, path, section string) string {
	msg := strings.ReplaceAll(template, "{subject}", subject)
	msg = strings.ReplaceAll(msg, "{path}", path)
	msg = strings.ReplaceAll(msg, "{basename}", filepath.Base(path))
	msg = strings.ReplaceAll(msg, "{dir}", filepath.Dir(path))
	if section == "" {
		// Collapse the placeholder (and a dangling separator) gracefully.
		msg = strings.ReplaceAll(msg, ": {section}", "")
		msg = strings.ReplaceAll(msg, "{section}", filepath.Base(path))
	} else {
		msg = strings.ReplaceAll(msg, "{section}", section)
	}
	return msg
}

// hunkGroup is a set of hunk indices sharing one enclosing section.
type hunkGroup struct {
	section string
	indices []int
}

// Gap-fallback tuning: when section labels cannot discriminate, hunks
// separated by more than sectionGapThreshold unchanged lines are proposed as
// separate groups -- but only for files with at most maxGapSplitHunks hunks.
// Many scattered hunks in one file (lock files, generated output, snapshots)
// almost always belong to ONE mechanical change, and exploding them into
// draft entries would be pure review noise.
const (
	sectionGapThreshold = 8
	maxGapSplitHunks    = 6
)

// groupHunksBySection groups a file's hunks into split proposals using a
// signal hierarchy: enclosing sections first, contiguity gaps as fallback.
//
//  1. Section labels (git's function-context, trimmed) discriminate when at
//     least two distinct non-empty labels exist. Hunks with no label inherit
//     the NEXT hunk's label (imports precede the code that needs them).
//     Nearby hunks in one section stay together -- same-function changes are
//     usually one idea.
//  2. When labels cannot discriminate (config files, plain text, top-level
//     code), hunks separated by more than sectionGapThreshold unchanged
//     lines become separate groups labeled by their line span. Non-adjacent
//     regions of a sectionless file are usually separate ideas.
//
// The grouping is a draft heuristic for hc plan / hc split --hunks; the
// reviewing agent merges or refines it.
func groupHunksBySection(hunks []diff.Hunk) []hunkGroup {
	if len(hunks) < 2 {
		return nil
	}

	labels := make([]string, len(hunks))
	distinct := map[string]bool{}
	next := ""
	for i := len(hunks) - 1; i >= 0; i-- {
		if s := sectionLabel(hunks[i].Section); s != "" {
			next = s
		}
		labels[i] = next
	}
	for _, l := range labels {
		if l != "" {
			distinct[l] = true
		}
	}

	// Tier 1: sections discriminate.
	if len(distinct) >= 2 {
		var groups []hunkGroup
		pos := map[string]int{}
		for i, h := range hunks {
			if gi, ok := pos[labels[i]]; ok {
				groups[gi].indices = append(groups[gi].indices, h.Index)
				continue
			}
			pos[labels[i]] = len(groups)
			groups = append(groups, hunkGroup{section: labels[i], indices: []int{h.Index}})
		}
		return groups
	}

	// Tier 2: gap-based fallback for undiscriminating labels.
	if len(hunks) > maxGapSplitHunks {
		return nil // scattered-many pattern: almost certainly one mechanical change
	}
	var groups [][]diff.Hunk
	for i, h := range hunks {
		if i > 0 {
			prev := hunks[i-1]
			prevEnd := prev.OldStart
			if prev.OldCount > 0 {
				prevEnd = prev.OldStart + prev.OldCount - 1
			}
			if h.OldStart-prevEnd > sectionGapThreshold {
				groups = append(groups, nil)
			}
		} else {
			groups = append(groups, nil)
		}
		groups[len(groups)-1] = append(groups[len(groups)-1], h)
	}
	if len(groups) < 2 {
		return nil
	}
	out := make([]hunkGroup, 0, len(groups))
	for _, g := range groups {
		first, last := g[0], g[len(g)-1]
		end := last.OldStart
		if last.OldCount > 0 {
			end = last.OldStart + last.OldCount - 1
		}
		hg := hunkGroup{section: fmt.Sprintf("lines %d-%d", first.OldStart, end)}
		for _, h := range g {
			hg.indices = append(hg.indices, h.Index)
		}
		out = append(out, hg)
	}
	return out
}

// sectionLabel trims a raw function-context line down to a short label.
// Labels that do not look like code constructs return "" -- git's DEFAULT
// funcname regex treats any non-indented line as context, so plain-text and
// config files get synthetic "sections" that are just nearby content. Those
// must not drive grouping, the sections array, or granularity warnings; the
// contiguity-gap fallback handles such files instead.
func sectionLabel(section string) string {
	s := strings.TrimSpace(section)
	if s == "" || !looksLikeCodeSection(s) {
		return ""
	}
	if i := strings.Index(s, "("); i > 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(strings.TrimSuffix(s, "{"))
	if fields := strings.Fields(s); len(fields) > 0 {
		return fields[len(fields)-1]
	}
	return s
}

// looksLikeCodeSection reports whether a raw function-context line plausibly
// names a code construct: it has a parameter list, or starts with a
// declaration keyword. Arbitrary prose/config lines fail both.
func looksLikeCodeSection(s string) bool {
	if strings.Contains(s, "(") {
		return true
	}
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return false
	}
	switch strings.ToLower(fields[0]) {
	case "func", "def", "fn", "sub", "function",
		"class", "struct", "enum", "interface", "trait", "impl",
		"module", "namespace", "package", "type", "object", "abstract":
		return true
	}
	return false
}
