package cli

import (
	"os"
	"path/filepath"
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
	if got := hunkSectionLabel(hunks[0]); got != "hydrateParams" {
		t.Errorf("declaration hunk attributed to %q, want hydrateParams", got)
	}
	for i, h := range hunks {
		if got := hunkSectionLabel(h); got != "hydrateParams" {
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
	if got := hunkSectionLabel(hunks[0]); got != "" {
		t.Errorf("import-only hunk label = %q, want no section", got)
	}
	if got := hunkSectionLabel(hunks[1]); got != "handle" {
		t.Errorf("method hunk label = %q, want handle", got)
	}
	if w := multiSectionWarning(singleCommitPlan("Command.php", 0, 1), result.Files); w != "" {
		t.Errorf("an import riding with its method must not warn, got: %s", w)
	}
}
