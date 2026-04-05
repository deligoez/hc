package patch

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/deligoez/ac/internal/diff"
)

// BuildPatch produces a git-compatible unified diff patch for the selected hunks
// of a file, adjusting new-start line numbers using delta accumulation for any
// skipped hunks.
func BuildPatch(file diff.FileDiff, selectedIndices []int, allHunks []diff.Hunk) ([]byte, error) {
	selected := make(map[int]bool, len(selectedIndices))
	for _, i := range selectedIndices {
		selected[i] = true
	}

	var buf bytes.Buffer

	// --- header ---
	oldPath := file.Path
	if file.IsRenamed && file.OldPath != "" {
		oldPath = file.OldPath
	}
	fmt.Fprintf(&buf, "diff --git a/%s b/%s\n", oldPath, file.Path)

	if file.IsNew {
		fmt.Fprintf(&buf, "new file mode %s\n", file.NewMode)
	} else if file.IsDeleted {
		fmt.Fprintf(&buf, "deleted file mode %s\n", file.OldMode)
	} else {
		if file.OldMode != "" && file.NewMode != "" && file.OldMode != file.NewMode {
			fmt.Fprintf(&buf, "old mode %s\nnew mode %s\n", file.OldMode, file.NewMode)
		}
	}

	if file.IsRenamed {
		fmt.Fprintf(&buf, "rename from %s\nrename to %s\n", file.OldPath, file.Path)
	}

	// --- / +++ ---
	if file.IsNew {
		fmt.Fprintf(&buf, "--- /dev/null\n")
	} else {
		fmt.Fprintf(&buf, "--- a/%s\n", oldPath)
	}

	if file.IsDeleted {
		fmt.Fprintf(&buf, "+++ /dev/null\n")
	} else {
		fmt.Fprintf(&buf, "+++ b/%s\n", file.Path)
	}

	// --- hunks with delta accumulation ---
	var delta int64
	for _, h := range allHunks {
		if !selected[h.Index] {
			// skipped: accumulate delta
			delta += h.OldCount - h.NewCount
			continue
		}

		adjustedNewStart := h.NewStart + delta
		fmt.Fprintf(&buf, "@@ -%d,%d +%d,%d @@\n", h.OldStart, h.OldCount, adjustedNewStart, h.NewCount)

		for _, l := range h.Lines {
			content := strings.TrimSuffix(l.Content, "\n")
			switch l.Op {
			case diff.OpDelete:
				fmt.Fprintf(&buf, "-%s\n", content)
			case diff.OpAdd:
				fmt.Fprintf(&buf, "+%s\n", content)
			case diff.OpContext:
				fmt.Fprintf(&buf, " %s\n", content)
			}
		}
	}

	return buf.Bytes(), nil
}
