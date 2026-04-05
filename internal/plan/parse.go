package plan

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/deligoez/ac/internal/output"
)

// Parse decodes JSON bytes into a Plan and validates its structure.
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

	for i := range p.Commits {
		c := &p.Commits[i]

		if strings.TrimSpace(c.Message) == "" {
			return nil, output.NewValidationError(
				"commit has an empty message",
				"Every commit must have a non-empty \"message\" field.",
			)
		}

		if len(c.Files) == 0 {
			return nil, output.NewValidationError(
				"commit has no files",
				"Every commit must list at least one file in the \"files\" array.",
			)
		}

		for j := range c.Files {
			f := &c.Files[j]

			if strings.TrimSpace(f.Path) == "" {
				return nil, output.NewValidationError(
					"file entry has an empty path",
					"Every file must have a non-empty \"path\" field.",
				)
			}

			// Reject absolute paths and paths containing "..".
			if filepath.IsAbs(f.Path) {
				return nil, output.NewValidationError(
					"file path must be relative: "+f.Path,
					"Use relative paths from the repository root without \"..\" components.",
				)
			}
			for _, part := range strings.Split(filepath.ToSlash(f.Path), "/") {
				if part == ".." {
					return nil, output.NewValidationError(
						"file path contains \"..\" component: "+f.Path,
						"Use relative paths from the repository root without \"..\" components.",
					)
				}
			}

			// Normalize empty hunks slice to nil (full-file mode).
			if f.Hunks != nil && len(f.Hunks) == 0 {
				f.Hunks = nil
			}
		}
	}

	return &p, nil
}
