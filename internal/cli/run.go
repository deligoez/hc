package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/deligoez/ac/internal/diff"
	"github.com/deligoez/ac/internal/git"
	"github.com/deligoez/ac/internal/output"
	"github.com/deligoez/ac/internal/patch"
	"github.com/deligoez/ac/internal/plan"
)

func newRunCmd() *cobra.Command {
	var dryRun bool

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
				os.Exit(ExitValidation)
			}

			runner := git.NewRunner(".")

			result, acErr := runPlan(planData, runner, dryRun)
			if acErr != nil {
				printer.PrintError(acErr)
				os.Exit(acErr.Code)
			}

			if dryRun {
				dr := result.(*output.DryRunResult)
				if printer.UseJSON() {
					printer.PrintJSON(dr)
				} else {
					printer.Info("Dry run: %d commits, %d files, %d hunks", dr.Commits, dr.Files, dr.HunksTotal)
					printer.Info("Coverage: %d/%d hunks assigned, 0 unplanned files", dr.HunksAssigned, dr.HunksTotal)
					printer.Info("Plan valid: %d commits would be created", dr.Commits)
				}
			} else {
				r := result.(*output.Result)
				if printer.UseJSON() {
					printer.PrintJSON(r)
				} else {
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

	return cmd
}

// runPlan validates and (in Phase 2) executes a commit plan.
func runPlan(planData []byte, runner *git.Runner, dryRun bool) (any, *output.ACError) {
	// --- Step 1: Ensure we are in a git repo ---
	if err := runner.EnsureRepo(); err != nil {
		return nil, output.NewExecutionError(
			"not a git repository",
			"Run ac from inside a git repository.",
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

	// --- Step 4: Intent-to-add for untracked files that have hunks ---
	var intentAdded []string
	revertIntent := func() {
		for _, path := range intentAdded {
			_ = runner.RevertIntentToAdd(path)
		}
	}

	// Collect all unique file paths referenced in the plan that have hunks.
	intentPaths := collectHunkFilePaths(p)
	for _, path := range intentPaths {
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

	// Also intent-to-add full-file entries that are untracked, so they appear
	// in the diff for coverage validation.
	fullFilePaths := collectFullFilePaths(p)
	for _, path := range fullFilePaths {
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
			"Run 'git reset HEAD' first. ac requires a clean staging area.",
		)
	}

	// --- Step 5: Capture diff ---
	rawDiff, err := runner.Diff("-U0", "-M", "--no-ext-diff")
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

	// --- Step 9: Sequential dry-run with temp index ---
	if valErr := validateWithTempIndex(p, parsedFiles, runner); valErr != nil {
		revertIntent()
		return nil, valErr
	}

	// --- Dry-run exit ---
	if dryRun {
		revertIntent()
		result := buildDryRunResult(p, parsedFiles)
		return result, nil
	}

	// --- Phase 2: Execute the plan ---
	result, acErr := executePlan(p, parsedFiles, runner, intentAdded)
	if acErr != nil {
		return result, acErr
	}

	// Clean up intent-to-add entries that were consumed by commits.
	// Any file that was committed no longer needs intent-to-add cleanup.
	// Files that were NOT touched by any commit still need reverting.
	revertIntent()

	return result, nil
}

// executePlan iterates over each commit in the plan, stages the appropriate
// hunks/files, and creates real commits. On failure it returns a partial result.
func executePlan(p *plan.Plan, origFiles []diff.FileDiff, runner *git.Runner, addedNFiles []string) (*output.Result, *output.ACError) {
	result := &output.Result{Total: len(p.Commits)}

	// Build a map from path to parsed file diff for quick lookup.
	diffMap := make(map[string]*diff.FileDiff, len(origFiles))
	for i := range origFiles {
		diffMap[origFiles[i].Path] = &origFiles[i]
	}

	for i, commit := range p.Commits {
		cr := executeCommit(i, commit, diffMap, runner)
		result.Commits = append(result.Commits, cr)

		if cr.Status == "failed" {
			// Build hint based on progress.
			if i == 0 {
				result.Hint = "No commits were created. Fix the issue and re-run."
			} else {
				result.Hint = fmt.Sprintf("Commits 0-%d are done. Re-plan remaining changes.", i-1)
			}
			result.Error = cr.Error

			// Clean up orphaned intent-to-add files.
			cleanupOrphanedIntentToAdd(runner, addedNFiles)

			return result, output.NewExecutionError(cr.Error, result.Hint)
		}

		result.Committed++
	}

	return result, nil
}

// executeCommit stages files for a single commit and creates it.
func executeCommit(idx int, commit plan.Commit, diffMap map[string]*diff.FileDiff, runner *git.Runner) output.CommitResult {
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
				// Reset staging on apply failure.
				_ = runner.ResetHead()
				cr.Files = append(cr.Files, fr)
				return cr
			}
		} else {
			fr.Strategy = "hunks"
			fr.Hunks = f.Hunks

			// Re-diff against current index.
			reDiffOutput, err := runner.DiffFile(f.Path, "-U0", "--no-ext-diff")
			if err != nil {
				cr.Status = "failed"
				cr.Error = fmt.Sprintf("cannot diff %s: %v", f.Path, err)
				_ = runner.ResetHead()
				cr.Files = append(cr.Files, fr)
				return cr
			}

			currentFiles, err := diff.Parse(reDiffOutput)
			if err != nil {
				cr.Status = "failed"
				cr.Error = fmt.Sprintf("cannot parse diff for %s: %v", f.Path, err)
				_ = runner.ResetHead()
				cr.Files = append(cr.Files, fr)
				return cr
			}

			if len(currentFiles) == 0 {
				cr.Status = "failed"
				cr.Error = fmt.Sprintf("no diff for %s against current index", f.Path)
				cr.Hint = "The file may have already been fully staged by a previous commit."
				_ = runner.ResetHead()
				cr.Files = append(cr.Files, fr)
				return cr
			}

			currentFileDiff := currentFiles[0]

			// Fingerprint current hunks.
			for j := range currentFileDiff.Hunks {
				currentFileDiff.Hunks[j].Fingerprint = diff.Fingerprint(currentFileDiff.Hunks[j])
			}

			// Get the original file diff for fingerprint matching.
			origFile, ok := diffMap[f.Path]
			if !ok {
				cr.Status = "failed"
				cr.Error = fmt.Sprintf("%s not found in original diff", f.Path)
				_ = runner.ResetHead()
				cr.Files = append(cr.Files, fr)
				return cr
			}

			// Build subset of original hunks that this file entry references.
			origSubset := make([]diff.Hunk, 0, len(f.Hunks))
			for _, hunkIdx := range f.Hunks {
				if hunkIdx >= len(origFile.Hunks) {
					cr.Status = "failed"
					cr.Error = fmt.Sprintf("hunk %d out of range for %s", hunkIdx, f.Path)
					_ = runner.ResetHead()
					cr.Files = append(cr.Files, fr)
					return cr
				}
				origSubset = append(origSubset, origFile.Hunks[hunkIdx])
			}

			// Match original hunks to current hunks.
			matchMap, err := diff.MatchHunks(origSubset, currentFileDiff.Hunks)
			if err != nil {
				cr.Status = "failed"
				cr.Error = fmt.Sprintf("hunk matching failed for %s: %v", f.Path, err)
				cr.Hint = "A previous commit may have consumed or altered this hunk."
				_ = runner.ResetHead()
				cr.Files = append(cr.Files, fr)
				return cr
			}

			// Collect matched current hunk indices.
			currentSelectedIndices := make([]int, 0, len(f.Hunks))
			for i := 0; i < len(origSubset); i++ {
				currentSelectedIndices = append(currentSelectedIndices, matchMap[i])
			}

			// Build adjusted patch.
			patchBytes, err := patch.BuildPatch(currentFileDiff, currentSelectedIndices, currentFileDiff.Hunks)
			if err != nil {
				cr.Status = "failed"
				cr.Error = fmt.Sprintf("cannot build patch for %s: %v", f.Path, err)
				_ = runner.ResetHead()
				cr.Files = append(cr.Files, fr)
				return cr
			}

			// Apply patch to the real index.
			if err := patch.Apply(runner, patchBytes); err != nil {
				cr.Status = "failed"
				cr.Error = fmt.Sprintf("git apply failed for %s: %v", f.Path, err)
				cr.Hint = "Working tree may have changed during execution. Run 'git reset HEAD --' and retry."
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

// validateWithTempIndex copies the current git index to a temp file and
// simulates applying all commits in order to verify that patches apply cleanly.
func validateWithTempIndex(p *plan.Plan, parsedFiles []diff.FileDiff, runner *git.Runner) *output.ACError {
	// Find the git directory.
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
	tmpFile, err := os.CreateTemp("", "ac-index-*")
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

	// Create a runner that uses the temp index.
	tempRunner := &git.Runner{
		Dir: runner.Dir,
		Env: []string{"GIT_INDEX_FILE=" + tmpPath},
	}

	// Build a map from path to parsed file diff for quick lookup.
	diffMap := make(map[string]*diff.FileDiff, len(parsedFiles))
	for i := range parsedFiles {
		diffMap[parsedFiles[i].Path] = &parsedFiles[i]
	}

	// Process each commit in order.
	for ci, c := range p.Commits {
		for _, f := range c.Files {
			if f.IsFullFile() {
				// Full-file: stage the file into the temp index.
				if err := tempRunner.Add(f.Path); err != nil {
					return output.NewExecutionError(
						fmt.Sprintf("validation failed at commit %d: cannot stage %s: %v", ci, f.Path, err),
						"Check that the file exists and has changes.",
					)
				}
			} else {
				// Hunk-select: get current diff against the temp index,
				// match hunks by fingerprint, build and apply a patch.
				currentRaw, err := tempRunner.DiffFile(f.Path, "-U0", "--no-ext-diff")
				if err != nil {
					return output.NewExecutionError(
						fmt.Sprintf("validation failed at commit %d: cannot diff %s against temp index: %v", ci, f.Path, err),
						"",
					)
				}

				currentFiles, err := diff.Parse(currentRaw)
				if err != nil {
					return output.NewExecutionError(
						fmt.Sprintf("validation failed at commit %d: cannot parse diff for %s: %v", ci, f.Path, err),
						"",
					)
				}

				if len(currentFiles) == 0 {
					return output.NewExecutionError(
						fmt.Sprintf("validation failed at commit %d: no diff for %s against temp index", ci, f.Path),
						"The file may have already been fully staged by a previous commit.",
					)
				}

				currentFile := currentFiles[0]

				// Fingerprint the current hunks.
				for j := range currentFile.Hunks {
					currentFile.Hunks[j].Fingerprint = diff.Fingerprint(currentFile.Hunks[j])
				}

				// Get the original file diff for matching.
				origFile, ok := diffMap[f.Path]
				if !ok {
					return output.NewExecutionError(
						fmt.Sprintf("validation failed at commit %d: %s not found in original diff", ci, f.Path),
						"",
					)
				}

				// Build subset of original hunks that this file entry references.
				origSubset := make([]diff.Hunk, 0, len(f.Hunks))
				for _, hunkIdx := range f.Hunks {
					if hunkIdx >= len(origFile.Hunks) {
						return output.NewExecutionError(
							fmt.Sprintf("validation failed at commit %d: hunk %d out of range for %s", ci, hunkIdx, f.Path),
							"",
						)
					}
					origSubset = append(origSubset, origFile.Hunks[hunkIdx])
				}

				// Match original hunks to current hunks using MatchHunks.
				matchMap, err := diff.MatchHunks(origSubset, currentFile.Hunks)
				if err != nil {
					return output.NewExecutionError(
						fmt.Sprintf("validation failed at commit %d: hunk matching failed for %s: %v", ci, f.Path, err),
						"A previous commit may have consumed or altered this hunk.",
					)
				}

				// Collect matched current hunk indices.
				selectedCurrentIndices := make([]int, 0, len(f.Hunks))
				for i := 0; i < len(origSubset); i++ {
					selectedCurrentIndices = append(selectedCurrentIndices, matchMap[i])
				}

				// Build a patch using the current file's hunks.
				patchData, err := patch.BuildPatch(currentFile, selectedCurrentIndices, currentFile.Hunks)
				if err != nil {
					return output.NewExecutionError(
						fmt.Sprintf("validation failed at commit %d: cannot build patch for %s: %v", ci, f.Path, err),
						"",
					)
				}

				// Apply the patch to the temp index.
				if err := patch.Apply(tempRunner, patchData); err != nil {
					return output.NewExecutionError(
						fmt.Sprintf("patch validation failed for %s hunks %v: %v", f.Path, f.Hunks, err),
						"This may indicate a diff parsing issue. Run 'ac diff' to inspect the current state.",
					)
				}
			}
		}
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

// collectHunkFilePaths returns unique file paths that use hunk-select mode.
func collectHunkFilePaths(p *plan.Plan) []string {
	seen := make(map[string]bool)
	var paths []string
	for _, c := range p.Commits {
		for _, f := range c.Files {
			if !f.IsFullFile() && !seen[f.Path] {
				seen[f.Path] = true
				paths = append(paths, f.Path)
			}
		}
	}
	return paths
}

// collectFullFilePaths returns unique file paths that use full-file mode.
func collectFullFilePaths(p *plan.Plan) []string {
	seen := make(map[string]bool)
	var paths []string
	for _, c := range p.Commits {
		for _, f := range c.Files {
			if f.IsFullFile() && !seen[f.Path] {
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

