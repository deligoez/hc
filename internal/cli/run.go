package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/deligoez/hc/internal/diff"
	"github.com/deligoez/hc/internal/git"
	"github.com/deligoez/hc/internal/output"
	"github.com/deligoez/hc/internal/plan"
)

func newRunCmd() *cobra.Command {
	var dryRun bool
	var prefix string

	cmd := &cobra.Command{
		Use:   "run [plan-file | -]",
		Short: "Execute a commit plan",
		Long:  "Reads a JSON commit plan from a file (or stdin with \"-\") and creates atomic commits.",
		Args:  cobra.ExactArgs(1),
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
					fmt.Sprintf("cannot read plan: %v", err),
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

			result, acErr := runPlan(planData, runner, dryRun, prefix)
			if acErr != nil {
				// Execution failures carry a partial result: report which
				// commits were created (with SHAs) so the agent can re-plan
				// only the remaining changes.
				if r, ok := result.(*output.Result); ok && r != nil && len(r.Commits) > 0 {
					if printer.UseJSON() {
						printer.PrintJSON(r)
					} else {
						for _, cr := range r.Commits {
							if cr.Status == "committed" {
								printer.Info("[%d] %s %s", cr.Index, cr.SHA, cr.Message)
							}
						}
						printer.Info("committed %d/%d", r.Committed, r.Total)
						printer.PrintError(acErr)
					}
				} else {
					printer.PrintError(acErr)
				}
				return &exitError{code: acErr.Code}
			}

			if dryRun {
				dr := result.(*output.DryRunResult)
				if printer.UseJSON() {
					printer.PrintJSON(dr)
				} else {
					for _, w := range dr.Warnings {
						fmt.Fprintln(printer.ErrOut, "warning:", w)
					}
					printer.Info("Dry run: %d commits, %d files, %d hunks", dr.Commits, dr.Files, dr.HunksTotal)
					printer.Info("Coverage: %d/%d hunks assigned, 0 unplanned files", dr.HunksAssigned, dr.HunksTotal)
					printer.Info("Plan valid: %d commits would be created", dr.Commits)
				}
			} else {
				r := result.(*output.Result)
				if printer.UseJSON() {
					printer.PrintJSON(r)
				} else {
					for _, w := range r.Warnings {
						fmt.Fprintln(printer.ErrOut, "warning:", w)
					}
					for _, cr := range r.Commits {
						if cr.Status == "committed" {
							printer.Info("[%d] %s %s", cr.Index, cr.SHA, cr.Message)
						}
					}
					printer.Info("committed %d/%d", r.Committed, r.Total)
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Validate the plan without creating commits")
	cmd.Flags().StringVar(&prefix, "prefix", "", "Prefix prepended to every commit message (idempotent), e.g. \"WB-1234: \"")

	return cmd
}

// runPlan validates and (in Phase 2) executes a commit plan. The optional
// prefixOpt (from --prefix) is prepended to every commit message; prefixing
// is idempotent. Per-commit prefixes (e.g. one ticket per commit on an
// umbrella branch) are the plan author's job: write them into the messages.
func runPlan(planData []byte, runner *git.Runner, dryRun bool, prefixOpt ...string) (any, *output.ACError) {
	prefix := ""
	if len(prefixOpt) > 0 {
		prefix = prefixOpt[0]
	}
	// --- Step 1: Ensure we are in a git repo ---
	if err := runner.EnsureRepo(); err != nil {
		return nil, output.NewValidationError(
			"not a git repository",
			"Run hc from inside a git repository.",
		)
	}

	// --- Step 2: Parse the plan ---
	p, err := plan.Parse(planData)
	if err != nil {
		if acErr, ok := err.(*output.ACError); ok {
			return nil, acErr
		}
		return nil, output.NewValidationError(err.Error(), "")
	}

	// --- Step 3: Validate plan fields ---
	if err := plan.ValidateFields(p); err != nil {
		if acErr, ok := err.(*output.ACError); ok {
			return nil, acErr
		}
		return nil, output.NewValidationError(err.Error(), "")
	}

	// --- Step 3b: Apply the --prefix flag to every commit message ---
	if prefix != "" {
		for i := range p.Commits {
			if !strings.HasPrefix(p.Commits[i].Message, prefix) {
				p.Commits[i].Message = prefix + p.Commits[i].Message
			}
		}
	}

	// --- Step 4: Intent-to-add for untracked files referenced in the plan ---
	var intentAdded []string
	revertIntent := func() {
		for _, path := range intentAdded {
			_ = runner.RevertIntentToAdd(path)
		}
	}

	for _, path := range collectPlanFilePaths(p) {
		untracked, err := runner.IsUntracked(path)
		if err != nil {
			revertIntent()
			return nil, output.NewExecutionError(
				fmt.Sprintf("cannot check untracked status for %s: %v", path, err),
				"Ensure the file exists and git is working correctly.",
			)
		}
		if untracked {
			if err := runner.IntentToAdd(path); err != nil {
				revertIntent()
				return nil, output.NewExecutionError(
					fmt.Sprintf("git add -N failed for %s: %v", path, err),
					"",
				)
			}
			intentAdded = append(intentAdded, path)
		}
	}

	// --- Step 1b: Ensure clean staging (after intent-to-add) ---
	if err := runner.EnsureCleanStaging(); err != nil {
		revertIntent()
		return nil, output.NewValidationError(
			"staging area is not clean",
			"Run 'git reset HEAD' first. hc requires a clean staging area.",
		)
	}

	// --- Step 5: Capture diff ---
	rawDiff, err := runner.Diff("-U0", "--no-renames", "--no-ext-diff")
	if err != nil {
		revertIntent()
		return nil, output.NewExecutionError(
			fmt.Sprintf("git diff failed: %v", err),
			"",
		)
	}

	// --- Step 6: Parse diff ---
	parsedFiles, err := diff.Parse(rawDiff)
	if err != nil {
		revertIntent()
		return nil, output.NewExecutionError(
			fmt.Sprintf("failed to parse diff: %v", err),
			"",
		)
	}

	// --- Step 6b: Check for no changes ---
	if len(parsedFiles) == 0 && len(intentAdded) == 0 {
		revertIntent()
		return nil, output.NewValidationError(
			"no uncommitted changes",
			"There is nothing to commit.",
		)
	}

	// --- Step 6c: Exempt unplanned intent-to-add files from coverage ---
	// With a clean staging area, an IsNew entry in the UNSTAGED diff can only
	// come from an intent-to-add index entry. hc itself only runs git add -N
	// on paths referenced by the plan (Step 4), so an unplanned IsNew entry
	// is a stale or foreign `git add -N` (crashed tool, editor plugin, ...).
	// hc can never stage such a file -- staging is always driven by the plan
	// -- so requiring it in the plan (or in allow_unplanned) adds noise
	// without adding safety. Skip it from coverage and surface a warning.
	planned := make(map[string]bool)
	for _, path := range collectPlanFilePaths(p) {
		planned[path] = true
	}
	var skippedITA []string
	kept := make([]diff.FileDiff, 0, len(parsedFiles))
	for _, fd := range parsedFiles {
		if fd.IsNew && !planned[fd.Path] {
			skippedITA = append(skippedITA, fd.Path)
			continue
		}
		kept = append(kept, fd)
	}
	parsedFiles = kept

	var warnings []string
	switch {
	case len(skippedITA) == 0:
	case len(skippedITA) <= 5:
		for _, path := range skippedITA {
			warnings = append(warnings, fmt.Sprintf(
				"skipped unplanned intent-to-add file %s (never staged by hc; add it to a commit to include it)", path))
		}
	default:
		warnings = append(warnings, fmt.Sprintf(
			"skipped %d unplanned intent-to-add files (never staged by hc; add paths to a commit to include them): %s, ... (+%d more)",
			len(skippedITA), strings.Join(skippedITA[:5], ", "), len(skippedITA)-5))
	}
	if len(parsedFiles) == 0 {
		revertIntent()
		return nil, output.NewValidationError(
			"no uncommitted changes",
			"There is nothing to commit.",
		)
	}

	// --- Step 7: Compute fingerprints ---
	for i := range parsedFiles {
		for j := range parsedFiles[i].Hunks {
			parsedFiles[i].Hunks[j].Fingerprint = diff.Fingerprint(parsedFiles[i].Hunks[j])
		}
	}

	// --- Step 8: Validate coverage ---
	if err := plan.ValidateCoverage(p, parsedFiles); err != nil {
		revertIntent()
		if acErr, ok := err.(*output.ACError); ok {
			return nil, acErr
		}
		return nil, output.NewValidationError(err.Error(), "")
	}

	// --- Step 8a: Advisory granularity check ---
	// A commit whose hunk selection for one file spans multiple enclosing
	// sections is often more than one idea. Mechanically undecidable (a
	// single idea can touch several functions), so this only warns.
	if w := multiSectionWarning(p, parsedFiles); w != "" {
		warnings = append(warnings, w)
	}

	// --- Step 8b: Capture base content for every hunk-mode file ---
	states, acErr := buildFileStates(p, parsedFiles, runner)
	if acErr != nil {
		revertIntent()
		return nil, acErr
	}

	// --- Step 9: Validate reconstruction + simulate on a temp index ---
	if valErr := validateWithTempIndex(p, states, runner); valErr != nil {
		revertIntent()
		return nil, valErr
	}

	// --- Dry-run exit ---
	if dryRun {
		revertIntent()
		result := buildDryRunResult(p, parsedFiles)
		result.Warnings = warnings
		return result, nil
	}

	// --- Phase 2: Execute the plan ---
	result, acErr := executePlan(p, states, runner, intentAdded)
	if result != nil {
		result.Warnings = warnings
	}
	if acErr != nil {
		return result, acErr
	}

	// After successful execution, do NOT revert intent-to-add entries.
	// Files that were committed via git add (full-file mode) are now properly
	// in the repository. Reverting them (git rm --cached) would corrupt the index.
	// Files that were committed via hunk-select also have their content
	// properly staged and committed.
	// Only revert intent-to-add on FAILURE paths (already handled above).

	return result, nil
}

// applyCommitPrefix prepends the configured commit prefix to every commit
// message in the plan (idempotent: messages already carrying the prefix are
// left alone). "${ticket}" in the prefix resolves via ticket_from_branch
// against the current branch name; an unresolved ticket skips prefixing and
// returns a warning instead of failing the plan.
// multiSectionWarning reports commits that bundle hunks from multiple
// sections of one file, as a single aggregated advisory line.
func multiSectionWarning(p *plan.Plan, parsedFiles []diff.FileDiff) string {
	diffMap := make(map[string]*diff.FileDiff, len(parsedFiles))
	for i := range parsedFiles {
		diffMap[parsedFiles[i].Path] = &parsedFiles[i]
	}
	var hits []string
	for ci, c := range p.Commits {
		for _, f := range c.Files {
			fd := diffMap[f.Path]
			if fd == nil {
				continue
			}
			indices := f.Hunks
			if f.IsFullFile() {
				indices = nil
				for _, h := range fd.Hunks {
					indices = append(indices, h.Index)
				}
			}
			seen := map[string]bool{}
			var labels []string
			for _, idx := range indices {
				if idx < 0 || idx >= len(fd.Hunks) {
					continue
				}
				if l := sectionLabel(fd.Hunks[idx].Section); l != "" && !seen[l] {
					seen[l] = true
					labels = append(labels, l)
				}
			}
			if len(labels) > 1 {
				hits = append(hits, fmt.Sprintf("commit %d: %s spans %s", ci, f.Path, strings.Join(labels, ", ")))
			}
		}
	}
	if len(hits) == 0 {
		return ""
	}
	sample := hits[0]
	if len(hits) > 1 {
		sample += fmt.Sprintf(" (+%d more)", len(hits)-1)
	}
	return fmt.Sprintf("review granularity: %d commit(s) bundle hunks from multiple sections of one file -- split unless each is a single idea. %s", len(hits), sample)
}

// fileState holds everything needed to rebuild a hunk-mode file's staged
// content at any point in the plan: the parsed diff and the file's base
// content (its stage-0 index blob at capture time). Staged content for a
// commit is always Reconstruct(base, union of committed+current hunks) --
// original diff coordinates, immune to line drift, hunk merging, or git
// re-splitting hunks over repeated content.
type fileState struct {
	fd   *diff.FileDiff
	base []byte
}

// stageError describes a failure while staging a file entry.
type stageError struct {
	msg  string
	hint string
}

// buildFileStates fetches the base (index) content of every hunk-mode file in
// the plan.
func buildFileStates(p *plan.Plan, parsedFiles []diff.FileDiff, runner *git.Runner) (map[string]*fileState, *output.ACError) {
	diffMap := make(map[string]*diff.FileDiff, len(parsedFiles))
	for i := range parsedFiles {
		diffMap[parsedFiles[i].Path] = &parsedFiles[i]
	}

	states := make(map[string]*fileState)
	for _, c := range p.Commits {
		for _, f := range c.Files {
			if f.IsFullFile() || states[f.Path] != nil {
				continue
			}
			fd, ok := diffMap[f.Path]
			if !ok {
				// Coverage validation guarantees presence; guard anyway.
				return nil, output.NewExecutionError(
					fmt.Sprintf("%s not found in original diff", f.Path),
					"",
				)
			}
			base, err := runner.IndexBlob(f.Path)
			if err != nil {
				return nil, output.NewExecutionError(
					fmt.Sprintf("cannot read index content for %s: %v", f.Path, err),
					"",
				)
			}
			states[f.Path] = &fileState{fd: fd, base: base}
		}
	}
	return states, nil
}

// stageHunkSelection stages base + union(committed, newHunks) for path into
// the runner's index. It is shared by temp-index validation and execution:
// the only difference is which index the runner points at.
func stageHunkSelection(r *git.Runner, path string, st *fileState, committed map[int]bool, newHunks []int) *stageError {
	fd := st.fd

	for _, i := range newHunks {
		if i >= len(fd.Hunks) {
			return &stageError{msg: fmt.Sprintf("hunk %d out of range for %s", i, path)}
		}
	}

	// A deleted file's diff is a single hunk deleting everything; selecting
	// it stages the deletion itself, not an empty file.
	if fd.IsDeleted {
		if err := r.RemoveFromIndex(path); err != nil {
			return &stageError{msg: fmt.Sprintf("cannot stage deletion of %s: %v", path, err)}
		}
		return nil
	}

	union := make([]int, 0, len(committed)+len(newHunks))
	for i := range committed {
		union = append(union, i)
	}
	union = append(union, newHunks...)
	sort.Ints(union)

	subset := make([]diff.Hunk, 0, len(union))
	for _, i := range union {
		subset = append(subset, fd.Hunks[i])
	}

	content, err := diff.Reconstruct(st.base, subset)
	if err != nil {
		return &stageError{
			msg:  fmt.Sprintf("cannot reconstruct %s: %v", path, err),
			hint: "The working tree may have changed since the diff was captured. Re-run 'hc diff' and rebuild the plan.",
		}
	}

	sha, err := r.HashObjectWrite(content)
	if err != nil {
		return &stageError{msg: fmt.Sprintf("cannot hash reconstructed content for %s: %v", path, err)}
	}

	mode := fd.NewMode
	if mode == "" {
		mode, err = r.IndexEntryMode(path)
		if err != nil || mode == "" {
			mode = "100644"
		}
	}

	if err := r.StageBlob(mode, sha, path); err != nil {
		return &stageError{msg: fmt.Sprintf("cannot stage %s: %v", path, err)}
	}
	return nil
}

// mergeCommitted records a commit's hunk selections after the commit succeeds.
func mergeCommitted(committed map[string]map[int]bool, c plan.Commit) {
	for _, f := range c.Files {
		if f.IsFullFile() {
			continue
		}
		if committed[f.Path] == nil {
			committed[f.Path] = make(map[int]bool)
		}
		for _, h := range f.Hunks {
			committed[f.Path][h] = true
		}
	}
}

// executePlan iterates over each commit in the plan, stages the appropriate
// hunks/files, and creates real commits. On failure it returns a partial result.
func executePlan(p *plan.Plan, states map[string]*fileState, runner *git.Runner, addedNFiles []string) (*output.Result, *output.ACError) {
	result := &output.Result{Total: len(p.Commits)}
	committed := make(map[string]map[int]bool)

	for i, commit := range p.Commits {
		cr := executeCommit(i, commit, states, committed, runner)
		result.Commits = append(result.Commits, cr)

		if cr.Status == "failed" {
			// Build hint based on progress.
			if i == 0 {
				result.Hint = "No commits were created. Fix the issue and re-run."
			} else {
				result.Hint = fmt.Sprintf("Commits 0-%d are done. Re-plan remaining changes.", i-1)
			}
			result.Error = cr.Error
			result.Code = 3

			// Clean up orphaned intent-to-add files.
			cleanupOrphanedIntentToAdd(runner, addedNFiles)

			return result, output.NewExecutionError(cr.Error, result.Hint)
		}

		mergeCommitted(committed, commit)
		result.Committed++
	}

	return result, nil
}

// executeCommit stages files for a single commit and creates it.
func executeCommit(idx int, commit plan.Commit, states map[string]*fileState, committed map[string]map[int]bool, runner *git.Runner) output.CommitResult {
	cr := output.CommitResult{
		Index:   idx,
		Message: commit.Message,
	}

	for _, f := range commit.Files {
		fr := output.FileResult{Path: f.Path}

		if f.IsFullFile() {
			fr.Strategy = "full"
			if err := runner.Add(f.Path); err != nil {
				cr.Status = "failed"
				cr.Error = fmt.Sprintf("cannot stage %s: %v", f.Path, err)
				cr.Hint = "Check that the file exists and has changes."
				// Reset staging on stage failure.
				_ = runner.ResetHead()
				cr.Files = append(cr.Files, fr)
				return cr
			}
		} else {
			fr.Strategy = "hunks"
			fr.Hunks = f.Hunks

			st, ok := states[f.Path]
			if !ok {
				cr.Status = "failed"
				cr.Error = fmt.Sprintf("%s not found in original diff", f.Path)
				_ = runner.ResetHead()
				cr.Files = append(cr.Files, fr)
				return cr
			}

			if se := stageHunkSelection(runner, f.Path, st, committed[f.Path], f.Hunks); se != nil {
				cr.Status = "failed"
				cr.Error = se.msg
				cr.Hint = se.hint
				_ = runner.ResetHead()
				cr.Files = append(cr.Files, fr)
				return cr
			}
		}

		cr.Files = append(cr.Files, fr)
	}

	// All files staged successfully; create the commit.
	sha, err := runner.Commit(commit.Message)
	if err != nil {
		cr.Status = "failed"
		cr.Error = fmt.Sprintf("git commit failed: %v", err)
		cr.Hint = fmt.Sprintf("Staging is intact. If a pre-commit hook failed, fix the issue and run 'git commit -m \"%s\"' manually, then re-plan remaining changes.", commit.Message)
		// Do NOT reset staging on commit failure (leave intact for manual fix).
		return cr
	}

	cr.SHA = sha
	cr.Status = "committed"
	return cr
}

// cleanupOrphanedIntentToAdd reverts intent-to-add entries that are still
// only intent-to-add (empty blob in the index). This prevents orphaned
// entries after a failed execution.
func cleanupOrphanedIntentToAdd(runner *git.Runner, addedNFiles []string) {
	for _, path := range addedNFiles {
		// Best-effort: try to revert; ignore errors (file may have been committed).
		_ = runner.RevertIntentToAdd(path)
	}
}

// validateWithTempIndex validates the plan without touching real git state:
//
//  1. For every hunk-mode file, rebuilding the FULL diff from its base must
//     reproduce the working tree byte-for-byte. This catches captured-diff
//     inconsistencies and working-tree drift up front, before any commit.
//  2. Every commit is then simulated in order against a copy of the index,
//     exercising the exact same staging calls as execution.
func validateWithTempIndex(p *plan.Plan, states map[string]*fileState, runner *git.Runner) *output.ACError {
	// --- (1) full-content invariant ---
	for path, st := range states {
		full, err := diff.Reconstruct(st.base, st.fd.Hunks)
		if err != nil {
			return output.NewValidationError(
				fmt.Sprintf("captured diff is inconsistent for %s: %v", path, err),
				"Re-run 'hc diff --json' and rebuild the plan.",
			)
		}
		if st.fd.IsDeleted {
			continue // no working-tree file to compare against
		}
		wtSHA, err := runner.HashWorktreeFile(path)
		if err != nil {
			return output.NewExecutionError(
				fmt.Sprintf("cannot hash working tree file %s: %v", path, err),
				"",
			)
		}
		fullSHA, err := runner.RunWithStdin(full, "hash-object", "--stdin")
		if err != nil {
			return output.NewExecutionError(
				fmt.Sprintf("cannot hash reconstructed content for %s: %v", path, err),
				"",
			)
		}
		if strings.TrimSpace(fullSHA) != wtSHA {
			return output.NewValidationError(
				fmt.Sprintf("working tree content of %s does not match the captured diff", path),
				"The file changed while hc was running. Re-run 'hc diff --json' and rebuild the plan.",
			)
		}
	}

	// --- (2) sequential simulation on a temp index ---
	gitDirOut, err := runner.Run("rev-parse", "--git-dir")
	if err != nil {
		return output.NewExecutionError(
			fmt.Sprintf("cannot find .git directory: %v", err),
			"",
		)
	}
	gitDir := strings.TrimSpace(gitDirOut)

	// Resolve to absolute path if relative.
	if !filepath.IsAbs(gitDir) {
		if runner.Dir != "" {
			gitDir = filepath.Join(runner.Dir, gitDir)
		}
	}

	origIndex := filepath.Join(gitDir, "index")

	// Create a temp file for the index copy.
	tmpFile, err := os.CreateTemp("", "hc-index-*")
	if err != nil {
		return output.NewExecutionError(
			fmt.Sprintf("cannot create temp index file: %v", err),
			"",
		)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	// Copy the original index to the temp file.
	if err := copyFile(origIndex, tmpPath); err != nil {
		return output.NewExecutionError(
			fmt.Sprintf("cannot copy index file: %v", err),
			"",
		)
	}

	// Preserve the original index's mtime on the copy. Git's racy-git
	// protection content-checks entries whose stat mtime is not older than
	// the index file itself; a fresh copy timestamp defeats that check, and
	// a file rewritten within the stat granularity of its index entry (same
	// size, same mtime) is then silently reported as unchanged.
	if fi, err := os.Stat(origIndex); err == nil {
		_ = os.Chtimes(tmpPath, fi.ModTime(), fi.ModTime())
	}

	// Create a runner that uses the temp index.
	tempRunner := &git.Runner{
		Dir: runner.Dir,
		Env: []string{"GIT_INDEX_FILE=" + tmpPath},
	}

	committed := make(map[string]map[int]bool)
	for ci, c := range p.Commits {
		for _, f := range c.Files {
			if f.IsFullFile() {
				if err := tempRunner.Add(f.Path); err != nil {
					return output.NewExecutionError(
						fmt.Sprintf("validation failed at commit %d: cannot stage %s: %v", ci, f.Path, err),
						"Check that the file exists and has changes.",
					)
				}
				continue
			}

			st, ok := states[f.Path]
			if !ok {
				return output.NewExecutionError(
					fmt.Sprintf("validation failed at commit %d: %s not found in original diff", ci, f.Path),
					"",
				)
			}
			if se := stageHunkSelection(tempRunner, f.Path, st, committed[f.Path], f.Hunks); se != nil {
				return output.NewExecutionError(
					fmt.Sprintf("validation failed at commit %d: %s", ci, se.msg),
					se.hint,
				)
			}
		}
		mergeCommitted(committed, c)
	}

	return nil
}

// copyFile copies src to dst, overwriting dst if it exists.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}

	return out.Close()
}

// collectPlanFilePaths returns the unique file paths referenced by the plan,
// in first-seen order. Both hunk-select and full-file entries are included:
// untracked files in either mode need intent-to-add so they appear in the
// diff for coverage validation.
func collectPlanFilePaths(p *plan.Plan) []string {
	seen := make(map[string]bool)
	var paths []string
	for _, c := range p.Commits {
		for _, f := range c.Files {
			if !seen[f.Path] {
				seen[f.Path] = true
				paths = append(paths, f.Path)
			}
		}
	}
	return paths
}

// buildDryRunResult creates a DryRunResult for dry-run output.
func buildDryRunResult(p *plan.Plan, parsedFiles []diff.FileDiff) *output.DryRunResult {
	// Count total hunks in the diff
	hunksTotal := 0
	for _, f := range parsedFiles {
		hunksTotal += len(f.Hunks)
	}

	// Count assigned hunks from the plan
	hunksAssigned := 0
	fileSet := make(map[string]bool)
	for _, c := range p.Commits {
		for _, f := range c.Files {
			fileSet[f.Path] = true
			if f.IsFullFile() {
				// Full-file covers all hunks for that file
				for _, df := range parsedFiles {
					if df.Path == f.Path {
						hunksAssigned += len(df.Hunks)
						break
					}
				}
			} else {
				hunksAssigned += len(f.Hunks)
			}
		}
	}

	return &output.DryRunResult{
		Valid:         true,
		Commits:       len(p.Commits),
		Files:         len(fileSet),
		HunksTotal:    hunksTotal,
		HunksAssigned: hunksAssigned,
		Issues:        []output.DryRunIssue{},
	}
}
