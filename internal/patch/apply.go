package patch

import (
	"github.com/deligoez/ac/internal/git"
)

// Apply applies a patch to the git index using git apply --cached --unidiff-zero.
func Apply(runner *git.Runner, patch []byte) error {
	_, err := runner.RunWithStdin(patch, "apply", "--cached", "--unidiff-zero")
	return err
}

// ApplyCheck performs a dry-run of applying a patch to the git index.
func ApplyCheck(runner *git.Runner, patch []byte) error {
	_, err := runner.RunWithStdin(patch, "apply", "--cached", "--unidiff-zero", "--check")
	return err
}
