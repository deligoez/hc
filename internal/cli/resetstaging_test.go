package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestResetStagingWarnsOnlyWhenWorkIsStranded guards a case the docs used to
// deny outright. hc unstages what a failed commit had already staged, but that
// reset needs the same index lock the staging call just lost -- so under
// contention it fails too and the staging survives. Measured on a 40-file
// commit interrupted mid-way: three files stayed staged, and that leftover is
// exactly what makes `hc run --continue` refuse with "staging area is not
// clean".
//
// The warning is conditioned on the index actually being dirty, because losing
// the lock BEFORE the first file was staged fails the reset just as loudly
// while leaving nothing to clean up.
func TestResetStagingWarnsOnlyWhenWorkIsStranded(t *testing.T) {
	t.Setenv("HC_LOCK_TIMEOUT", "50ms")

	dir := t.TempDir()
	r := initRepo(t, dir)
	lock := filepath.Join(dir, ".git", "index.lock")

	takeLock := func() {
		t.Helper()
		must(t, os.WriteFile(lock, nil, 0o600))
	}
	dropLock := func() {
		t.Helper()
		must(t, os.Remove(lock))
	}

	if got := resetStaging(r); got != "" {
		t.Fatalf("a reset that worked should add nothing to the hint, got %q", got)
	}

	takeLock()
	if got := resetStaging(r); got != "" {
		t.Errorf("nothing was staged, so a failed reset stranded nothing: %q", got)
	}
	dropLock()

	// The case that actually costs the agent something: staged work plus a
	// lock hc cannot win.
	writeFile(t, dir, "x.txt", "x\n")
	must(t, run(r, "add", "x.txt"))
	takeLock()
	defer dropLock()

	got := resetStaging(r)
	if !strings.Contains(got, "git reset HEAD") {
		t.Errorf("a reset that stranded staged work must say so, got %q", got)
	}
	if !strings.HasPrefix(got, " ") {
		t.Errorf("the text is appended to an existing hint, so it needs a leading space: %q", got)
	}
}
