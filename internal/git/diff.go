package git

import "strings"

// Diff runs git diff with the given flags and returns the output.
func (r *Runner) Diff(flags ...string) (string, error) {
	args := append([]string{"diff"}, flags...)
	return r.Run(args...)
}

// DiffFile runs git diff for a specific file.
func (r *Runner) DiffFile(path string, flags ...string) (string, error) {
	args := append([]string{"diff"}, flags...)
	args = append(args, "--", path)
	return r.Run(args...)
}

// IsUntracked checks if a file is untracked (not ignored).
func (r *Runner) IsUntracked(path string) (bool, error) {
	out, err := r.Run("ls-files", "--others", "--exclude-standard", "--", path)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// IntentToAdd runs git add -N for a file.
func (r *Runner) IntentToAdd(path string) error {
	_, err := r.Run("add", "-N", "--", path)
	return err
}

// RevertIntentToAdd reverts a git add -N operation.
func (r *Runner) RevertIntentToAdd(path string) error {
	_, err := r.Run("rm", "--cached", "--", path)
	return err
}
