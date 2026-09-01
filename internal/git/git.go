package git

import (
	"bytes"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"time"
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
	return r.exec(nil, args...)
}

// RunWithStdin executes a git command with stdin data.
func (r *Runner) RunWithStdin(data []byte, args ...string) (string, error) {
	if data == nil {
		// nil still means "feed git an empty stdin", not "no stdin": an
		// empty reconstructed file is a legitimate blob to hash.
		data = []byte{}
	}
	return r.exec(data, args...)
}

// exec runs one git command, retrying for as long as the only thing wrong is
// another process holding a repository lock (see lock.go).
func (r *Runner) exec(stdin []byte, args ...string) (string, error) {
	started := time.Now()
	deadline := started.Add(lockTimeout())
	delay := lockRetryInitialDelay

	for attempt := 1; ; attempt++ {
		stdout, stderr, err := r.start(stdin, args...)
		if err == nil {
			return stdout, nil
		}
		if !isLockContention(stderr) {
			return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), stderr)
		}

		wait := jitter(delay)
		if time.Now().Add(wait).After(deadline) {
			return "", &LockError{
				Args:     slices.Clone(args),
				Stderr:   stderr,
				Attempts: attempt,
				Waited:   time.Since(started),
			}
		}
		time.Sleep(wait)
		delay = min(2*delay, lockRetryMaxDelay)
	}
}

// start runs the command once and returns stdout and stderr separately, so
// exec can classify the failure before deciding whether to repeat it.
func (r *Runner) start(stdin []byte, args ...string) (string, string, error) {
	cmd := exec.Command("git", args...)
	if r.Dir != "" {
		cmd.Dir = r.Dir
	}
	// r.Env goes last: os/exec keeps the last occurrence of a duplicate key.
	cmd.Env = append(append(cmd.Environ(), gitMessageEnv...), r.Env...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), strings.TrimSpace(stderr.String()), err
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
