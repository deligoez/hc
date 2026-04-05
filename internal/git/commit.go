package git

import (
	"fmt"
	"strings"
)

// Commit creates a git commit and returns the short SHA.
func (r *Runner) Commit(message string) (string, error) {
	_, err := r.Run("commit", "-m", message)
	if err != nil {
		return "", err
	}
	out, err := r.Run("rev-parse", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("commit succeeded but could not get SHA: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// Add stages a file.
func (r *Runner) Add(path string) error {
	_, err := r.Run("add", "--", path)
	return err
}

// ResetHead resets the staging area (does not affect commits).
func (r *Runner) ResetHead() error {
	_, err := r.Run("reset", "HEAD", "--")
	return err
}
