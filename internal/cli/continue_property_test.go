package cli

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/deligoez/hc/internal/diff"
	"github.com/deligoez/hc/internal/git"
	"github.com/deligoez/hc/internal/output"
	"github.com/deligoez/hc/internal/plan"
)

// randomFilePair builds a file and a randomly edited version of it. Edits land
// only on even lines, so no two are adjacent and each one is its own hunk
// under -U0 -- the hunk count is then exactly the number of edits.
func randomFilePair(rng *rand.Rand) (base, edited string) {
	n := 20 + rng.Intn(40)
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("line%d", i)
	}
	base = strings.Join(lines, "\n") + "\n"

	even := rng.Perm(n / 2)
	k := min(2+rng.Intn(5), len(even))
	for _, j := range even[:k] {
		lines[j*2] = fmt.Sprintf("CHANGED%d", j*2)
	}
	return base, strings.Join(lines, "\n") + "\n"
}

// repoWithEdit creates a repository holding base as a commit and edited in the
// working tree.
func repoWithEdit(t *testing.T, dir, base, edited string) *git.Runner {
	t.Helper()
	must(t, os.MkdirAll(dir, 0o755))
	r := initRepo(t, dir)
	writeFile(t, dir, "app.txt", base)
	must(t, run(r, "add", "-A"))
	must(t, run(r, "commit", "-m", "base"))
	writeFile(t, dir, "app.txt", edited)
	return r
}

// randomResumePlan partitions the hunks across at least two commits, out of
// order. Out-of-order matters here: it is what forces a resumed run to rebuild
// each file from base + the hunks earlier commits took + its own, rather than
// from whatever happens to be adjacent.
func randomResumePlan(rng *rand.Rand, nHunks int) (planJSON []byte, nCommits int) {
	nCommits = min(2+rng.Intn(3), nHunks)
	buckets := make([][]int, nCommits)
	for i, h := range rng.Perm(nHunks) {
		buckets[i%nCommits] = append(buckets[i%nCommits], h)
	}

	p := plan.Plan{Commits: make([]plan.Commit, 0, nCommits)}
	for i, b := range buckets {
		slices.Sort(b)
		p.Commits = append(p.Commits, plan.Commit{
			Message: fmt.Sprintf("fuzz: part %d", i),
			Files:   []plan.FileEntry{{Path: "app.txt", Hunks: b}},
		})
	}
	data, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}
	return data, nCommits
}

// lockAfterCommit installs a post-commit hook that takes the index lock once,
// right after commit k -- reproducing the reported interruption, where the
// next commit's staging cannot get the lock.
func lockAfterCommit(t *testing.T, dir, counter string, k int) {
	t.Helper()
	must(t, os.WriteFile(counter, []byte("0"), 0o600))
	script := fmt.Sprintf("#!/bin/sh\n"+
		"n=$(cat %q)\n"+
		"echo $((n+1)) > %q\n"+
		"[ \"$n\" -eq %d ] || exit 0\n"+
		": > \"$(git rev-parse --git-path index.lock)\"\n", counter, counter, k)
	must(t, os.WriteFile(filepath.Join(dir, ".git", "hooks", "post-commit"), []byte(script), 0o755))
}

// planCommitTrees returns "<message>\x00<tree>" for the last n commits, oldest
// first.
func planCommitTrees(t *testing.T, r *git.Runner, n int) []string {
	t.Helper()
	out, err := r.Run("log", fmt.Sprintf("-%d", n), "--format=%s%x00%T", "--reverse")
	must(t, err)
	return strings.Split(strings.TrimSpace(out), "\n")
}

// TestPropertyResumeMatchesStraightRun is the deterministic, in-process port of
// the resume fuzzer. Each seed builds the same repository twice: one runs a
// random plan straight through, the other is interrupted at a random commit by
// a lock it cannot win and then resumed with --continue.
//
// The invariant is compared per COMMIT, not on the final tree. A resumed run
// that mis-mapped a hunk would still land the same final tree while the
// individual commits differed, and mis-mapping is precisely the hazard
// --continue has to avoid: by the time it runs, the index has moved past the
// commits that succeeded, so the plan's indices no longer address the diff the
// index would produce.
func TestPropertyResumeMatchesStraightRun(t *testing.T) {
	// Short enough that an interruption is instant, long enough that a genuine
	// neighbour would still be waited out.
	t.Setenv("HC_LOCK_TIMEOUT", "50ms")

	// Each seed builds two repositories and runs the plan twice, so a seed
	// here costs ~3.5x one of TestPropertyRandomPlans'. Measured under -race:
	// 1.05 s/seed against 0.30. The count is set to keep this test's share of
	// the gate in line with that one's rather than to search exhaustively --
	// deep runs are what the out-of-repo fuzzer is for.
	seeds := int64(20)
	if testing.Short() {
		seeds = 6
	}
	for seed := int64(1); seed <= seeds; seed++ {
		t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed)) //nolint:gosec // reproducible test input, not a secret
			work := t.TempDir()

			base, edited := randomFilePair(rng)
			dirA, dirB := filepath.Join(work, "a"), filepath.Join(work, "b")
			rA := repoWithEdit(t, dirA, base, edited)
			rB := repoWithEdit(t, dirB, base, edited)

			raw, err := rA.Diff("-U0", "--no-renames", "--no-ext-diff")
			must(t, err)
			parsed, err := diff.Parse(raw)
			must(t, err)
			if len(parsed) != 1 || len(parsed[0].Hunks) < 2 {
				t.Skip("need at least two hunks to interrupt between commits")
			}
			planJSON, nCommits := randomResumePlan(rng, len(parsed[0].Hunks))

			if _, acErr := runPlan(planJSON, rA, false); acErr != nil {
				t.Fatalf("straight run failed: %s | %s", acErr.Message, acErr.Hint)
			}
			want := planCommitTrees(t, rA, nCommits)

			stopAfter := rng.Intn(nCommits - 1)
			lockAfterCommit(t, dirB, filepath.Join(work, "counter"), stopAfter)

			res, acErr := runPlan(planJSON, rB, false)
			if acErr == nil || acErr.Code != 3 {
				t.Fatalf("expected the run to stop with exit 3, got %v", acErr)
			}
			if got := res.(*output.Result).Committed; got != stopAfter+1 {
				t.Fatalf("stopped after %d commits, want %d", got, stopAfter+1)
			}

			must(t, os.Remove(filepath.Join(dirB, ".git", "hooks", "post-commit")))
			_ = os.Remove(filepath.Join(dirB, ".git", "index.lock"))

			st, acErr := loadRunState(rB)
			if acErr != nil {
				t.Fatalf("no resume record after the stop: %s", acErr.Message)
			}
			resumed, acErr := runPlanFrom(st.Plan, rB, false, st.Prefix, st)
			if acErr != nil {
				t.Fatalf("--continue failed: %s | %s", acErr.Message, acErr.Hint)
			}
			if r := resumed.(*output.Result); r.Committed != nCommits {
				t.Fatalf("resumed run committed %d of %d", r.Committed, nCommits)
			}

			if got := planCommitTrees(t, rB, nCommits); !slices.Equal(got, want) {
				t.Fatalf("resuming after commit %d produced a different history\n straight: %q\n resumed:  %q",
					stopAfter, want, got)
			}
			if s, _ := rB.Run("status", "--porcelain"); strings.TrimSpace(s) != "" {
				t.Fatalf("tree not clean after resuming:\n%s", s)
			}
			if _, err := os.Stat(filepath.Join(dirB, ".git", "hc-run-state.json")); !os.IsNotExist(err) {
				t.Error("the resume record survived a completed plan")
			}
		})
	}
}
