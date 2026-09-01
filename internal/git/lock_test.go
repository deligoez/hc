package git

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// planIndexLock takes the repository's index lock the way a concurrent git
// process would, and returns its path.
func plantIndexLock(t *testing.T, r *Runner) string {
	t.Helper()
	lock := filepath.Join(r.Dir, ".git", "index.lock")
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(lock) })
	return lock
}

func TestExecRetriesUntilTheLockIsReleased(t *testing.T) {
	r := newTestRepo(t)
	lock := plantIndexLock(t, r)

	if err := os.WriteFile(filepath.Join(r.Dir, "base.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	const held = 150 * time.Millisecond
	released := make(chan struct{})
	go func() {
		time.Sleep(held)
		_ = os.Remove(lock)
		close(released)
	}()

	start := time.Now()
	if err := r.Add("base.txt"); err != nil {
		t.Fatalf("Add should have waited for the lock, got: %v", err)
	}
	<-released

	if elapsed := time.Since(start); elapsed < held {
		t.Fatalf("Add succeeded after %s, before the lock was released -- it cannot have retried", elapsed)
	}
}

func TestExecReportsLockContentionAfterTheBudget(t *testing.T) {
	// git translates the lockfile error, and tr_TR moves the quotes around
	// the path, so a text match written against English would miss it. hc
	// pins LC_ALL=C to keep that from happening; this asserts the pinning
	// holds. Where the locale is not installed git answers in English anyway
	// and the test still passes -- it can only get stronger, never flaky.
	t.Setenv("LC_ALL", "tr_TR.UTF-8")
	t.Setenv("LANGUAGE", "tr")
	t.Setenv("HC_LOCK_TIMEOUT", "100ms")

	r := newTestRepo(t)
	plantIndexLock(t, r)

	_, err := r.Run("update-index", "--force-remove", "--", "base.txt")
	var le *LockError
	if !errors.As(err, &le) {
		t.Fatalf("expected a *LockError, got %T: %v", err, err)
	}
	if le.Attempts < 2 {
		t.Errorf("gave up after %d attempt(s); the budget allows more", le.Attempts)
	}
	if le.Waited <= 0 {
		t.Errorf("reported no wait at all: %s", le.Waited)
	}
	if !strings.Contains(le.Error(), "index.lock") {
		t.Errorf("error should carry git's own message, got: %v", le)
	}
}
