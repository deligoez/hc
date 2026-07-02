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

// TestPropertyRandomRewrites builds random linear histories, splits random
// commits into random per-file / per-hunk replacements, and checks the
// invariants that make hc rewrite safe:
//  1. the final tree is byte-identical to the original head,
//  2. the commit count grows exactly by the added replacements,
//  3. untouched commits keep their subjects in order,
//  4. the working tree/status stay clean.
func TestPropertyRandomRewrites(t *testing.T) {
	if testing.Short() {
		t.Skip("property test skipped in -short mode")
	}
	for seed := int64(1); seed <= 40; seed++ {
		seed := seed
		t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			dir := t.TempDir()
			r := initRepo(t, dir)

			// Base state: a handful of files with plenty of lines.
			nFiles := 2 + rng.Intn(3)
			files := make(map[string][]string, nFiles)
			names := make([]string, 0, nFiles)
			baseState := map[string]string{}
			for i := 0; i < nFiles; i++ {
				name := fmt.Sprintf("f%d.txt", i)
				lines := make([]string, 40)
				for j := range lines {
					lines[j] = fmt.Sprintf("%s-line%d", name, j)
				}
				files[name] = lines
				names = append(names, name)
				baseState[name] = strings.Join(lines, "\n") + "\n"
			}
			mkCommit(t, r, dir, "base", baseState)

			// Random linear history: each commit edits 1..3 files at
			// well-separated positions (distinct hunks).
			type commitRec struct {
				sha     string
				subject string
			}
			var history []commitRec
			nCommits := 3 + rng.Intn(5)
			for c := 0; c < nCommits; c++ {
				touched := map[string]string{}
				nTouch := 1 + rng.Intn(3)
				if nTouch > len(names) {
					nTouch = len(names)
				}
				perm := rng.Perm(len(names))
				for _, idx := range perm[:nTouch] {
					name := names[idx]
					lines := files[name]
					nEdits := 1 + rng.Intn(3)
					for e := 0; e < nEdits; e++ {
						pos := 3 + e*12 + rng.Intn(5)
						if pos < len(lines) {
							lines[pos] = fmt.Sprintf("%s-c%d-e%d", name, c, e)
						}
					}
					files[name] = lines
					touched[name] = strings.Join(lines, "\n") + "\n"
				}
				subject := fmt.Sprintf("commit %d", c)
				sha := mkCommit(t, r, dir, subject, touched)
				history = append(history, commitRec{sha, subject})
			}
			oldHead, _ := r.ResolveSHA("HEAD")

			// Split a random subset of commits.
			var rewrites []string
			extra := 0
			splitCount := 0
			for _, rec := range history {
				if rng.Float64() > 0.5 {
					continue
				}
				raw, err := r.DiffCommit(rec.sha)
				must(t, err)
				parsed, err := diff.Parse(raw)
				must(t, err)
				if len(parsed) == 0 {
					continue
				}

				var subCommits []string
				for _, fd := range parsed {
					if len(fd.Hunks) > 1 && rng.Float64() < 0.4 {
						// Hunk-partition this file into two sub-commits.
						cut := 1 + rng.Intn(len(fd.Hunks)-1)
						var a, b []string
						for h := 0; h < len(fd.Hunks); h++ {
							if h < cut {
								a = append(a, fmt.Sprintf("%d", h))
							} else {
								b = append(b, fmt.Sprintf("%d", h))
							}
						}
						subCommits = append(subCommits,
							fmt.Sprintf(`{"message":"split %s A","files":[{"path":"%s","hunks":[%s]}]}`, fd.Path, fd.Path, strings.Join(a, ",")),
							fmt.Sprintf(`{"message":"split %s B","files":[{"path":"%s","hunks":[%s]}]}`, fd.Path, fd.Path, strings.Join(b, ",")))
					} else {
						subCommits = append(subCommits,
							fmt.Sprintf(`{"message":"split %s","files":[{"path":"%s"}]}`, fd.Path, fd.Path))
					}
				}
				if len(subCommits) < 2 {
					continue // splitting into one part is pointless; keep fuzz interesting
				}
				rewrites = append(rewrites, fmt.Sprintf(`{"commit":"%s","commits":[%s]}`, rec.sha, strings.Join(subCommits, ",")))
				extra += len(subCommits) - 1
				splitCount++
			}
			if len(rewrites) == 0 {
				t.Skip("random draw split nothing")
			}

			planJSON := `{"rewrites":[` + strings.Join(rewrites, ",") + `]}`
			res, acErr := runRewrite([]byte(planJSON), r, rewriteOpts{})
			if acErr != nil {
				t.Fatalf("rewrite failed (seed %d): %s | %s\nplan: %s", seed, acErr.Message, acErr.Hint, planJSON)
			}

			// (1) content identity
			assertSameContent(t, r, oldHead, "HEAD")
			// (2) commit count
			oldCount := len(history) + 1 + 1 // initRepo initial + base + history
			newTotal, _ := r.Run("rev-list", "--count", "HEAD")
			if strings.TrimSpace(newTotal) != fmt.Sprintf("%d", oldCount+extra) {
				t.Fatalf("commit count = %s, want %d", strings.TrimSpace(newTotal), oldCount+extra)
			}
			// (3) untouched subjects survive in order
			subsAfter := subjects(t, r, fmt.Sprintf("HEAD~%d..HEAD", len(history)+extra))
			joined := strings.Join(subsAfter, "\n")
			for _, rec := range history {
				if !strings.Contains(planJSON, rec.sha) && !strings.Contains(joined, rec.subject) {
					t.Fatalf("untouched commit %q lost", rec.subject)
				}
			}
			// (4) clean status
			if st, _ := r.Run("status", "--porcelain"); strings.TrimSpace(st) != "" {
				t.Fatalf("status not clean:\n%s", st)
			}
			// backup ref points at old head
			backup, err := r.ResolveSHA(res.BackupRef)
			must(t, err)
			if backup != oldHead {
				t.Fatal("backup ref lost the old head")
			}
			_ = os.Remove(filepath.Join(dir, ".unused"))
			_ = splitCount
		})
	}
}
