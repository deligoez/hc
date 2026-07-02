package diff

import (
	"fmt"
	"math"

	"github.com/deligoez/hc/internal/output"
)

// MatchHunks maps original hunk indices to current hunk indices by content fingerprint.
// It uses a greedy algorithm: for each original hunk (in order), it finds candidates
// in the current hunks with matching fingerprints. If exactly one candidate exists,
// it is assigned. If multiple candidates share the same fingerprint, the one with the
// smallest |original.OldStart - candidate.OldStart| is chosen (ties broken by smaller
// OldStart). Assigned current hunks are removed from the candidate pool.
//
// Fallback: if no exact fingerprint match is found, the algorithm tries content-subset
// matching. This handles the case where git merges adjacent hunks after earlier commits
// change line counts — a current hunk may contain all lines of the original hunk plus
// lines from neighboring hunks. The candidate with the best OldStart proximity wins.
// Returns an error if any original hunk has no matching candidate.
func MatchHunks(original, current []Hunk) (map[int]int, error) {
	// Build fingerprint -> list of current hunk indices.
	pool := make(map[string][]int)
	for i, h := range current {
		fp := h.Fingerprint
		if fp == "" {
			fp = Fingerprint(h)
		}
		pool[fp] = append(pool[fp], i)
	}

	// Track which current hunks are already assigned (for subset fallback).
	assigned := make(map[int]bool)

	result := make(map[int]int, len(original))

	for oi, oh := range original {
		fp := oh.Fingerprint
		if fp == "" {
			fp = Fingerprint(oh)
		}

		candidates, ok := pool[fp]
		if ok && len(candidates) > 0 {
			// Exact fingerprint match.
			chosen := pickClosest(oh, candidates, current)
			result[oi] = candidates[chosen]
			assigned[candidates[chosen]] = true

			// Remove assigned candidate from pool.
			candidates[chosen] = candidates[len(candidates)-1]
			pool[fp] = candidates[:len(candidates)-1]
			continue
		}

		// Fallback: content-subset matching.
		// The original hunk's lines may be a subset of a merged current hunk.
		// A second, order-insensitive pass handles git sliding an ambiguous
		// hunk window across repeated content: the same semantic change is
		// reported with rotated line order, so ordered subsequence matching
		// fails even though the change is identical.
		bestIdx := -1
		bestDist := int64(math.MaxInt64)
		bestStart := int64(math.MaxInt64)
		for _, ordered := range []bool{true, false} {
			for ci, ch := range current {
				if assigned[ci] {
					continue
				}
				matched := false
				if ordered {
					matched = containsAllLines(ch, oh)
				} else {
					matched = containsAllLinesMultiset(ch, oh)
				}
				if matched {
					dist := abs64(oh.OldStart - ch.OldStart)
					if dist < bestDist || (dist == bestDist && ch.OldStart < bestStart) {
						bestDist = dist
						bestStart = ch.OldStart
						bestIdx = ci
					}
				}
			}
			if bestIdx >= 0 {
				break
			}
		}

		if bestIdx >= 0 {
			result[oi] = bestIdx
			// Do NOT remove from pool — merged hunks can match multiple originals.
			// But mark as used for proximity fallback ordering.
			continue
		}

		return nil, output.NewExecutionError(
			fmt.Sprintf("no matching hunk found for original hunk %d (old_start=%d)", oi, oh.OldStart),
			"Hunk content changed between validation and execution. Re-run 'hc diff' and rebuild the plan.",
		)
	}

	return result, nil
}

func abs64(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

// pickClosest selects the candidate index closest to the original hunk by OldStart.
func pickClosest(oh Hunk, candidates []int, current []Hunk) int {
	if len(candidates) == 1 {
		return 0
	}
	bestDist := int64(math.MaxInt64)
	bestStart := int64(math.MaxInt64)
	chosen := 0
	for ci, idx := range candidates {
		dist := abs64(oh.OldStart - current[idx].OldStart)
		if dist < bestDist || (dist == bestDist && current[idx].OldStart < bestStart) {
			bestDist = dist
			bestStart = current[idx].OldStart
			chosen = ci
		}
	}
	return chosen
}

// containsAllLines checks if the current hunk contains all delete and add lines
// from the original hunk, in order. This handles the case where git merges
// adjacent hunks into one larger hunk.
func containsAllLines(current, original Hunk) bool {
	origDels := linesOfOp(original, OpDelete)
	origAdds := linesOfOp(original, OpAdd)
	curDels := linesOfOp(current, OpDelete)
	curAdds := linesOfOp(current, OpAdd)

	return isSubsequence(origDels, curDels) && isSubsequence(origAdds, curAdds)
}

// linesOfOp extracts line content for a given operation type.
func linesOfOp(h Hunk, op LineOp) []string {
	var lines []string
	for _, l := range h.Lines {
		if l.Op == op {
			lines = append(lines, l.Content)
		}
	}
	return lines
}

// isSubsequence checks if needle is a subsequence of haystack (same order, not necessarily contiguous).
func isSubsequence(needle, haystack []string) bool {
	if len(needle) == 0 {
		return true
	}
	ni := 0
	for _, h := range haystack {
		if h == needle[ni] {
			ni++
			if ni == len(needle) {
				return true
			}
		}
	}
	return false
}
