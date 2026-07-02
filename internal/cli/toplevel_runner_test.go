package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// TestNewRepoRunnerResolvesToplevel guards subdirectory support: plan paths
// are repo-root-relative, so the runner must execute git from the toplevel
// regardless of the current working directory.
func TestNewRepoRunnerResolvesToplevel(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	sub := filepath.Join(dir, "sub")
	must(t, os.MkdirAll(sub, 0o755))

	oldWD, err := os.Getwd()
	must(t, err)
	must(t, os.Chdir(sub))
	defer os.Chdir(oldWD)

	runner, acErr := newRepoRunner()
	if acErr != nil {
		t.Fatalf("newRepoRunner in subdir: %v", acErr)
	}
	got, err := filepath.EvalSymlinks(runner.Dir)
	must(t, err)
	want, err := filepath.EvalSymlinks(dir)
	must(t, err)
	if got != want {
		t.Errorf("runner.Dir = %q, want repo toplevel %q", got, want)
	}
}
