package plan

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/deligoez/ac/internal/diff"
	"github.com/deligoez/ac/internal/output"
)

// ValidateFields validates plan fields after parsing.
// It checks for negative hunk indices, duplicate hunks, path safety,
// full-file in multiple commits, and mixed full-file/hunk-select modes.
func ValidateFields(p *Plan) error {
	// Track per-file: which commits use full-file, which use hunk-select,
	// and which (commitIdx, hunkIdx) pairs are assigned.
	type hunkKey struct {
		commitIdx int
		hunkIdx   int
	}

	// fullFileCommits[path] = list of commit indices using full-file mode
	fullFileCommits := make(map[string][]int)
	// hunkSelectCommits[path] = list of commit indices using hunk-select mode
	hunkSelectCommits := make(map[string][]int)
	// hunkAssignments[path][hunkIdx] = commit index (first seen)
	hunkAssignments := make(map[string]map[int]int)

	for i, c := range p.Commits {
		if strings.TrimSpace(c.Message) == "" {
			return output.NewValidationError(
				fmt.Sprintf("commit %d has empty message", i),
				"Each commit must have a non-empty message string.",
			)
		}

		if len(c.Files) == 0 {
			return output.NewValidationError(
				fmt.Sprintf("commit %d has no files", i),
				"Each commit must include at least one file.",
			)
		}

		for _, f := range c.Files {
			// Path safety
			if strings.TrimSpace(f.Path) == "" {
				return output.NewValidationError(
					fmt.Sprintf("file path is empty in commit %d", i),
					"Each file must have a non-empty relative path.",
				)
			}
			if filepath.IsAbs(f.Path) {
				return output.NewValidationError(
					fmt.Sprintf("file path %q must be relative to repo root", f.Path),
					"Use a relative path (e.g., \"src/foo.go\").",
				)
			}
			for _, part := range strings.Split(filepath.ToSlash(f.Path), "/") {
				if part == ".." {
					return output.NewValidationError(
						fmt.Sprintf("file path %q contains \"..\" traversal", f.Path),
						"Use paths relative to the repo root without \"..\" components.",
					)
				}
			}

			if f.IsFullFile() {
				fullFileCommits[f.Path] = append(fullFileCommits[f.Path], i)
			} else {
				hunkSelectCommits[f.Path] = append(hunkSelectCommits[f.Path], i)

				// Check for negative and duplicate hunk indices within this file entry.
				seen := make(map[int]bool)
				for _, h := range f.Hunks {
					if h < 0 {
						return output.NewValidationError(
							fmt.Sprintf("hunk index %d is invalid for %s", h, f.Path),
							"Hunk indices must be non-negative integers.",
						)
					}
					if seen[h] {
						return output.NewValidationError(
							fmt.Sprintf("hunk index %d duplicated in %s (commit %d)", h, f.Path, i),
							"Remove the duplicate index from the hunks array.",
						)
					}
					seen[h] = true

					// Check cross-commit duplicate hunk assignment.
					if hunkAssignments[f.Path] == nil {
						hunkAssignments[f.Path] = make(map[int]int)
					}
					if prevCommit, exists := hunkAssignments[f.Path][h]; exists {
						return output.NewValidationError(
							fmt.Sprintf("hunk %d of %s assigned to both commit %d and commit %d", h, f.Path, prevCommit, i),
							"Each hunk must appear in exactly one commit.",
						)
					}
					hunkAssignments[f.Path][h] = i
				}
			}
		}
	}

	// Check full-file in multiple commits.
	for path, commits := range fullFileCommits {
		if len(commits) > 1 {
			return output.NewValidationError(
				fmt.Sprintf("%s appears in full-file mode in commits %d and %d", path, commits[0], commits[1]),
				"A file in full-file mode can only appear in one commit. Use hunk-select mode to split across commits.",
			)
		}
	}

	// Check mixed full-file/hunk-select for same file.
	for path, ffCommits := range fullFileCommits {
		if hsCommits, ok := hunkSelectCommits[path]; ok {
			return output.NewValidationError(
				fmt.Sprintf("%s uses full-file mode in commit %d and hunk-select in commit %d", path, ffCommits[0], hsCommits[0]),
				"A file in full-file mode stages everything. Use hunk-select mode in all commits, or put the file in exactly one commit with full-file mode.",
			)
		}
	}

	return nil
}

// ValidateCoverage validates the plan against the parsed diff output.
// It checks that all referenced files exist in the diff, hunk indices
// are in range, binary files don't use hunks, and all diff hunks are
// covered by the plan.
func ValidateCoverage(p *Plan, files []diff.FileDiff) error {
	// Build a map from path to FileDiff.
	diffMap := make(map[string]*diff.FileDiff, len(files))
	for i := range files {
		diffMap[files[i].Path] = &files[i]
	}

	// Collect all hunk assignments: path -> set of assigned hunk indices.
	assignedHunks := make(map[string]map[int]bool)
	// Track full-file paths.
	fullFilePaths := make(map[string]bool)
	// Track hunk assignments for duplicate detection: path -> hunkIdx -> commitIdx.
	hunkCommitMap := make(map[string]map[int]int)

	for i, c := range p.Commits {
		for _, f := range c.Files {
			fd, inDiff := diffMap[f.Path]

			if !inDiff {
				// If file is not in diff, it could be untracked (caller handles
				// intent-to-add). We only error if the file truly doesn't exist
				// in the diff. For full-file mode on new/untracked files, the
				// caller adds them via git add -N before capturing the diff.
				// So if we reach here, the file genuinely has no changes.
				return output.NewValidationError(
					fmt.Sprintf("file %q has no changes in the working tree", f.Path),
					"Only include files with uncommitted changes. Run 'ac diff' to see changed files.",
				)
			}

			if f.IsFullFile() {
				fullFilePaths[f.Path] = true
			} else {
				// Binary file with hunks.
				if fd.IsBinary {
					return output.NewValidationError(
						fmt.Sprintf("%s is a binary file and cannot be split into hunks", f.Path),
						"Remove the \"hunks\" field to stage the entire file.",
					)
				}

				if assignedHunks[f.Path] == nil {
					assignedHunks[f.Path] = make(map[int]bool)
				}
				if hunkCommitMap[f.Path] == nil {
					hunkCommitMap[f.Path] = make(map[int]int)
				}

				for _, h := range f.Hunks {
					// Hunk index out of range.
					if h >= len(fd.Hunks) {
						return output.NewValidationError(
							fmt.Sprintf("hunk index %d out of range for %s (has %d hunks, indices 0-%d)", h, f.Path, len(fd.Hunks), len(fd.Hunks)-1),
							"Run 'ac diff --json' to see available hunks.",
						)
					}

					// Duplicate hunk across commits.
					if prevCommit, exists := hunkCommitMap[f.Path][h]; exists && prevCommit != i {
						return output.NewValidationError(
							fmt.Sprintf("hunk %d of %s assigned to both commit %d and commit %d", h, f.Path, prevCommit, i),
							"Each hunk must appear in exactly one commit.",
						)
					}
					hunkCommitMap[f.Path][h] = i
					assignedHunks[f.Path][h] = true
				}
			}
		}
	}

	// Build allow_unplanned matcher set.
	allowUnplanned := make(map[string]bool)
	for _, pattern := range p.AllowUnplanned {
		allowUnplanned[pattern] = true
	}

	isAllowedUnplanned := func(path string) bool {
		for pattern := range allowUnplanned {
			if matched, _ := filepath.Match(pattern, path); matched {
				return true
			}
		}
		return false
	}

	// Check complete coverage: every diff file must be in the plan.
	for _, fd := range files {
		if isAllowedUnplanned(fd.Path) {
			continue
		}

		if fullFilePaths[fd.Path] {
			// Full-file mode covers everything.
			continue
		}

		assigned, inPlan := assignedHunks[fd.Path]
		if !inPlan {
			return output.NewValidationError(
				fmt.Sprintf("%s has changes but is not in the plan", fd.Path),
				fmt.Sprintf("Add %s to a commit, or add %q to allow_unplanned.", fd.Path, fd.Path),
			)
		}

		// Check that all hunks are assigned.
		var missing []int
		for j := range fd.Hunks {
			if !assigned[j] {
				missing = append(missing, j)
			}
		}
		if len(missing) > 0 {
			missingStr := fmt.Sprintf("%d", missing[0])
			for _, m := range missing[1:] {
				missingStr += fmt.Sprintf(", %d", m)
			}
			hintIndices := fmt.Sprintf("%d", missing[0])
			for _, m := range missing[1:] {
				hintIndices += fmt.Sprintf(",%d", m)
			}
			return output.NewValidationError(
				fmt.Sprintf("%s hunks [%s] not assigned to any commit", fd.Path, missingStr),
				fmt.Sprintf("Assign %s hunks %s to a commit, or add %q to allow_unplanned.", fd.Path, hintIndices, fd.Path),
			)
		}
	}

	return nil
}
