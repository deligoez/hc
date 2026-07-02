package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deligoez/hc/internal/git"
)

// mkCommit writes files and commits them, returning the commit SHA.
func mkCommit(t *testing.T, r *git.Runner, dir, msg string, files map[string]string) string {
	t.Helper()
	for path, content := range files {
		full := filepath.Join(dir, path)
		must(t, os.MkdirAll(filepath.Dir(full), 0o755))
		must(t, os.WriteFile(full, []byte(content), 0o644))
	}
	must(t, run(r, "add", "-A"))
	must(t, run(r, "commit", "-qm", msg))
	sha, err := r.ResolveSHA("HEAD")
	must(t, err)
	return sha
}

// assertSameContent asserts two revs have byte-identical trees.
func assertSameContent(t *testing.T, r *git.Runner, a, b string) {
	t.Helper()
	out, err := r.Run("diff", a, b)
	must(t, err)
	if strings.TrimSpace(out) != "" {
		t.Fatalf("trees differ between %s and %s:\n%s", a, b, out)
	}
}

func subjects(t *testing.T, r *git.Runner, rangeArg string) []string {
	t.Helper()
	out, err := r.Run("log", "--reverse", "--format=%s", rangeArg)
	must(t, err)
	return strings.Split(strings.TrimSpace(out), "\n")
}

func TestRewriteSplitHeadCommitPerFile(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)
	mkCommit(t, r, dir, "base", map[string]string{"a.txt": "a\n", "b.txt": "b\n"})
	big := mkCommit(t, r, dir, "big: touch both files", map[string]string{"a.txt": "a2\n", "b.txt": "b2\n"})
	oldHead, _ := r.ResolveSHA("HEAD")

	planJSON := fmt.Sprintf(`{"rewrites":[{"commit":"%s","commits":[
		{"message":"feat: update a","files":[{"path":"a.txt"}]},
		{"message":"feat: update b","files":[{"path":"b.txt"}]}]}]}`, big)

	res, acErr := runRewrite([]byte(planJSON), r, rewriteOpts{})
	if acErr != nil {
		t.Fatalf("rewrite failed: %v %s", acErr, acErr.Hint)
	}

	assertSameContent(t, r, oldHead, "HEAD")
	subs := subjects(t, r, "HEAD~2..HEAD")
	if subs[0] != "feat: update a" || subs[1] != "feat: update b" {
		t.Fatalf("unexpected subjects: %v", subs)
	}
	// Backup preserves the old head.
	backup, err := r.ResolveSHA(res.BackupRef)
	must(t, err)
	if backup != oldHead {
		t.Fatalf("backup ref = %s, want %s", backup, oldHead)
	}
	// Intermediate state: after the first replacement only a.txt changed.
	out, _ := r.Run("show", "--stat", "--format=", "HEAD~1")
	if !strings.Contains(out, "a.txt") || strings.Contains(out, "b.txt") {
		t.Fatalf("first replacement should touch only a.txt:\n%s", out)
	}
}

func TestRewriteSplitMiddleCommitPreservesDownstream(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)
	mkCommit(t, r, dir, "base", map[string]string{"a.txt": "a\n", "b.txt": "b\n"})
	mid := mkCommit(t, r, dir, "big: middle", map[string]string{"a.txt": "a2\n", "b.txt": "b2\n"})
	mkCommit(t, r, dir, "later: untouched work", map[string]string{"c.txt": "c\n"})
	oldHead, _ := r.ResolveSHA("HEAD")

	planJSON := fmt.Sprintf(`{"rewrites":[{"commit":"%s","commits":[
		{"message":"part 1","files":[{"path":"a.txt"}]},
		{"message":"part 2","files":[{"path":"b.txt"}]}]}]}`, mid)

	_, acErr := runRewrite([]byte(planJSON), r, rewriteOpts{})
	if acErr != nil {
		t.Fatalf("rewrite failed: %v", acErr)
	}

	assertSameContent(t, r, oldHead, "HEAD")
	subs := subjects(t, r, "HEAD~3..HEAD")
	want := []string{"part 1", "part 2", "later: untouched work"}
	for i, w := range want {
		if subs[i] != w {
			t.Fatalf("subjects = %v, want %v", subs, want)
		}
	}
}

func TestRewriteHunkLevelSplitWithinFile(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)
	base := make([]string, 30)
	for i := range base {
		base[i] = fmt.Sprintf("line%d", i)
	}
	mkCommit(t, r, dir, "base", map[string]string{"f.txt": strings.Join(base, "\n") + "\n"})
	mut := append([]string(nil), base...)
	mut[5] = "EDIT-A"
	mut[20] = "EDIT-B"
	big := mkCommit(t, r, dir, "both edits", map[string]string{"f.txt": strings.Join(mut, "\n") + "\n"})
	oldHead, _ := r.ResolveSHA("HEAD")

	planJSON := fmt.Sprintf(`{"rewrites":[{"commit":"%s","commits":[
		{"message":"edit A","files":[{"path":"f.txt","hunks":[0]}]},
		{"message":"edit B","files":[{"path":"f.txt","hunks":[1]}]}]}]}`, big)

	_, acErr := runRewrite([]byte(planJSON), r, rewriteOpts{})
	if acErr != nil {
		t.Fatalf("rewrite failed: %v", acErr)
	}
	assertSameContent(t, r, oldHead, "HEAD")

	first, _ := r.Run("show", "HEAD~1", "--format=", "--unified=0")
	if !strings.Contains(first, "+EDIT-A") || strings.Contains(first, "EDIT-B") {
		t.Fatalf("first replacement should contain only EDIT-A:\n%s", first)
	}
}

func TestRewriteNewAndDeletedFiles(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)
	mkCommit(t, r, dir, "base", map[string]string{"old.txt": "old\n", "keep.txt": "keep\n"})
	// One commit that deletes old.txt AND creates new.txt.
	must(t, os.Remove(filepath.Join(dir, "old.txt")))
	must(t, os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0o644))
	must(t, run(r, "add", "-A"))
	must(t, run(r, "commit", "-qm", "swap files"))
	big, _ := r.ResolveSHA("HEAD")
	oldHead := big

	planJSON := fmt.Sprintf(`{"rewrites":[{"commit":"%s","commits":[
		{"message":"chore: drop old.txt","files":[{"path":"old.txt"}]},
		{"message":"feat: add new.txt","files":[{"path":"new.txt"}]}]}]}`, big)

	_, acErr := runRewrite([]byte(planJSON), r, rewriteOpts{})
	if acErr != nil {
		t.Fatalf("rewrite failed: %v", acErr)
	}
	assertSameContent(t, r, oldHead, "HEAD")

	stat, _ := r.Run("show", "--stat", "--format=", "HEAD~1")
	if !strings.Contains(stat, "old.txt") || strings.Contains(stat, "new.txt") {
		t.Fatalf("first replacement should only delete old.txt:\n%s", stat)
	}
}

func TestRewriteCoverageViolationLeavesBranchUntouched(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)
	mkCommit(t, r, dir, "base", map[string]string{"a.txt": "a\n", "b.txt": "b\n"})
	big := mkCommit(t, r, dir, "big", map[string]string{"a.txt": "a2\n", "b.txt": "b2\n"})
	oldHead, _ := r.ResolveSHA("HEAD")

	// b.txt's change is not covered.
	planJSON := fmt.Sprintf(`{"rewrites":[{"commit":"%s","commits":[
		{"message":"only a","files":[{"path":"a.txt"}]}]}]}`, big)

	_, acErr := runRewrite([]byte(planJSON), r, rewriteOpts{})
	if acErr == nil || acErr.Code != 2 {
		t.Fatalf("want validation error, got %v", acErr)
	}
	head, _ := r.ResolveSHA("HEAD")
	if head != oldHead {
		t.Fatal("branch must not move on validation failure")
	}
	if _, err := r.Run("rev-parse", "--verify", "refs/hc/backup/"+currentBranchName(t, r)); err == nil {
		t.Fatal("no backup ref should exist after a failed rewrite")
	}
}

func currentBranchName(t *testing.T, r *git.Runner) string {
	t.Helper()
	b, err := r.CurrentBranch()
	must(t, err)
	return b
}

func TestRewriteRejectsMergeAndForeignCommits(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)
	mkCommit(t, r, dir, "base", map[string]string{"a.txt": "a\n"})
	mkCommit(t, r, dir, "on main", map[string]string{"a.txt": "a2\n"})

	// Build a merge above a side branch.
	must(t, run(r, "checkout", "-qb", "side", "HEAD~1"))
	sideSHA := mkCommit(t, r, dir, "side work", map[string]string{"s.txt": "s\n"})
	branch := currentBranchName(t, r)
	_ = branch
	must(t, run(r, "checkout", "-q", "-"))
	must(t, run(r, "merge", "-q", "--no-ff", "-m", "merge side", "side"))
	target := mkCommit(t, r, dir, "after merge", map[string]string{"a.txt": "a3\n", "b.txt": "b\n"})
	mergeSHA, _ := r.ResolveSHA("HEAD~1")

	// Splitting the merge itself is rejected.
	planJSON := fmt.Sprintf(`{"rewrites":[{"commit":"%s","commits":[{"message":"x","files":[{"path":"a.txt"}]}]}]}`, mergeSHA)
	_, acErr := runRewrite([]byte(planJSON), r, rewriteOpts{})
	if acErr == nil || !strings.Contains(acErr.Message, "merge") {
		t.Fatalf("merge commit should be rejected, got %v", acErr)
	}

	// A commit only reachable through the side branch (not first-parent) is rejected.
	planJSON = fmt.Sprintf(`{"rewrites":[{"commit":"%s","commits":[{"message":"x","files":[{"path":"s.txt"}]}]}]}`, sideSHA)
	_, acErr = runRewrite([]byte(planJSON), r, rewriteOpts{})
	if acErr == nil || !strings.Contains(acErr.Message, "first-parent") {
		t.Fatalf("foreign commit should be rejected, got %v", acErr)
	}

	// Splitting the commit above the merge still works (merge stays intact below).
	planJSON = fmt.Sprintf(`{"rewrites":[{"commit":"%s","commits":[
		{"message":"a3","files":[{"path":"a.txt"}]},
		{"message":"b","files":[{"path":"b.txt"}]}]}]}`, target)
	oldHead, _ := r.ResolveSHA("HEAD")
	_, acErr = runRewrite([]byte(planJSON), r, rewriteOpts{})
	if acErr != nil {
		t.Fatalf("split above merge failed: %v", acErr)
	}
	assertSameContent(t, r, oldHead, "HEAD")
}

func TestRewriteRootCommitRejected(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)
	root, err := r.Run("rev-list", "--max-parents=0", "HEAD")
	must(t, err)
	planJSON := fmt.Sprintf(`{"rewrites":[{"commit":"%s","commits":[{"message":"x","files":[{"path":"y"}]}]}]}`, strings.TrimSpace(root))
	_, acErr := runRewrite([]byte(planJSON), r, rewriteOpts{})
	if acErr == nil || !strings.Contains(acErr.Message, "root commit") {
		t.Fatalf("root commit should be rejected, got %v", acErr)
	}
}

func TestRewritePushedGuardAndForce(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)
	mkCommit(t, r, dir, "base", map[string]string{"a.txt": "a\n", "b.txt": "b\n"})
	big := mkCommit(t, r, dir, "big", map[string]string{"a.txt": "a2\n", "b.txt": "b2\n"})

	// Simulate a push: a remote clone containing the commit.
	remoteDir := t.TempDir()
	remote := git.NewRunner(remoteDir)
	must(t, run(remote, "init", "-q", "--bare"))
	must(t, run(r, "remote", "add", "origin", remoteDir))
	must(t, run(r, "push", "-q", "origin", "HEAD"))
	must(t, run(r, "fetch", "-q", "origin"))

	planJSON := fmt.Sprintf(`{"rewrites":[{"commit":"%s","commits":[
		{"message":"a","files":[{"path":"a.txt"}]},
		{"message":"b","files":[{"path":"b.txt"}]}]}]}`, big)

	_, acErr := runRewrite([]byte(planJSON), r, rewriteOpts{})
	if acErr == nil || !strings.Contains(acErr.Message, "remote") {
		t.Fatalf("pushed commit should require --force, got %v", acErr)
	}

	oldHead, _ := r.ResolveSHA("HEAD")
	_, acErr = runRewrite([]byte(planJSON), r, rewriteOpts{force: true})
	if acErr != nil {
		t.Fatalf("--force rewrite failed: %v", acErr)
	}
	assertSameContent(t, r, oldHead, "HEAD")
}

func TestRewriteDryRunMovesNothing(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)
	mkCommit(t, r, dir, "base", map[string]string{"a.txt": "a\n", "b.txt": "b\n"})
	big := mkCommit(t, r, dir, "big", map[string]string{"a.txt": "a2\n", "b.txt": "b2\n"})
	oldHead, _ := r.ResolveSHA("HEAD")

	planJSON := fmt.Sprintf(`{"rewrites":[{"commit":"%s","commits":[
		{"message":"a","files":[{"path":"a.txt"}]},
		{"message":"b","files":[{"path":"b.txt"}]}]}]}`, big)

	res, acErr := runRewrite([]byte(planJSON), r, rewriteOpts{dryRun: true})
	if acErr != nil {
		t.Fatalf("dry-run failed: %v", acErr)
	}
	if !res.DryRun || len(res.Rewrites) != 1 || len(res.Rewrites[0].Replacements) != 2 {
		t.Fatalf("dry-run result incomplete: %+v", res)
	}
	head, _ := r.ResolveSHA("HEAD")
	if head != oldHead {
		t.Fatal("dry-run must not move the branch")
	}
	if res.BackupRef != "" {
		t.Fatal("dry-run must not create a backup ref")
	}
}

func TestRewritePreservesAuthorAndWorkingTree(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)
	mkCommit(t, r, dir, "base", map[string]string{"a.txt": "a\n", "b.txt": "b\n"})

	// Commit with a distinct author identity/date.
	must(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a2\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b2\n"), 0o644))
	must(t, run(r, "add", "-A"))
	_, err := r.Run("-c", "user.name=Original Author", "-c", "user.email=orig@x.y",
		"commit", "-q", "--date=2024-01-02T03:04:05+00:00", "-m", "big by author")
	must(t, err)
	big, _ := r.ResolveSHA("HEAD")

	// Dirty working tree file (uncommitted) must survive untouched.
	must(t, os.WriteFile(filepath.Join(dir, "wip.txt"), []byte("uncommitted\n"), 0o644))

	planJSON := fmt.Sprintf(`{"rewrites":[{"commit":"%s","commits":[
		{"message":"a part","files":[{"path":"a.txt"}]},
		{"message":"b part","files":[{"path":"b.txt"}]}]}]}`, big)
	_, acErr := runRewrite([]byte(planJSON), r, rewriteOpts{})
	if acErr != nil {
		t.Fatalf("rewrite failed: %v", acErr)
	}

	meta, _ := r.Run("log", "-2", "--format=%an|%ae|%aI")
	for _, line := range strings.Split(strings.TrimSpace(meta), "\n") {
		if !strings.HasPrefix(line, "Original Author|orig@x.y|2024-01-02T03:04:05") {
			t.Fatalf("author identity/date not preserved: %q", line)
		}
	}

	wip, err := os.ReadFile(filepath.Join(dir, "wip.txt"))
	must(t, err)
	if string(wip) != "uncommitted\n" {
		t.Fatal("working tree file was touched")
	}
	st, _ := r.Run("status", "--porcelain")
	if !strings.Contains(st, "wip.txt") || strings.Count(strings.TrimSpace(st), "\n") != 0 {
		t.Fatalf("unexpected status after rewrite:\n%s", st)
	}
}

func TestRewriteMultipleCommitsInOnePlan(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)
	mkCommit(t, r, dir, "base", map[string]string{"a.txt": "a\n", "b.txt": "b\n", "c.txt": "c\n"})
	first := mkCommit(t, r, dir, "first big", map[string]string{"a.txt": "a2\n", "b.txt": "b2\n"})
	mkCommit(t, r, dir, "middle untouched", map[string]string{"c.txt": "c2\n"})
	second := mkCommit(t, r, dir, "second big", map[string]string{"a.txt": "a3\n", "b.txt": "b3\n"})
	oldHead, _ := r.ResolveSHA("HEAD")

	planJSON := fmt.Sprintf(`{"rewrites":[
		{"commit":"%s","commits":[
			{"message":"1a","files":[{"path":"a.txt"}]},
			{"message":"1b","files":[{"path":"b.txt"}]}]},
		{"commit":"%s","commits":[
			{"message":"2a","files":[{"path":"a.txt"}]},
			{"message":"2b","files":[{"path":"b.txt"}]}]}]}`, first, second)

	_, acErr := runRewrite([]byte(planJSON), r, rewriteOpts{})
	if acErr != nil {
		t.Fatalf("rewrite failed: %v", acErr)
	}
	assertSameContent(t, r, oldHead, "HEAD")
	subs := subjects(t, r, "HEAD~5..HEAD")
	want := []string{"1a", "1b", "middle untouched", "2a", "2b"}
	for i, w := range want {
		if subs[i] != w {
			t.Fatalf("subjects = %v, want %v", subs, want)
		}
	}
}

func TestLogListsCommitsWithHunks(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)
	baseSHA := mkCommit(t, r, dir, "base", map[string]string{"a.txt": "one\ntwo\nthree\n"})
	mkCommit(t, r, dir, "edit a", map[string]string{"a.txt": "one\nTWO\nthree\n"})
	mkCommit(t, r, dir, "add b", map[string]string{"b.txt": "b\n"})

	res, acErr := runLog(r, baseSHA+"..HEAD", false)
	if acErr != nil {
		t.Fatalf("runLog failed: %v", acErr)
	}
	if len(res.Commits) != 2 {
		t.Fatalf("commits = %d, want 2", len(res.Commits))
	}
	if res.Commits[0].Subject != "edit a" || res.Commits[1].Subject != "add b" {
		t.Fatalf("subjects wrong: %+v", res.Commits)
	}
	h := res.Commits[0].Files[0].Hunks[0]
	if !strings.Contains(h.Content, "-two") || !strings.Contains(h.Content, "+TWO") {
		t.Fatalf("hunk content missing: %q", h.Content)
	}
	if !res.Commits[1].Files[0].IsNew {
		t.Fatal("added file should be marked is_new")
	}
}

func TestRewritePreservesMidRangeMerge(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)
	mkCommit(t, r, dir, "base", map[string]string{"a.txt": "a\n", "b.txt": "b\n"})
	big := mkCommit(t, r, dir, "big below merge", map[string]string{"a.txt": "a2\n", "b.txt": "b2\n"})

	// Side branch + merge ABOVE the split target.
	must(t, run(r, "checkout", "-qb", "side", "HEAD~1"))
	mkCommit(t, r, dir, "side work", map[string]string{"s.txt": "s\n"})
	must(t, run(r, "checkout", "-q", "-"))
	must(t, run(r, "merge", "-q", "--no-ff", "-m", "merge side", "side"))
	mkCommit(t, r, dir, "after merge", map[string]string{"a.txt": "a3\n"})
	oldHead, _ := r.ResolveSHA("HEAD")

	planJSON := fmt.Sprintf(`{"rewrites":[{"commit":"%s","commits":[
		{"message":"a2","files":[{"path":"a.txt"}]},
		{"message":"b2","files":[{"path":"b.txt"}]}]}]}`, big)

	res, acErr := runRewrite([]byte(planJSON), r, rewriteOpts{})
	if acErr != nil {
		t.Fatalf("rewrite across a mid-range merge failed: %v | %s", acErr, acErr.Hint)
	}
	assertSameContent(t, r, oldHead, "HEAD")
	if !res.TreeIdentical {
		t.Fatal("tree_identical must be true")
	}
	// The merge survives with two parents and its second parent unchanged.
	parents, _ := r.Run("log", "-1", "--format=%P", "HEAD~1")
	if len(strings.Fields(parents)) != 2 {
		t.Fatalf("merge lost its parents: %q", parents)
	}
	sideTip, _ := r.ResolveSHA("side")
	if !strings.Contains(parents, sideTip) {
		t.Fatal("merge's second parent must stay the original side tip")
	}
	if res.Summary.Split != 1 || res.Summary.Replacements != 2 || res.Summary.Kept != 2 || res.Summary.TotalAfter != 4 {
		t.Fatalf("summary wrong: %+v", res.Summary)
	}
}

func TestRewriteProtectRefusesUpstreamCommits(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)
	mkCommit(t, r, dir, "upstream work", map[string]string{"a.txt": "a\n", "b.txt": "b\n"})
	upstream, _ := r.ResolveSHA("HEAD")
	must(t, run(r, "branch", "develop")) // simulates origin/develop
	mine := mkCommit(t, r, dir, "my big", map[string]string{"a.txt": "a2\n", "b.txt": "b2\n"})

	// Trying to split the upstream commit with --protect develop is refused.
	planJSON := fmt.Sprintf(`{"rewrites":[{"commit":"%s","commits":[
		{"message":"a","files":[{"path":"a.txt"}]},
		{"message":"b","files":[{"path":"b.txt"}]}]}]}`, upstream)
	_, acErr := runRewrite([]byte(planJSON), r, rewriteOpts{protect: []string{"develop"}})
	if acErr == nil || !strings.Contains(acErr.Message, "protected") {
		t.Fatalf("protected commit should be refused, got %v", acErr)
	}

	// My own commit above the protected ref splits fine with the same flag.
	planJSON = fmt.Sprintf(`{"rewrites":[{"commit":"%s","commits":[
		{"message":"a2","files":[{"path":"a.txt"}]},
		{"message":"b2","files":[{"path":"b.txt"}]}]}]}`, mine)
	oldHead, _ := r.ResolveSHA("HEAD")
	_, acErr = runRewrite([]byte(planJSON), r, rewriteOpts{protect: []string{"develop"}})
	if acErr != nil {
		t.Fatalf("own commit should split under --protect: %v", acErr)
	}
	assertSameContent(t, r, oldHead, "HEAD")
}

func TestRewriteSummaryOnlyOmitsReplacements(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)
	mkCommit(t, r, dir, "base", map[string]string{"a.txt": "a\n", "b.txt": "b\n"})
	big := mkCommit(t, r, dir, "big", map[string]string{"a.txt": "a2\n", "b.txt": "b2\n"})

	planJSON := fmt.Sprintf(`{"rewrites":[{"commit":"%s","commits":[
		{"message":"a","files":[{"path":"a.txt"}]},
		{"message":"b","files":[{"path":"b.txt"}]}]}]}`, big)
	res, acErr := runRewrite([]byte(planJSON), r, rewriteOpts{summaryOnly: true})
	if acErr != nil {
		t.Fatalf("rewrite failed: %v", acErr)
	}
	if len(res.Rewrites) != 0 {
		t.Fatal("--summary must omit per-rewrite replacement lists")
	}
	if res.Summary.Split != 1 || res.Summary.Replacements != 2 {
		t.Fatalf("summary counts wrong: %+v", res.Summary)
	}
}

func TestSplitEmitsFileFirstPlanThatRewriteAccepts(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)
	baseSHA := mkCommit(t, r, dir, "base", map[string]string{"a.txt": "a\n", "b.txt": "b\n", "c.txt": "c\n"})
	mkCommit(t, r, dir, "WB-7: feat: multi one", map[string]string{"a.txt": "a2\n", "b.txt": "b2\n"})
	mkCommit(t, r, dir, "single-file commit", map[string]string{"c.txt": "c2\n"})
	mkCommit(t, r, dir, "WB-8: feat: multi two", map[string]string{"a.txt": "a3\n", "c.txt": "c3\n"})
	oldHead, _ := r.ResolveSHA("HEAD")

	rp, skipped, acErr := runSplit(r, baseSHA+"..HEAD", "{subject} ({basename})")
	if acErr != nil {
		t.Fatalf("split failed: %v", acErr)
	}
	if len(rp.Rewrites) != 2 || skipped != 1 {
		t.Fatalf("want 2 rewrites + 1 skipped, got %d/%d", len(rp.Rewrites), skipped)
	}
	if rp.Rewrites[0].Commits[0].Message != "WB-7: feat: multi one (a.txt)" {
		t.Fatalf("template not applied: %q", rp.Rewrites[0].Commits[0].Message)
	}

	// The emitted plan is directly executable by hc rewrite.
	planJSON, err := jsonMarshal(rp)
	must(t, err)
	res, acErr := runRewrite(planJSON, r, rewriteOpts{})
	if acErr != nil {
		t.Fatalf("emitted plan rejected by rewrite: %v | %s", acErr, acErr.Hint)
	}
	assertSameContent(t, r, oldHead, "HEAD")
	if res.Summary.Split != 2 || res.Summary.Replacements != 4 || res.Summary.Kept != 1 {
		t.Fatalf("summary wrong: %+v", res.Summary)
	}
}

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }
