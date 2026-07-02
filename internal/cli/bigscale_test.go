package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deligoez/hc/internal/diff"
)

// TestManyFilesManyCommits guards the large-plan regime: dozens of files,
// dozens of hunks, one commit per hunk plus new files and deletions, in a
// shuffled order. Scaled-down version of the /tmp QA big-scale run (which
// passed at 200 files / 1000 hunks / 1000 commits, ~63 ms/commit).
func TestManyFilesManyCommits(t *testing.T) {
	if testing.Short() {
		t.Skip("large-scale test skipped in -short mode")
	}
	dir := t.TempDir()
	r := initRepo(t, dir)

	const nFiles = 20
	contents := make(map[string][]string, nFiles)
	for i := 0; i < nFiles; i++ {
		path := fmt.Sprintf("pkg%d/file%d.txt", i%4, i)
		must(t, os.MkdirAll(filepath.Join(dir, filepath.Dir(path)), 0o755))
		lines := make([]string, 60)
		for j := range lines {
			lines[j] = fmt.Sprintf("f%d-line%d", i, j)
		}
		contents[path] = lines
		must(t, os.WriteFile(filepath.Join(dir, path), []byte(strings.Join(lines, "\n")+"\n"), 0o644))
	}
	must(t, run(r, "add", "-A"))
	must(t, run(r, "commit", "-qm", "base"))

	// Three well-separated edits per file -> 3 hunks each.
	for path, lines := range contents {
		for k := 0; k < 3; k++ {
			lines[10+k*18] = fmt.Sprintf("EDIT-%s-%d", path, k)
		}
		must(t, os.WriteFile(filepath.Join(dir, path), []byte(strings.Join(lines, "\n")+"\n"), 0o644))
	}
	// Plus new files and a deletion.
	must(t, os.WriteFile(filepath.Join(dir, "brand-new.txt"), []byte("hi\n"), 0o644))
	deleted := "pkg0/file0.txt"
	must(t, os.Remove(filepath.Join(dir, deleted)))
	delete(contents, deleted)

	raw, err := r.Diff("-U0", "--no-renames", "--no-ext-diff")
	must(t, err)
	parsed, err := diff.Parse(raw)
	must(t, err)

	var commits []string
	for _, fd := range parsed {
		if fd.IsDeleted {
			commits = append(commits, fmt.Sprintf(`{"message":"rm %s","files":[{"path":"%s"}]}`, fd.Path, fd.Path))
			continue
		}
		for _, h := range fd.Hunks {
			commits = append(commits, fmt.Sprintf(
				`{"message":"c %s %d","files":[{"path":"%s","hunks":[%d]}]}`, fd.Path, h.Index, fd.Path, h.Index))
		}
	}
	commits = append(commits, `{"message":"add brand-new","files":[{"path":"brand-new.txt"}]}`)
	// Deterministic shuffle: reverse order decouples commit order from diff order.
	for i, j := 0, len(commits)-1; i < j; i, j = i+1, j-1 {
		commits[i], commits[j] = commits[j], commits[i]
	}

	planJSON := `{"commits":[` + strings.Join(commits, ",") + `]}`
	res, acErr := runPlan([]byte(planJSON), r, false)
	if acErr != nil {
		t.Fatalf("large plan failed: %s | %s", acErr.Message, acErr.Hint)
	}
	_ = res

	if out, _ := r.Run("status", "--porcelain"); strings.TrimSpace(out) != "" {
		t.Fatalf("tree not clean:\n%s", out)
	}
	for path, lines := range contents {
		got, err := os.ReadFile(filepath.Join(dir, path))
		must(t, err)
		if string(got) != strings.Join(lines, "\n")+"\n" {
			t.Fatalf("content mismatch: %s", path)
		}
	}
	count, _ := r.Run("rev-list", "--count", "HEAD")
	want := fmt.Sprintf("%d", 2+len(commits))
	if strings.TrimSpace(count) != want {
		t.Fatalf("commit count = %s, want %s", strings.TrimSpace(count), want)
	}
}
