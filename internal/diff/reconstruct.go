package diff

import (
	"bytes"
	"fmt"
)

// SplitLines splits b into lines, each keeping its trailing newline. The
// final line may lack one (no-EOL file). Empty input yields no lines.
func SplitLines(b []byte) [][]byte {
	var lines [][]byte
	for len(b) > 0 {
		i := bytes.IndexByte(b, '\n')
		if i < 0 {
			lines = append(lines, b)
			break
		}
		lines = append(lines, b[:i+1])
		b = b[i+1:]
	}
	return lines
}

// effStart returns the 0-based index in the base line slice where the hunk
// takes effect. For deletions/replacements OldStart is the 1-based first
// deleted line; for pure insertions (OldCount == 0) git's convention is
// "insert after line OldStart", i.e. before 0-based index OldStart.
func effStart(h Hunk) int {
	if h.OldCount == 0 {
		return int(h.OldStart)
	}
	return int(h.OldStart) - 1
}

// Reconstruct applies the given -U0 hunks to base and returns the resulting
// content. All hunks must come from a single diff against base (so they are
// ordered and non-overlapping in base coordinates); any subset of that diff's
// hunks is valid input. Every delete line is verified byte-for-byte against
// base, so working-tree drift is detected instead of silently mis-applied.
//
// This is the core of hc's staging: instead of adjusting patch line numbers
// and re-matching hunks after each commit (fragile when git re-splits or
// slides hunk windows over repeated content), the staged file content is
// rebuilt deterministically from original diff coordinates.
func Reconstruct(base []byte, hunks []Hunk) ([]byte, error) {
	lines := SplitLines(base)
	var out bytes.Buffer
	cursor := 0 // next base line (0-based) not yet emitted

	for _, h := range hunks {
		start := effStart(h)
		if start < cursor {
			return nil, fmt.Errorf("hunk at old line %d overlaps a previous hunk", h.OldStart)
		}
		if start > len(lines) {
			return nil, fmt.Errorf("hunk at old line %d is beyond end of file (%d lines)", h.OldStart, len(lines))
		}

		// Emit unchanged lines up to the hunk.
		for ; cursor < start; cursor++ {
			out.Write(lines[cursor])
		}

		// Consume and verify deleted lines, collect added lines.
		deleted := 0
		for _, l := range h.Lines {
			switch l.Op {
			case OpDelete:
				if cursor >= len(lines) {
					return nil, fmt.Errorf("hunk at old line %d deletes past end of file", h.OldStart)
				}
				if string(lines[cursor]) != l.Content {
					return nil, fmt.Errorf("hunk at old line %d: base line %d does not match the captured diff", h.OldStart, cursor+1)
				}
				cursor++
				deleted++
			case OpAdd:
				out.WriteString(l.Content)
			}
		}
		if int64(deleted) != h.OldCount {
			return nil, fmt.Errorf("hunk at old line %d: header says %d deletions, body has %d", h.OldStart, h.OldCount, deleted)
		}
	}

	for ; cursor < len(lines); cursor++ {
		out.Write(lines[cursor])
	}

	return out.Bytes(), nil
}
