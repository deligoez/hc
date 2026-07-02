package diff

import (
	"fmt"
	"strings"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
)

// Parse parses raw git diff -U0 output into a slice of FileDiff.
func Parse(raw string) ([]FileDiff, error) {
	if strings.TrimSpace(raw) == "" {
		return []FileDiff{}, nil
	}

	files, _, err := gitdiff.Parse(strings.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("parsing diff: %w", err)
	}

	result := make([]FileDiff, 0, len(files))
	for _, f := range files {
		fd := FileDiff{
			Path:      f.NewName,
			IsBinary:  f.IsBinary,
			IsNew:     f.IsNew,
			IsDeleted: f.IsDelete,
			IsRenamed: f.IsRename,
		}

		if f.IsDelete {
			fd.Path = f.OldName
		}

		if f.IsRename {
			fd.OldPath = f.OldName
		}

		if f.OldMode != 0 {
			fd.OldMode = fmt.Sprintf("%06o", f.OldMode)
		}
		if f.NewMode != 0 {
			fd.NewMode = fmt.Sprintf("%06o", f.NewMode)
		}

		for i, frag := range f.TextFragments {
			h := Hunk{
				Index:    i,
				OldStart: frag.OldPosition,
				OldCount: frag.OldLines,
				NewStart: frag.NewPosition,
				NewCount: frag.NewLines,
				Section:  frag.Comment,
			}

			for _, line := range frag.Lines {
				// Skip "no newline" markers
				if line.Op == gitdiff.OpContext && line.Line == "" && isNoEOL(line) {
					continue
				}

				var op LineOp
				switch line.Op {
				case gitdiff.OpAdd:
					op = OpAdd
				case gitdiff.OpDelete:
					op = OpDelete
				case gitdiff.OpContext:
					op = OpContext
				}

				h.Lines = append(h.Lines, Line{
					Op:      op,
					Content: line.Line,
				})
			}

			fd.Hunks = append(fd.Hunks, h)
		}

		result = append(result, fd)
	}

	return result, nil
}

// isNoEOL checks if a line is the "\ No newline at end of file" marker.
func isNoEOL(line gitdiff.Line) bool {
	return line.NoEOL()
}
