package git

import (
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"time"
)

// Repository locks are held by whichever git process got there first, and in
// an agent environment hc is never the only git process in the repository: an
// editor's status poll, another agent's `git add`, a background indexer. Every
// index-mutating command hc runs (`update-index`, `add`, `add -N`, `commit`,
// `reset`, `rm --cached`) dies with the same fatal error when it loses that
// race, and mid-plan that costs the whole remaining plan.
//
// Retrying is safe precisely because the error says the lock could not be
// CREATED: git had not touched the index when it gave up, so the command is a
// no-op to repeat. That is why the retry is gated on this error and no other.
const (
	defaultLockTimeout    = 2 * time.Second
	lockRetryInitialDelay = 20 * time.Millisecond
	lockRetryMaxDelay     = 400 * time.Millisecond
)

// gitMessageEnv pins git's own messages to English.
//
// The lockfile error is translated, and the translation does not merely change
// the words: measured under tr_TR.UTF-8, git 2.55 renders it
// `"…/.git/index.lock" oluşturulamıyor: File exists.` -- double quotes around
// the path where the English text uses single ones. A localized user would
// therefore fall through isLockContention and lose the retry silently, which
// is the worst shape a locale bug can take. LC_ALL=C also defeats LANGUAGE,
// which otherwise overrides LC_MESSAGES.
var gitMessageEnv = []string{"LC_ALL=C"}

// LockError reports a git command that kept losing the race for a repository
// lock until hc's retry budget ran out. Its message is identical to any other
// git failure's; the type is what lets callers attach a recovery hint that
// names the real cause instead of blaming the plan.
type LockError struct {
	Args     []string
	Stderr   string
	Attempts int
	Waited   time.Duration
}

func (e *LockError) Error() string {
	return fmt.Sprintf("git %s: %s", strings.Join(e.Args, " "), e.Stderr)
}

// isLockContention matches git's lockfile.c failure ("Unable to create
// '%s.lock': %s", followed by the "Another git process" advice). Both spellings
// are checked because git emits the advice only for well-known locks.
func isLockContention(stderr string) bool {
	return strings.Contains(stderr, ".lock': File exists") ||
		strings.Contains(stderr, "Another git process seems to be running in this repository")
}

// lockTimeout is the total budget for retrying one command. HC_LOCK_TIMEOUT
// (any time.ParseDuration value) raises it for repositories with a slow
// neighbour, or disables retrying with 0. It is read per command rather than
// once at init so tests -- and a wrapper script -- can set it.
func lockTimeout() time.Duration {
	v, ok := os.LookupEnv("HC_LOCK_TIMEOUT")
	if !ok {
		return defaultLockTimeout
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return defaultLockTimeout
	}
	return d
}

// jitter spreads retries over [d/2, 3d/2) so that two hc processes that
// collided once do not keep colliding in lockstep.
func jitter(d time.Duration) time.Duration {
	return d/2 + rand.N(d) //nolint:gosec // scheduling jitter, not a secret
}
