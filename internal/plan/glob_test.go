package plan

import (
	"testing"

	"github.com/deligoez/hc/internal/diff"
)

// TestAllowUnplannedRecursiveGlob verifies that allow_unplanned supports
// '**' patterns matching across directory levels.
func TestAllowUnplannedRecursiveGlob(t *testing.T) {
	files := []diff.FileDiff{
		{Path: "experiments/deep/nested/wip.go", Hunks: []diff.Hunk{{Index: 0}}},
		{Path: "main.go", Hunks: []diff.Hunk{{Index: 0}}},
	}

	p := &Plan{
		Commits: []Commit{
			{Message: "feat: main", Files: []FileEntry{{Path: "main.go"}}},
		},
		AllowUnplanned: []string{"experiments/**"},
	}

	if err := ValidateCoverage(p, files); err != nil {
		t.Fatalf("'**' pattern should cover nested files: %v", err)
	}
}

// TestAllowUnplannedSingleStarIsOneLevel documents that a single '*'
// only matches one path level.
func TestAllowUnplannedSingleStarIsOneLevel(t *testing.T) {
	files := []diff.FileDiff{
		{Path: "experiments/deep/nested/wip.go", Hunks: []diff.Hunk{{Index: 0}}},
		{Path: "main.go", Hunks: []diff.Hunk{{Index: 0}}},
	}

	p := &Plan{
		Commits: []Commit{
			{Message: "feat: main", Files: []FileEntry{{Path: "main.go"}}},
		},
		AllowUnplanned: []string{"experiments/*"},
	}

	if err := ValidateCoverage(p, files); err == nil {
		t.Fatal("single '*' should NOT match nested paths; expected coverage error")
	}
}
