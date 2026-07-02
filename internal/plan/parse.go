package plan

import (
	"encoding/json"
	"fmt"

	"github.com/deligoez/hc/internal/output"
)

// Parse decodes JSON bytes into a Plan and validates its top-level structure.
// Per-commit and per-file validation (messages, paths, hunk indices) lives in
// ValidateFields so each rule has exactly one implementation and one
// spec-defined error message.
func Parse(data []byte) (*Plan, error) {
	var p Plan
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, output.NewValidationError(
			fmt.Sprintf("plan parse error: %v", err),
			"Check JSON syntax. Plan must have a \"commits\" array.",
		)
	}

	// The Commits field is a slice; after unmarshalling it will be nil if the
	// key was absent, or an empty slice if it was `"commits": []`.
	if p.Commits == nil {
		return nil, output.NewValidationError(
			"plan missing \"commits\" array",
			"Plan must have a \"commits\" array with at least one commit.",
		)
	}

	if len(p.Commits) == 0 {
		return nil, output.NewValidationError(
			"plan has no commits",
			"Add at least one commit to the \"commits\" array.",
		)
	}

	// Normalize empty hunks slices to nil (full-file mode).
	for i := range p.Commits {
		for j := range p.Commits[i].Files {
			f := &p.Commits[i].Files[j]
			if f.Hunks != nil && len(f.Hunks) == 0 {
				f.Hunks = nil
			}
		}
	}

	return &p, nil
}
