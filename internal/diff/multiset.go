package diff

// containsAllLinesMultiset reports whether the current hunk contains every
// delete and add line of the original at least as many times, ignoring order.
// Git may slide an ambiguous hunk window across repeated content, reporting
// the same semantic change with rotated line order; multiset containment
// still identifies the hunk. This is safe because the patch that gets applied
// is built from the CURRENT hunk -- a rotated selection of identical content
// produces an identical result.
func containsAllLinesMultiset(current, original Hunk) bool {
	return isMultisetSubset(linesOfOp(original, OpDelete), linesOfOp(current, OpDelete)) &&
		isMultisetSubset(linesOfOp(original, OpAdd), linesOfOp(current, OpAdd))
}

// EqualContentMultiset reports whether two hunks have identical delete and
// add lines as multisets (same lines, same counts, order ignored). Used to
// recognize a rotated-but-identical hunk as an exact match.
func EqualContentMultiset(a, b Hunk) bool {
	aDels, bDels := linesOfOp(a, OpDelete), linesOfOp(b, OpDelete)
	aAdds, bAdds := linesOfOp(a, OpAdd), linesOfOp(b, OpAdd)
	if len(aDels) != len(bDels) || len(aAdds) != len(bAdds) {
		return false
	}
	return isMultisetSubset(aDels, bDels) && isMultisetSubset(aAdds, bAdds)
}

// isMultisetSubset reports whether every string in needle occurs in haystack
// at least as many times.
func isMultisetSubset(needle, haystack []string) bool {
	if len(needle) == 0 {
		return true
	}
	if len(needle) > len(haystack) {
		return false
	}
	counts := make(map[string]int, len(haystack))
	for _, h := range haystack {
		counts[h]++
	}
	for _, n := range needle {
		counts[n]--
		if counts[n] < 0 {
			return false
		}
	}
	return true
}
