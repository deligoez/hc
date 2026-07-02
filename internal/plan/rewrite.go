package plan

import (
	"encoding/json"
	"fmt"

	"github.com/deligoez/hc/internal/output"
)

// RewritePlan maps existing commits to the sub-commits that should replace
// them. Commits on the branch that are not listed stay as they are (their
// SHAs still change because their ancestry changes).
type RewritePlan struct {
	Rewrites []Rewrite `json:"rewrites"`
}

// Rewrite splits one existing commit into one or more replacement commits.
type Rewrite struct {
	// Commit is the SHA (or unique prefix) of the commit to split.
	Commit string `json:"commit"`
	// Commits are the replacements, in order. Together they must cover every
	// hunk of the original commit's diff exactly once -- the same complete
	// coverage guarantee hc run applies to the working tree.
	Commits []Commit `json:"commits"`
}

// ParseRewrite decodes and structurally validates a rewrite plan.
func ParseRewrite(data []byte) (*RewritePlan, error) {
	var p RewritePlan
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, output.NewValidationError(
			fmt.Sprintf("rewrite plan parse error: %v", err),
			"Check JSON syntax. Plan must have a \"rewrites\" array.",
		)
	}
	if len(p.Rewrites) == 0 {
		return nil, output.NewValidationError(
			"rewrite plan has no rewrites",
			"Add at least one entry to the \"rewrites\" array.",
		)
	}

	seen := make(map[string]bool)
	for i, r := range p.Rewrites {
		if r.Commit == "" {
			return nil, output.NewValidationError(
				fmt.Sprintf("rewrite %d has no commit", i),
				"Each rewrite must name the commit SHA to split.",
			)
		}
		if seen[r.Commit] {
			return nil, output.NewValidationError(
				fmt.Sprintf("commit %s appears in more than one rewrite", r.Commit),
				"Merge the entries: each commit can be rewritten once.",
			)
		}
		seen[r.Commit] = true
		if len(r.Commits) == 0 {
			return nil, output.NewValidationError(
				fmt.Sprintf("rewrite of %s has no replacement commits", r.Commit),
				"List at least one replacement commit.",
			)
		}
		// Normalize empty hunks slices to nil (full-file mode), mirroring Parse.
		for ci := range r.Commits {
			for fi := range r.Commits[ci].Files {
				f := &r.Commits[ci].Files[fi]
				if f.Hunks != nil && len(f.Hunks) == 0 {
					f.Hunks = nil
				}
			}
		}
	}
	return &p, nil
}
