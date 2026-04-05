package git

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// Runner executes git commands. It can be configured with a custom
// environment (e.g., GIT_INDEX_FILE for temp index operations).
type Runner struct {
	Dir string
	Env []string // additional env vars (e.g., "GIT_INDEX_FILE=/tmp/idx")
}

// NewRunner creates a runner for the given directory.
func NewRunner(dir string) *Runner {
	return &Runner{Dir: dir}
}

// Run executes a git command and returns combined stdout.
func (r *Runner) Run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if r.Dir != "" {
		cmd.Dir = r.Dir
	}
	if len(r.Env) > 0 {
		cmd.Env = append(cmd.Environ(), r.Env...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// RunWithStdin executes a git command with stdin data.
func (r *Runner) RunWithStdin(data []byte, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if r.Dir != "" {
		cmd.Dir = r.Dir
	}
	if len(r.Env) > 0 {
		cmd.Env = append(cmd.Environ(), r.Env...)
	}
	cmd.Stdin = bytes.NewReader(data)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// EnsureRepo checks that we are inside a git repository.
func (r *Runner) EnsureRepo() error {
	_, err := r.Run("rev-parse", "--git-dir")
	if err != nil {
		return fmt.Errorf("not a git repository")
	}
	return nil
}

// EnsureCleanStaging checks that the staging area is clean.
func (r *Runner) EnsureCleanStaging() error {
	out, err := r.Run("diff", "--cached", "--quiet")
	_ = out
	if err != nil {
		return fmt.Errorf("staging area is not clean")
	}
	return nil
}
