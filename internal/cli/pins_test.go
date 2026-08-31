package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Every tool in the CI gate is pinned to an exact version, and a pin is a
// decision that rots: the version that was right on the day stops being right
// without anything in this repo changing. Writing the removal condition in a
// comment is not enough. On 2026-08-31 ci.yml said "raise the pin once a
// golangci-lint release ships a staticcheck that parses 1.27" while that
// release was already twelve days old, and nothing noticed -- a comment cannot
// check itself. This test can, so the review date is a build artifact rather
// than a good intention.
//
// It fails in two directions, both deliberate:
//   - the date passes, so the pins get looked at on a schedule
//   - a workflow that pins a tool stops carrying a date, so the mechanism
//     cannot be quietly deleted along with the comment that explains it

// pinnedInstall matches a gate tool pinned to an exact version, e.g.
// `go install golang.org/x/tools/cmd/deadcode@v0.49.0`. An `@latest` install is
// not a pin and is caught by review, not by this pattern.
var pinnedInstall = regexp.MustCompile(`go install \S+@v\d[^\s"']*`)

var reviewBy = regexp.MustCompile(`Review by (\d{4}-\d{2}-\d{2})`)

// maxReviewHorizon keeps the date from being pushed so far out that it stops
// being a review and becomes a way to silence this test.
const maxReviewHorizon = 18 * 30 * 24 * time.Hour

func TestGatePinsCarryALiveReviewDate(t *testing.T) {
	dir := filepath.Join(repoRoot(t), ".github", "workflows")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	checked := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yml") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		pins := pinnedInstall.FindAllString(string(body), -1)
		if len(pins) == 0 {
			continue // nothing in this workflow can rot
		}
		checked++

		m := reviewBy.FindSubmatch(body)
		if m == nil {
			t.Errorf("%s pins %d tool(s) but carries no `Review by YYYY-MM-DD`:\n  %s\n"+
				"A pin without a review date is a decision nobody will revisit. Add one.",
				e.Name(), len(pins), strings.Join(pins, "\n  "))
			continue
		}

		due, err := time.Parse("2006-01-02", string(m[1]))
		if err != nil {
			t.Errorf("%s: unparseable review date %q: %v", e.Name(), m[1], err)
			continue
		}
		switch {
		case time.Now().After(due):
			t.Errorf("%s: pins are due for review (was %s). Check for newer releases of:\n  %s\n"+
				"Then bump the versions, or keep them and move the date -- but decide, do not just move it.",
				e.Name(), m[1], strings.Join(pins, "\n  "))
		case time.Until(due) > maxReviewHorizon:
			t.Errorf("%s: review date %s is too far out to be a review. Keep it within ~18 months.",
				e.Name(), m[1])
		}
	}

	if checked == 0 {
		t.Fatal("no workflow with a pinned tool install was found -- either the gate " +
			"stopped pinning its tools, or this test stopped being able to see them")
	}
}

// repoRoot walks up from the test's working directory to the module root, so
// this test does not depend on which package it lives in.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod found above the working directory")
		}
		dir = parent
	}
}
