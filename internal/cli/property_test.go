package cli

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deligoez/hc/internal/diff"
)

// TestPropertyRandomPlans is a deterministic, in-process port of the QA
// fuzzer: random repetitive-content files, random edits, random hunk->commit
// partitions. Invariants: the plan validates and executes, the tree ends
// clean, the final file content is byte-identical to the working tree, and
// the commit count matches the plan.
//
// Small vocabularies force duplicate lines, which is exactly the regime where
// diff decompositions are ambiguous (git may re-split or slide hunk windows).
// The reconstruction staging design must be immune by construction.
func TestPropertyRandomPlans(t *testing.T) {
	for seed := int64(1); seed <= 60; seed++ {
		seed := seed
		t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			dir := t.TempDir()
			r := initRepo(t, dir)

			var vocab []string
			if seed%2 == 0 {
				for i := 0; i < 3+rng.Intn(4); i++ {
					vocab = append(vocab, fmt.Sprintf("tok%d", i))
				}
			} else {
				for i := 0; i < 200; i++ {
					vocab = append(vocab, fmt.Sprintf("line-%d-%d", i, rng.Intn(1000000)))
				}
			}

			genLines := func(n int) []string {
				out := make([]string, n)
				for i := range out {
					out[i] = vocab[rng.Intn(len(vocab))]
				}
				return out
			}

			base := genLines(20 + rng.Intn(80))
			trailingNL := rng.Float64() < 0.9
			write := func(lines []string) string {
				content := strings.Join(lines, "\n")
				if len(lines) > 0 && trailingNL {
					content += "\n"
				}
				must(t, os.WriteFile(filepath.Join(dir, "f.txt"), []byte(content), 0o644))
				return content
			}
			write(base)
			must(t, run(r, "add", "-A"))
			must(t, run(r, "commit", "-qm", "base"))

			// Random mutation ops.
			lines := append([]string(nil), base...)
			for i, n := 0, 1+rng.Intn(8); i < n; i++ {
				if len(lines) == 0 {
					lines = genLines(3)
					continue
				}
				pos := rng.Intn(len(lines))
				runLen := 1 + rng.Intn(3)
				switch rng.Intn(3) {
				case 0: // replace
					for j := pos; j < pos+runLen && j < len(lines); j++ {
						lines[j] = vocab[rng.Intn(len(vocab))]
					}
				case 1: // delete
					end := pos + runLen
					if end > len(lines) {
						end = len(lines)
					}
					lines = append(lines[:pos], lines[end:]...)
				default: // insert
					ins := genLines(runLen)
					lines = append(lines[:pos], append(ins, lines[pos:]...)...)
				}
			}
			content := write(lines)

			// Read hunks via the same pipeline hc uses.
			raw, err := r.Diff("-U0", "--no-renames", "--no-ext-diff")
			must(t, err)
			parsed, err := diff.Parse(raw)
			must(t, err)
			if len(parsed) == 0 {
				t.Skip("mutation was a no-op")
			}
			nHunks := len(parsed[0].Hunks)

			// Random partition into 1..4 commits, random order.
			idx := rng.Perm(nHunks)
			nCommits := 1 + rng.Intn(4)
			if nCommits > nHunks {
				nCommits = nHunks
			}
			buckets := make([][]int, nCommits)
			for i, h := range idx {
				buckets[i%nCommits] = append(buckets[i%nCommits], h)
			}
			rng.Shuffle(len(buckets), func(i, j int) { buckets[i], buckets[j] = buckets[j], buckets[i] })

			var commits []string
			for i, b := range buckets {
				var hs []string
				for _, h := range b {
					hs = append(hs, fmt.Sprintf("%d", h))
				}
				commits = append(commits, fmt.Sprintf(
					`{"message":"fuzz %d","files":[{"path":"f.txt","hunks":[%s]}]}`, i, strings.Join(hs, ",")))
			}
			planJSON := `{"commits":[` + strings.Join(commits, ",") + `]}`

			_, acErr := runPlan([]byte(planJSON), r, false)
			if acErr != nil {
				t.Fatalf("plan failed (seed %d): %s | hint: %s\nplan: %s", seed, acErr.Message, acErr.Hint, planJSON)
			}

			if out, _ := r.Run("status", "--porcelain"); strings.TrimSpace(out) != "" {
				t.Fatalf("tree not clean:\n%s", out)
			}
			got, err := os.ReadFile(filepath.Join(dir, "f.txt"))
			must(t, err)
			if string(got) != content {
				t.Fatal("final content differs from working tree at plan time")
			}
			count, _ := r.Run("rev-list", "--count", "HEAD")
			want := fmt.Sprintf("%d", 2+nCommits) // initial + base + plan commits
			if strings.TrimSpace(count) != want {
				t.Fatalf("commit count = %s, want %s", strings.TrimSpace(count), want)
			}
		})
	}
}
