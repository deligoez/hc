package diff

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// Fingerprint computes a SHA-256 hex digest of a hunk's semantic content.
// Deletions are written first (prefixed with "-"), then "||", then additions
// (prefixed with "+"). Line order within each group follows the original
// hunk order.
func Fingerprint(hunk Hunk) string {
	var b strings.Builder

	for _, l := range hunk.Lines {
		if l.Op == OpDelete {
			b.WriteString("-")
			b.WriteString(l.Content)
			b.WriteString("\n")
		}
	}

	b.WriteString("||")

	for _, l := range hunk.Lines {
		if l.Op == OpAdd {
			b.WriteString("+")
			b.WriteString(l.Content)
			b.WriteString("\n")
		}
	}

	sum := sha256.Sum256([]byte(b.String()))
	return fmt.Sprintf("%x", sum)
}
