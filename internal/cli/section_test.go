package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deligoez/hc/internal/plan"
)

// singleCommitPlan wraps one file's hunk selection in a one-commit plan, for
// exercising the advisory granularity check.
func singleCommitPlan(path string, hunks ...int) *plan.Plan {
	return &plan.Plan{Commits: []plan.Commit{{
		Message: "feat: one idea",
		Files:   []plan.FileEntry{{Path: path, Hunks: hunks}},
	}}}
}

// TestSignatureChangeStaysOneSection covers the false positive reported from
// production: git labels a hunk with the declaration STRICTLY BEFORE its first
// changed line, so changing a signature -- an atomic, unsplittable change --
// made the granularity check see the PRECEDING function and warn.
func TestSignatureChangeStaysOneSection(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)

	src := "package main\n\n" +
		"func param(key string) string {\n\treturn key\n}\n\n" +
		"func hydrateParams(raw []string) {\n\tfor _, v := range raw {\n\t\t_ = v\n\t}\n}\n"
	f := filepath.Join(dir, "params.go")
	must(t, os.WriteFile(f, []byte(src), 0o644))
	must(t, run(r, "add", "params.go"))
	must(t, run(r, "commit", "-m", "add params.go"))

	// Add a parameter to hydrateParams and use it: the first hunk changes the
	// declaration line itself.
	modified := "package main\n\n" +
		"func param(key string) string {\n\treturn key\n}\n\n" +
		"func hydrateParams(raw []string, strict bool) {\n\tfor _, v := range raw {\n\t\t_ = v\n\t}\n\t_ = strict\n}\n"
	must(t, os.WriteFile(f, []byte(modified), 0o644))

	result, err := runDiff(r)
	if err != nil {
		t.Fatalf("runDiff: %v", err)
	}
	hunks := result.Files[0].Hunks
	if len(hunks) < 2 {
		t.Fatalf("want the declaration hunk plus a body hunk, got %d", len(hunks))
	}
	if got := hunkSectionLabel("params.go", hunks[0]); got != "hydrateParams" {
		t.Errorf("declaration hunk attributed to %q, want hydrateParams", got)
	}
	for i, h := range hunks {
		if got := hunkSectionLabel("params.go", h); got != "hydrateParams" {
			t.Errorf("hunk %d label = %q, want hydrateParams", i, got)
		}
	}

	indices := make([]int, len(hunks))
	for i := range hunks {
		indices[i] = i
	}
	if w := multiSectionWarning(singleCommitPlan("params.go", indices...), result.Files); w != "" {
		t.Errorf("signature change must not warn, got: %s", w)
	}
}

// TestImportOnlyHunkClaimsNoSection covers the second reported shape: adding
// a trait/import at the top of a class body plus using it in a method is one
// idea, but git labels the import hunk with the enclosing class. Imports ride
// with the code that needs them, so they claim no section of their own.
func TestImportOnlyHunkClaimsNoSection(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)
	must(t, os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte("*.php diff=php\n"), 0o644))

	src := "<?php\n\nclass Command\n{\n    protected $signature = 'app:run';\n\n" +
		"    public function handle(): int\n    {\n        $this->info('running');\n\n        return 0;\n    }\n}\n"
	f := filepath.Join(dir, "Command.php")
	must(t, os.WriteFile(f, []byte(src), 0o644))
	must(t, run(r, "add", "-A"))
	must(t, run(r, "commit", "-m", "add Command.php"))

	modified := "<?php\n\nclass Command\n{\n    use InteractsWithQueue;\n\n    protected $signature = 'app:run';\n\n" +
		"    public function handle(): int\n    {\n        $this->dispatchQueued();\n        $this->info('running');\n\n        return 0;\n    }\n}\n"
	must(t, os.WriteFile(f, []byte(modified), 0o644))

	result, err := runDiff(r)
	if err != nil {
		t.Fatalf("runDiff: %v", err)
	}
	hunks := result.Files[0].Hunks
	if len(hunks) != 2 {
		t.Fatalf("want the use-line hunk plus the method hunk, got %d", len(hunks))
	}
	if got := hunkSectionLabel("Command.php", hunks[0]); got != "" {
		t.Errorf("import-only hunk label = %q, want no section", got)
	}
	if got := hunkSectionLabel("Command.php", hunks[1]); got != "handle" {
		t.Errorf("method hunk label = %q, want handle", got)
	}
	if w := multiSectionWarning(singleCommitPlan("Command.php", 0, 1), result.Files); w != "" {
		t.Errorf("an import riding with its method must not warn, got: %s", w)
	}
}

// TestDeclaredName pins the two guards that keep incidental keywords from
// being read as declarations: only modifier words may precede the keyword,
// and the keyword must be followed by a name.
func TestDeclaredName(t *testing.T) {
	cases := []struct {
		line string
		want string
	}{
		{"func hydrateParams(raw []string) {", "hydrateParams"},
		{"func (r *Runner) Diff(args ...string) error {", "Diff"},
		{"func Map[T any](xs []T) []T {", "Map"},
		{"    public function hydrateParams(array $raw, bool $strict = false): void", "hydrateParams"},
		{"    protected static function make(): self", "make"},
		{"async def fetch(session, url):", "fetch"},
		{"export default function handler(req, res) {", "handler"},
		{"def foo", "foo"},
		// Anonymous: a keyword with no name declares no section.
		{"        $callback = function ($x) {", ""},
		{"\treturn function() {}", ""},
		// Punctuation before the keyword: comments, strings, calls.
		{"// call the function later", ""},
		{"        $msg = \"the function foo(x) failed\";", ""},
		{"        $this->each(function ($row) {", ""},
		{"#[Test]", ""},
		// No declaration keyword at all.
		{"        $this->hydrated = true;", ""},
		{"static void helper(void) {", ""},
	}
	for _, c := range cases {
		if got := declaredName(c.line); got != c.want {
			t.Errorf("declaredName(%q) = %q, want %q", c.line, got, c.want)
		}
	}
}

// TestProseParentheticalIsNotASection guards the other end of the section
// signal: git's DEFAULT funcname regex treats any line starting with a letter
// as context, so a markdown paragraph becomes a "section". A parenthetical
// ("... for agent convenience (P1)") used to pass the code-construct filter on
// the bare presence of a "(", labeling docs commits with harvested prose.
func TestProseParentheticalIsNotASection(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)

	src := "# Doc\n\n" +
		"This command exists purely for agent convenience (P1). The agent may also read the diff.\n\n" +
		"- first bullet\n- second bullet\n\n" +
		"hc requires a clean staging area (no pre-existing staged changes). This is a hard rule.\n\n" +
		"- third bullet\n- fourth bullet\n"
	f := filepath.Join(dir, "doc.md")
	must(t, os.WriteFile(f, []byte(src), 0o644))
	must(t, run(r, "add", "doc.md"))
	must(t, run(r, "commit", "-m", "add doc.md"))

	modified := strings.ReplaceAll(src, "- second bullet", "- second bullet, reworded")
	modified = strings.ReplaceAll(modified, "- fourth bullet", "- fourth bullet, reworded")
	must(t, os.WriteFile(f, []byte(modified), 0o644))

	result, err := runDiff(r)
	if err != nil {
		t.Fatalf("runDiff: %v", err)
	}
	hunks := result.Files[0].Hunks
	if len(hunks) != 2 {
		t.Fatalf("want 2 hunks, got %d", len(hunks))
	}
	for i, h := range hunks {
		if !strings.Contains(h.Section, "(") {
			t.Fatalf("hunk %d must carry a prose parenthetical to be a real test, got %q", i, h.Section)
		}
		if got := hunkSectionLabel("doc.md", h); got != "" {
			t.Errorf("hunk %d labeled %q from prose %q, want no section", i, got, h.Section)
		}
	}
	if w := multiSectionWarning(singleCommitPlan("doc.md", 0, 1), result.Files); w != "" {
		t.Errorf("prose must not drive the granularity warning, got: %s", w)
	}
}
