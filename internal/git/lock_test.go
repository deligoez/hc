package git

import (
	"os"
	"path/filepath"
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
