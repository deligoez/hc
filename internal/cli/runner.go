package cli

import (
	"strings"

	"github.com/deligoez/hc/internal/git"
	"github.com/deligoez/hc/internal/output"
)

// newRepoRunner creates a git runner rooted at the repository toplevel, so
// hc works from any subdirectory: plan paths and git diff output are always
// relative to the repo root, and pathspecs (git add, git apply) must resolve
// against it rather than the current working directory.
func newRepoRunner() (*git.Runner, *output.ACError) {
	runner := git.NewRunner(".")
	top, err := runner.Run("rev-parse", "--show-toplevel")
	if err != nil {
		return nil, output.NewValidationError(
			"not a git repository",
			"Run hc from inside a git repository.",
		)
	}
	if dir := strings.TrimSpace(top); dir != "" {
		runner.Dir = dir
	}
	return runner, nil
}
