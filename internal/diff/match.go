package diff

import (
	"fmt"
	"math"
)

// MatchHunks maps original hunk indices to current hunk indices by content fingerprint.
// It uses a greedy algorithm: for each original hunk (in order), it finds candidates
// in the current hunks with matching fingerprints. If exactly one candidate exists,
// it is assigned. If multiple candidates share the same fingerprint, the one with the
// smallest |original.OldStart - candidate.OldStart| is chosen (ties broken by smaller
// OldStart). Assigned current hunks are removed from the candidate pool.
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

	result := make(map[int]int, len(original))

	for oi, oh := range original {
		fp := oh.Fingerprint
		if fp == "" {
			fp = Fingerprint(oh)
		}

		candidates, ok := pool[fp]
		if !ok || len(candidates) == 0 {
			return nil, fmt.Errorf("no matching hunk found for original hunk %d (old_start=%d)", oi, oh.OldStart)
		}

		chosen := -1
		if len(candidates) == 1 {
			chosen = 0
		} else {
			// Disambiguate by smallest |original.OldStart - candidate.OldStart|,
			// then smaller OldStart for ties.
			bestDist := int64(math.MaxInt64)
			bestStart := int64(math.MaxInt64)
			for ci, idx := range candidates {
				dist := abs64(oh.OldStart - current[idx].OldStart)
				if dist < bestDist || (dist == bestDist && current[idx].OldStart < bestStart) {
					bestDist = dist
					bestStart = current[idx].OldStart
					chosen = ci
				}
			}
		}

		result[oi] = candidates[chosen]

		// Remove assigned candidate from pool.
		candidates[chosen] = candidates[len(candidates)-1]
		pool[fp] = candidates[:len(candidates)-1]
	}

	return result, nil
}

func abs64(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}
