package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/deligoez/hc/internal/git"
	"github.com/deligoez/hc/internal/output"
	"github.com/deligoez/hc/internal/plan"
)

// runStateVersion is the on-disk format of the resume record. A run written by
// a newer hc is refused rather than misread.
const runStateVersion = 1

// runStateName is the resume record's filename inside the git directory.
//
// It goes in the PER-WORKTREE git dir (`rev-parse --git-dir`), never the
// common one: HEAD and the index the record describes are per-worktree, so two
// worktrees mid-plan at once must not overwrite each other's record.
const runStateName = "hc-run-state.json"

// runState is what `hc run --continue` needs to finish a plan that stopped
// part-way.
//
// The plan's hunk indices only mean anything against the diff the plan was
// written from, and by the time a run is interrupted the index has moved on --
// it now holds the commits that succeeded. So the record keeps `Base`, the
// commit HEAD pointed at before the first commit, and a resumed run re-derives
// the original diff from there. That is an exact reproduction, not a re-diff:
// the working tree is untouched by hc, so `git diff <base>` is byte-for-byte
// the diff the plan was written against, and every index in the plan still
// addresses the hunk it always did.
//
// `Head` is what makes a stale record safe. It is the commit hc last created;
// if HEAD has moved since (someone committed by hand, another run happened),
// the record no longer describes reality and continuing is refused.
type runState struct {
	Version int             `json:"version"`
	Base    string          `json:"base"`
	Head    string          `json:"head"`
	Prefix  string          `json:"prefix,omitempty"`
	SHAs    []string        `json:"shas"`
	Plan    json.RawMessage `json:"plan"`

	path string
}

// runStatePath locates the resume record for the worktree runner points at.
func runStatePath(runner *git.Runner) (string, error) {
	out, err := runner.Run("rev-parse", "--git-dir")
	if err != nil {
		return "", err
	}
	gitDir := strings.TrimSpace(out)
	if !filepath.IsAbs(gitDir) && runner.Dir != "" {
		gitDir = filepath.Join(runner.Dir, gitDir)
	}
	return filepath.Join(gitDir, runStateName), nil
}

// startRunState opens a resume record for a run about to enter Phase 2. It
// returns nil when the repository has no HEAD yet: there is no base commit to
// re-derive a diff from, so that one run simply cannot be continued.
//
// A record that cannot be written fails the run before any commit exists. That
// is not pedantry: the file lives beside `index.lock`, so a git directory hc
// cannot write to is one where the next `git commit` was going to fail anyway,
// and failing now costs nothing.
func startRunState(runner *git.Runner, planData []byte, prefix string) (*runState, *output.ACError) {
	base, err := runner.Head()
	if err != nil {
		return nil, nil // unborn HEAD: nothing to resume from
	}
	path, err := runStatePath(runner)
	if err != nil {
		return nil, output.NewExecutionError(fmt.Sprintf("cannot find .git directory: %v", err), "")
	}

	st := &runState{
		Version: runStateVersion,
		Base:    base,
		Head:    base,
		Prefix:  prefix,
		Plan:    json.RawMessage(planData),
		path:    path,
	}
	if err := st.save(); err != nil {
		return nil, output.NewExecutionError(
			fmt.Sprintf("cannot record the run for --continue: %v", err),
			"hc writes it next to the index in the git directory; check that the directory is writable.",
		)
	}
	return st, nil
}

// save rewrites the record. Every method below tolerates a nil receiver, which
// is the "this run cannot be continued" case.
func (s *runState) save() error {
	if s == nil {
		return nil
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, append(data, '\n'), 0o600)
}

// count is how many of the plan's commits already exist.
func (s *runState) count() int {
	if s == nil {
		return 0
	}
	return len(s.SHAs)
}

// record notes one more created commit. A failed write is deliberately not
// fatal: the run is going fine, and a record that falls behind reality is
// caught by the HEAD check on the way back in rather than acted on.
func (s *runState) record(runner *git.Runner, sha string) {
	if s == nil {
		return
	}
	s.SHAs = append(s.SHAs, sha)
	if head, err := runner.Head(); err == nil {
		s.Head = head
	}
	_ = s.save()
}

// clear removes the record once the plan is complete.
func (s *runState) clear() {
	if s == nil {
		return
	}
	_ = os.Remove(s.path)
}

// loadRunState reads the record for `hc run --continue` and refuses every way
// it can have gone stale.
func loadRunState(runner *git.Runner) (*runState, *output.ACError) {
	path, err := runStatePath(runner)
	if err != nil {
		return nil, output.NewExecutionError(fmt.Sprintf("cannot find .git directory: %v", err), "")
	}

	data, err := os.ReadFile(path) //nolint:gosec // path is derived from git rev-parse, not user input
	if err != nil {
		return nil, output.NewValidationError(
			"no interrupted run to continue",
			"hc keeps a resume record only while a plan is executing. Run 'hc diff --json' and write a plan.",
		)
	}

	var st runState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, output.NewValidationError(
			fmt.Sprintf("cannot read the resume record: %v", err),
			fmt.Sprintf("Delete %s and re-plan with 'hc diff --json'.", path),
		)
	}
	st.path = path
	if st.Version != runStateVersion {
		return nil, output.NewValidationError(
			fmt.Sprintf("resume record version %d is not supported", st.Version),
			fmt.Sprintf("It was written by a different hc. Delete %s and re-plan.", path),
		)
	}

	head, err := runner.Head()
	if err != nil || head != st.Head {
		return nil, output.NewValidationError(
			"HEAD has moved since the interrupted run stopped",
			"Something committed after hc did, so the recorded plan no longer describes the remaining work. Run 'hc diff --json' and write a new plan.",
		)
	}

	p, perr := plan.Parse(st.Plan)
	if perr != nil {
		return nil, output.NewValidationError(
			fmt.Sprintf("the recorded plan is unreadable: %v", perr),
			fmt.Sprintf("Delete %s and re-plan with 'hc diff --json'.", path),
		)
	}
	if len(st.SHAs) >= len(p.Commits) {
		st.clear()
		return nil, output.NewValidationError(
			"the recorded run is already complete",
			"Every commit in it was created. There is nothing to continue.",
		)
	}

	return &st, nil
}
