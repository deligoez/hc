package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/deligoez/hc/internal/diff"
	"github.com/deligoez/hc/internal/git"
	"github.com/deligoez/hc/internal/output"
	"github.com/deligoez/hc/internal/plan"
)

func newRewriteCmd() *cobra.Command {
	var dryRun bool
	var force bool

	cmd := &cobra.Command{
		Use:   "rewrite [plan-file | -]",
		Short: "Split existing commits into finer-grained commits (history rewrite)",
		Long: "Reads a JSON rewrite plan mapping existing commits to replacement commits and rebuilds\n" +
			"the current branch. Splitting is conflict-free by construction: every split must\n" +
			"reproduce the original commit's tree byte-for-byte, so all later commits re-parent\n" +
			"without ever touching the working tree. The previous head is saved under refs/hc/backup/.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var planData []byte
			var err error
			if args[0] == "-" {
				planData, err = io.ReadAll(os.Stdin)
			} else {
				planData, err = os.ReadFile(args[0])
			}
			if err != nil {
				acErr := output.NewValidationError(
					fmt.Sprintf("cannot read rewrite plan: %v", err),
					"Provide a valid file path or pipe JSON via stdin with \"-\".",
				)
				printer.PrintError(acErr)
				return &exitError{code: ExitValidation}
			}

			runner, acErr := newRepoRunner()
			if acErr != nil {
				printer.PrintError(acErr)
				return &exitError{code: acErr.Code}
			}

			result, acErr := runRewrite(planData, runner, dryRun, force)
			if acErr != nil {
				printer.PrintError(acErr)
				return &exitError{code: acErr.Code}
			}

			if printer.UseJSON() {
				return printer.PrintJSON(result)
			}
			for _, m := range result.Rewrites {
				printer.Info("%s ->", shortSHA(m.Commit))
				for _, r := range m.Replacements {
					printer.Info("  %s %s", shortSHA(r.SHA), r.Message)
				}
			}
			if result.DryRun {
				printer.Info("dry run: %s would move %s -> %s (%d commits rebuilt)",
					result.Branch, shortSHA(result.OldHead), shortSHA(result.NewHead), result.TotalCommits)
			} else {
				printer.Info("%s: %s -> %s (%d commits rebuilt, backup at %s)",
					result.Branch, shortSHA(result.OldHead), shortSHA(result.NewHead), result.TotalCommits, result.BackupRef)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Build and validate everything without moving the branch")
	cmd.Flags().BoolVar(&force, "force", false, "Allow rewriting commits that exist on a remote (requires force-push afterwards)")

	return cmd
}

// runRewrite validates the plan and rebuilds the current branch. Nothing is
// externally visible until the single final ref update: all intermediate
// objects are written unreferenced, so any validation failure leaves the
// repository untouched (exit 2).
func runRewrite(planData []byte, runner *git.Runner, dryRun, force bool) (*output.RewriteResult, *output.ACError) {
	if err := runner.EnsureRepo(); err != nil {
		return nil, output.NewValidationError(
			"not a git repository",
			"Run hc from inside a git repository.",
		)
	}

	rp, err := plan.ParseRewrite(planData)
	if err != nil {
		if acErr, ok := err.(*output.ACError); ok {
			return nil, acErr
		}
		return nil, output.NewValidationError(err.Error(), "")
	}

	branch, err := runner.CurrentBranch()
	if err != nil {
		return nil, output.NewExecutionError(fmt.Sprintf("cannot determine current branch: %v", err), "")
	}
	if branch == "" {
		return nil, output.NewValidationError(
			"HEAD is detached",
			"Check out the branch you want to rewrite first.",
		)
	}

	headSHA, err := runner.ResolveSHA("HEAD")
	if err != nil {
		return nil, output.NewExecutionError(fmt.Sprintf("cannot resolve HEAD: %v", err), "")
	}

	// Resolve rewrite targets to full SHAs.
	targets := make(map[string]*plan.Rewrite, len(rp.Rewrites)) // full sha -> rewrite
	for i := range rp.Rewrites {
		full, err := runner.ResolveSHA(rp.Rewrites[i].Commit)
		if err != nil {
			return nil, output.NewValidationError(
				fmt.Sprintf("cannot resolve commit %q", rp.Rewrites[i].Commit),
				"Use a SHA (or unique prefix) from 'hc log'.",
			)
		}
		if _, dup := targets[full]; dup {
			return nil, output.NewValidationError(
				fmt.Sprintf("commit %s appears in more than one rewrite", shortSHA(full)),
				"Merge the entries: each commit can be rewritten once.",
			)
		}
		targets[full] = &rp.Rewrites[i]
	}

	// Locate targets on the branch's first-parent chain (newest first).
	chain, err := runner.FirstParentChain(headSHA, 0)
	if err != nil {
		return nil, output.NewExecutionError(fmt.Sprintf("cannot list branch commits: %v", err), "")
	}
	pos := make(map[string]int, len(chain))
	for i, sha := range chain {
		pos[sha] = i
	}
	deepest := -1
	for sha := range targets {
		p, ok := pos[sha]
		if !ok {
			return nil, output.NewValidationError(
				fmt.Sprintf("commit %s is not on the current branch's first-parent chain", shortSHA(sha)),
				"hc rewrite only rewrites commits reachable from HEAD by first parents.",
			)
		}
		if p > deepest {
			deepest = p
		}
	}

	// Commits to rebuild, oldest first.
	rebuild := make([]string, 0, deepest+1)
	for i := deepest; i >= 0; i-- {
		rebuild = append(rebuild, chain[i])
	}

	// Load metadata and enforce a linear history.
	infos := make([]*git.CommitInfo, len(rebuild))
	for i, sha := range rebuild {
		ci, err := runner.ReadCommit(sha)
		if err != nil {
			return nil, output.NewExecutionError(fmt.Sprintf("cannot read commit %s: %v", sha, err), "")
		}
		if len(ci.Parents) == 0 {
			return nil, output.NewValidationError(
				fmt.Sprintf("cannot rewrite the root commit %s", shortSHA(sha)),
				"Rewrite a range that starts after the root commit.",
			)
		}
		if len(ci.Parents) > 1 {
			return nil, output.NewValidationError(
				fmt.Sprintf("commit %s is a merge; hc rewrite requires a linear history in the rewritten range", shortSHA(sha)),
				"Split only commits below the merge, or flatten the history first.",
			)
		}
		infos[i] = ci
	}

	// Refuse to rewrite published history unless forced. Dry runs are
	// exempt: they move nothing, and being able to validate a plan against
	// pushed history is exactly what makes them useful.
	if !force && !dryRun {
		remotes, err := runner.RemoteBranchesContaining(rebuild[0])
		if err == nil && len(remotes) > 0 {
			return nil, output.NewValidationError(
				fmt.Sprintf("commit %s is already on remote branch(es): %s", shortSHA(rebuild[0]), strings.Join(remotes, ", ")),
				"Rewriting published history requires --force and a force-push afterwards.",
			)
		}
	}

	// Temp index for tree construction.
	tmpFile, err := os.CreateTemp("", "hc-rewrite-index-*")
	if err != nil {
		return nil, output.NewExecutionError(fmt.Sprintf("cannot create temp index file: %v", err), "")
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	_ = os.Remove(tmpPath) // read-tree recreates it; avoid empty-file confusion
	defer os.Remove(tmpPath)
	tempRunner := &git.Runner{Dir: runner.Dir, Env: []string{"GIT_INDEX_FILE=" + tmpPath}}

	result := &output.RewriteResult{
		Branch:  branch,
		OldHead: shortSHA(headSHA),
		DryRun:  dryRun,
	}

	newParent := infos[0].Parents[0]
	for i, orig := range infos {
		rw, isSplit := targets[orig.SHA]
		if !isSplit {
			// Untouched commit: same tree, new parent, same author/message.
			newSHA, err := runner.CommitTree(orig.Tree, newParent, orig.Message, orig)
			if err != nil {
				return nil, output.NewExecutionError(
					fmt.Sprintf("cannot re-parent commit %s: %v", shortSHA(orig.SHA), err), "")
			}
			newParent = newSHA
			continue
		}

		replacements, acErr := buildSplit(runner, tempRunner, orig, rw, newParent)
		if acErr != nil {
			return nil, acErr
		}
		mapping := output.RewriteMapping{Commit: shortSHA(orig.SHA)}
		for _, r := range replacements {
			mapping.Replacements = append(mapping.Replacements, r)
			newParent = r.SHA
		}
		result.Rewrites = append(result.Rewrites, mapping)
		_ = i
	}

	result.NewHead = shortSHA(newParent)
	result.TotalCommits = len(rebuild)

	if dryRun {
		return result, nil
	}

	backupRef := "refs/hc/backup/" + branch
	if err := runner.UpdateRef(backupRef, headSHA, ""); err != nil {
		return nil, output.NewExecutionError(
			fmt.Sprintf("cannot create backup ref: %v", err), "")
	}
	if err := runner.UpdateRef("refs/heads/"+branch, newParent, headSHA); err != nil {
		return nil, output.NewExecutionError(
			fmt.Sprintf("cannot move branch %s: %v", branch, err),
			fmt.Sprintf("The branch changed while hc was running. Old head is preserved at %s.", backupRef),
		)
	}
	result.BackupRef = backupRef

	return result, nil
}

// buildSplit turns one original commit into its replacement commits on top of
// newParent, and verifies the final replacement reproduces the original tree
// byte-for-byte.
func buildSplit(runner, tempRunner *git.Runner, orig *git.CommitInfo, rw *plan.Rewrite, newParent string) ([]output.RewrittenEntry, *output.ACError) {
	wrap := func(msg string) string {
		return fmt.Sprintf("rewrite of %s: %s", shortSHA(orig.SHA), msg)
	}

	// Parse the commit's diff against its original parent.
	raw, err := runner.DiffCommit(orig.SHA)
	if err != nil {
		return nil, output.NewExecutionError(wrap(fmt.Sprintf("cannot diff commit: %v", err)), "")
	}
	parsedFiles, err := diff.Parse(raw)
	if err != nil {
		return nil, output.NewExecutionError(wrap(fmt.Sprintf("cannot parse diff: %v", err)), "")
	}
	if len(parsedFiles) == 0 {
		return nil, output.NewValidationError(
			wrap("commit introduces no changes"),
			"Empty commits cannot be split.",
		)
	}

	// Validate the sub-plan with the same rules as hc run: full coverage,
	// no duplicates, sane fields. No allow_unplanned inside a commit.
	sub := &plan.Plan{Commits: rw.Commits}
	if err := plan.ValidateFields(sub); err != nil {
		return nil, output.NewValidationError(wrap(err.Error()), errHint(err))
	}
	if err := plan.ValidateCoverage(sub, parsedFiles); err != nil {
		return nil, output.NewValidationError(wrap(err.Error()), errHint(err))
	}

	diffMap := make(map[string]*diff.FileDiff, len(parsedFiles))
	for i := range parsedFiles {
		diffMap[parsedFiles[i].Path] = &parsedFiles[i]
	}

	// Base blobs come from the ORIGINAL parent: split trees are rebuilt in
	// original-diff coordinates, exactly like hc run's staging.
	origParent := orig.Parents[0]
	states := make(map[string]*fileState)
	for _, c := range rw.Commits {
		for _, f := range c.Files {
			if f.IsFullFile() || states[f.Path] != nil {
				continue
			}
			base, _, err := runner.BlobAt(origParent, f.Path)
			if err != nil {
				return nil, output.NewExecutionError(
					wrap(fmt.Sprintf("cannot read %s at parent: %v", f.Path, err)), "")
			}
			states[f.Path] = &fileState{fd: diffMap[f.Path], base: base}
		}
	}

	// Start from the original parent's tree and stage each replacement.
	if err := tempRunner.ReadTree(origParent + "^{tree}"); err != nil {
		return nil, output.NewExecutionError(wrap(fmt.Sprintf("cannot read parent tree: %v", err)), "")
	}

	var replacements []output.RewrittenEntry
	committed := make(map[string]map[int]bool)
	prev := newParent
	lastTree := ""

	for ci, c := range rw.Commits {
		for _, f := range c.Files {
			if f.IsFullFile() {
				// Full-file: stage the file's state AS OF the original commit.
				mode, blob, exists, err := treeEntryOf(runner, orig.SHA, f.Path)
				if err != nil {
					return nil, output.NewExecutionError(
						wrap(fmt.Sprintf("cannot inspect %s at commit: %v", f.Path, err)), "")
				}
				if !exists {
					if err := tempRunner.RemoveFromIndex(f.Path); err != nil {
						return nil, output.NewExecutionError(
							wrap(fmt.Sprintf("cannot stage deletion of %s: %v", f.Path, err)), "")
					}
				} else if err := tempRunner.StageBlob(mode, blob, f.Path); err != nil {
					return nil, output.NewExecutionError(
						wrap(fmt.Sprintf("cannot stage %s: %v", f.Path, err)), "")
				}
				continue
			}

			st := states[f.Path]
			if st == nil || st.fd == nil {
				return nil, output.NewExecutionError(
					wrap(fmt.Sprintf("%s not found in commit diff", f.Path)), "")
			}
			if se := stageHunkSelection(tempRunner, f.Path, st, committed[f.Path], f.Hunks); se != nil {
				return nil, output.NewValidationError(wrap(se.msg), se.hint)
			}
		}
		mergeCommitted(committed, c)

		tree, err := tempRunner.WriteTree()
		if err != nil {
			return nil, output.NewExecutionError(wrap(fmt.Sprintf("cannot write tree: %v", err)), "")
		}
		newSHA, err := runner.CommitTree(tree, prev, c.Message, orig)
		if err != nil {
			return nil, output.NewExecutionError(wrap(fmt.Sprintf("cannot create commit %d: %v", ci, err)), "")
		}
		replacements = append(replacements, output.RewrittenEntry{SHA: newSHA, Message: c.Message})
		prev = newSHA
		lastTree = tree
	}

	// The invariant that makes the whole rewrite conflict-free: the final
	// replacement must reproduce the original commit's tree exactly.
	if lastTree != orig.Tree {
		return nil, output.NewValidationError(
			wrap("replacements do not reproduce the original tree"),
			"This indicates an inconsistency in the captured diff. Re-run 'hc log --json' and rebuild the plan.",
		)
	}

	return replacements, nil
}

// treeEntryOf adapts git.TreeEntry to also report existence cleanly.
func treeEntryOf(r *git.Runner, rev, path string) (mode, blob string, exists bool, err error) {
	mode, blob, exists, err = r.TreeEntry(rev, path)
	return
}

// errHint extracts the hint from an ACError-shaped error.
func errHint(err error) string {
	if acErr, ok := err.(*output.ACError); ok {
		return acErr.Hint
	}
	return ""
}
