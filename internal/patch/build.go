package patch

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/deligoez/hc/internal/diff"
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
			noEOL := len(l.Content) > 0 && !strings.HasSuffix(l.Content, "\n")
			content := strings.TrimSuffix(l.Content, "\n")
			switch l.Op {
			case diff.OpDelete:
				fmt.Fprintf(&buf, "-%s\n", content)
			case diff.OpAdd:
				fmt.Fprintf(&buf, "+%s\n", content)
			case diff.OpContext:
				fmt.Fprintf(&buf, " %s\n", content)
			}
			if noEOL {
				buf.WriteString("\\ No newline at end of file\n")
			}
		}
	}

	return buf.Bytes(), nil
}

// BuildCompositePatch builds a patch that handles both exact-match and merged
// hunks in the same file. For exact-match hunks it emits the full current hunk
// (with delta accumulation for skipped hunks). For merged hunks it extracts
// only the lines belonging to the commit's original hunks.
func BuildCompositePatch(file diff.FileDiff, selectedIndices []int, allHunks []diff.Hunk, currentToOrigs map[int][]diff.Hunk) ([]byte, error) {
	selected := make(map[int]bool, len(selectedIndices))
	for _, i := range selectedIndices {
		selected[i] = true
	}

	var buf bytes.Buffer

	// --- header (same as BuildPatch) ---
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

		origs := currentToOrigs[h.Index]
		if !IsMergedHunk(h, origs) {
			// Exact match: emit the full current hunk with delta adjustment.
			adjustedNewStart := h.NewStart + delta
			fmt.Fprintf(&buf, "@@ -%d,%d +%d,%d @@\n", h.OldStart, h.OldCount, adjustedNewStart, h.NewCount)

			for _, l := range h.Lines {
				noEOL := len(l.Content) > 0 && !strings.HasSuffix(l.Content, "\n")
				content := strings.TrimSuffix(l.Content, "\n")
				switch l.Op {
				case diff.OpDelete:
					fmt.Fprintf(&buf, "-%s\n", content)
				case diff.OpAdd:
					fmt.Fprintf(&buf, "+%s\n", content)
				case diff.OpContext:
					fmt.Fprintf(&buf, " %s\n", content)
				}
				if noEOL {
					buf.WriteString("\\ No newline at end of file\n")
				}
			}
		} else {
			// Merged hunk: extract only the lines belonging to our original hunks.
			subOld, subNew, subLines := extractSubLines(h, origs)

			adjustedNewStart := h.NewStart + delta
			fmt.Fprintf(&buf, "@@ -%d,%d +%d,%d @@\n", h.OldStart, subOld, adjustedNewStart, subNew)

			for _, l := range subLines {
				noEOL := len(l.Content) > 0 && !strings.HasSuffix(l.Content, "\n")
				content := strings.TrimSuffix(l.Content, "\n")
				switch l.Op {
				case diff.OpDelete:
					fmt.Fprintf(&buf, "-%s\n", content)
				case diff.OpAdd:
					fmt.Fprintf(&buf, "+%s\n", content)
				case diff.OpContext:
					fmt.Fprintf(&buf, " %s\n", content)
				}
				if noEOL {
					buf.WriteString("\\ No newline at end of file\n")
				}
			}

			// For delta purposes, the merged hunk's skipped portion contributes
			// its own delta. We applied subOld deletes and subNew adds, so
			// the remaining (h.OldCount - subOld) deletes and (h.NewCount - subNew)
			// adds are effectively skipped.
			delta += (h.OldCount - subOld) - (h.NewCount - subNew)
		}
	}

	return buf.Bytes(), nil
}

// extractSubLines picks only the delete/add lines from a merged hunk that
// belong to the given original hunks. Returns (oldCount, newCount, lines).
func extractSubLines(merged diff.Hunk, origs []diff.Hunk) (int64, int64, []diff.Line) {
	// Collect expected delete and add lines from original hunks.
	var wantDels, wantAdds []string
	for _, oh := range origs {
		for _, l := range oh.Lines {
			switch l.Op {
			case diff.OpDelete:
				wantDels = append(wantDels, l.Content)
			case diff.OpAdd:
				wantAdds = append(wantAdds, l.Content)
			}
		}
	}

	delCounts := countOccurrences(wantDels)
	addCounts := countOccurrences(wantAdds)

	var subLines []diff.Line
	var oldCount, newCount int64

	for _, l := range merged.Lines {
		switch l.Op {
		case diff.OpDelete:
			if delCounts[l.Content] > 0 {
				delCounts[l.Content]--
				subLines = append(subLines, l)
				oldCount++
			}
		case diff.OpAdd:
			if addCounts[l.Content] > 0 {
				addCounts[l.Content]--
				subLines = append(subLines, l)
				newCount++
			}
		}
	}

	return oldCount, newCount, subLines
}

// BuildSubPatch creates a patch containing only the lines from a merged current
// hunk that match the original hunks' content. This handles the case where git
// merged adjacent hunks after earlier commits were applied, so a single current
// hunk contains lines from multiple original hunks.
//
// The algorithm extracts the subsequence of delete/add lines from currentHunk
// that appear in origHunks (by content matching), then builds a minimal patch
// with correct @@ line counts.
func BuildSubPatch(file diff.FileDiff, currentHunk diff.Hunk, origHunks []diff.Hunk) ([]byte, error) {
	// Collect all expected delete and add lines from the original hunks.
	var wantDels, wantAdds []string
	for _, oh := range origHunks {
		for _, l := range oh.Lines {
			switch l.Op {
			case diff.OpDelete:
				wantDels = append(wantDels, l.Content)
			case diff.OpAdd:
				wantAdds = append(wantAdds, l.Content)
			}
		}
	}

	// Build sets for O(1) lookup. We use an occurrence-counted map
	// to handle duplicate line contents correctly.
	delCounts := countOccurrences(wantDels)
	addCounts := countOccurrences(wantAdds)

	// Walk through the current (merged) hunk's lines and pick only
	// those that belong to our original hunks.
	var subLines []diff.Line
	var oldCount, newCount int64

	// Track the old-side line position within the current hunk to compute
	// the correct OldStart for the sub-patch. We want the position of the
	// first matching delete line.
	oldLinePos := currentHunk.OldStart
	firstOldPos := int64(-1)
	// Track new-side position for NewStart.
	newLinePos := currentHunk.NewStart
	firstNewPos := int64(-1)

	// We also need to count skipped old-side lines BEFORE the first match
	// to know the correct OldStart, and skipped new-side lines for NewStart.
	for _, l := range currentHunk.Lines {
		switch l.Op {
		case diff.OpDelete:
			if delCounts[l.Content] > 0 {
				delCounts[l.Content]--
				subLines = append(subLines, l)
				oldCount++
				if firstOldPos < 0 {
					firstOldPos = oldLinePos
				}
				if firstNewPos < 0 {
					firstNewPos = newLinePos
				}
			}
			oldLinePos++
		case diff.OpAdd:
			if addCounts[l.Content] > 0 {
				addCounts[l.Content]--
				subLines = append(subLines, l)
				newCount++
				if firstNewPos < 0 {
					firstNewPos = newLinePos
				}
				if firstOldPos < 0 {
					firstOldPos = oldLinePos
				}
			}
			newLinePos++
		case diff.OpContext:
			oldLinePos++
			newLinePos++
		}
	}

	if firstOldPos < 0 {
		firstOldPos = currentHunk.OldStart
	}
	if firstNewPos < 0 {
		firstNewPos = currentHunk.NewStart
	}

	// Build the patch.
	var buf bytes.Buffer

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

	fmt.Fprintf(&buf, "@@ -%d,%d +%d,%d @@\n", firstOldPos, oldCount, firstNewPos, newCount)

	for _, l := range subLines {
		noEOL := len(l.Content) > 0 && !strings.HasSuffix(l.Content, "\n")
		content := strings.TrimSuffix(l.Content, "\n")
		switch l.Op {
		case diff.OpDelete:
			fmt.Fprintf(&buf, "-%s\n", content)
		case diff.OpAdd:
			fmt.Fprintf(&buf, "+%s\n", content)
		case diff.OpContext:
			fmt.Fprintf(&buf, " %s\n", content)
		}
		if noEOL {
			buf.WriteString("\\ No newline at end of file\n")
		}
	}

	return buf.Bytes(), nil
}

// countOccurrences builds a map of string -> count for duplicate-safe matching.
func countOccurrences(lines []string) map[string]int {
	m := make(map[string]int, len(lines))
	for _, l := range lines {
		m[l]++
	}
	return m
}

// IsMergedHunk reports whether currentHunk is a merged hunk: one that does
// not exactly correspond to any of the original hunks mapped to it. A hunk
// with a matching fingerprint, or with identical content as a multiset (git
// slid an ambiguous window across repeated content, rotating line order), is
// treated as exact -- applying the current hunk as-is yields the same result.
func IsMergedHunk(currentHunk diff.Hunk, origHunks []diff.Hunk) bool {
	cfp := currentHunk.Fingerprint
	if cfp == "" {
		cfp = diff.Fingerprint(currentHunk)
	}
	for _, oh := range origHunks {
		fp := oh.Fingerprint
		if fp == "" {
			fp = diff.Fingerprint(oh)
		}
		if cfp == fp {
			return false
		}
		if diff.EqualContentMultiset(currentHunk, oh) {
			return false
		}
	}
	return true
}
