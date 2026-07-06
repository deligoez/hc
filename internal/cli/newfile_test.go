package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deligoez/hc/internal/diff"
	"github.com/deligoez/hc/internal/git"
)

const newFileGoTests = `package m

import "testing"

func TestCreate(t *testing.T) {
	if 1 != 1 {
		t.Fatal("a")
	}
}

func TestUpdate(t *testing.T) {
	if 2 != 2 {
		t.Fatal("b")
	}
}

func TestDelete(t *testing.T) {
	if 3 != 3 {
		t.Fatal("c")
	}
}
`

// TestNewFileLogSplitsPerTestFunction verifies that hc log expands a brand-new
// test file's single whole-file hunk into one synthetic hunk per test
// function, with the preamble riding on the first one.
func TestNewFileLogSplitsPerTestFunction(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)
	must(t, os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("s\n"), 0o644))
	must(t, run(r, "add", "-A"))
	must(t, run(r, "commit", "-qm", "base"))
	must(t, os.WriteFile(filepath.Join(dir, "order_test.go"), []byte(newFileGoTests), 0o644))
	must(t, run(r, "add", "-A"))
	must(t, run(r, "commit", "-qm", "test: add order tests"))

	result, acErr := runLog(r, "HEAD~1..HEAD", false)
	if acErr != nil {
		t.Fatalf("runLog: %v", acErr)
	}
	hunks := result.Commits[0].Files[0].Hunks
	if len(hunks) != 3 {
		t.Fatalf("want 3 per-test hunks, got %d: %+v", len(hunks), hunks)
	}
	for i, want := range []string{"TestCreate", "TestUpdate", "TestDelete"} {
		if !strings.Contains(hunks[i].Section, want) {
			t.Errorf("hunk %d section = %q, want %s", i, hunks[i].Section, want)
		}
	}
	if !strings.Contains(hunks[0].Content, "package m") {
		t.Errorf("preamble should ride with the first test, content:\n%s", hunks[0].Content)
	}
	if hunks[0].Added+hunks[1].Added+hunks[2].Added != 21 {
		t.Errorf("expanded hunks must cover all 21 lines, got %d+%d+%d",
			hunks[0].Added, hunks[1].Added, hunks[2].Added)
	}
}

// TestNewFileRewritePerTest splits a committed new test file into per-test
// commits via runRewrite and verifies the tree invariant plus intermediate
// content: each replacement appends exactly one test.
func TestNewFileRewritePerTest(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)
	must(t, os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("s\n"), 0o644))
	must(t, run(r, "add", "-A"))
	must(t, run(r, "commit", "-qm", "base"))
	must(t, os.WriteFile(filepath.Join(dir, "order_test.go"), []byte(newFileGoTests), 0o644))
	must(t, run(r, "add", "-A"))
	must(t, run(r, "commit", "-qm", "test: add order tests"))
	sha, err := r.ResolveSHA("HEAD")
	must(t, err)

	planJSON := `{"rewrites":[{"commit":"` + sha + `","commits":[
		{"message":"test: add TestCreate","files":[{"path":"order_test.go","hunks":[0]}]},
		{"message":"test: add TestUpdate","files":[{"path":"order_test.go","hunks":[1]}]},
		{"message":"test: add TestDelete","files":[{"path":"order_test.go","hunks":[2]}]}]}]}`
	res, acErr := runRewrite([]byte(planJSON), r, rewriteOpts{})
	if acErr != nil {
		t.Fatalf("runRewrite: %v | %s", acErr, acErr.Hint)
	}
	if !res.TreeIdentical || res.Summary.Replacements != 3 {
		t.Fatalf("unexpected result: %+v", res)
	}

	after1, err := r.Run("show", "HEAD~2:order_test.go")
	must(t, err)
	if !strings.Contains(after1, "TestCreate") || strings.Contains(after1, "TestUpdate") {
		t.Errorf("first replacement should contain only TestCreate:\n%s", after1)
	}
	final, err := r.Run("show", "HEAD:order_test.go")
	must(t, err)
	if final != newFileGoTests {
		t.Errorf("final content must be byte-identical to the original")
	}
}

// TestNewFilePlainTextStaysWhole verifies expansion falls back gracefully:
// a new file without function-like sections keeps its single hunk.
func TestNewFilePlainTextStaysWhole(t *testing.T) {
	dir := t.TempDir()
	r := initRepo(t, dir)
	must(t, os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("s\n"), 0o644))
	must(t, run(r, "add", "-A"))
	must(t, run(r, "commit", "-qm", "base"))
	doc := "# Title\n\nSome prose here.\n\nMore prose.\n\n## Section two\n\nEven more prose lines.\n"
	must(t, os.WriteFile(filepath.Join(dir, "NOTES.md"), []byte(doc), 0o644))
	must(t, run(r, "add", "-A"))
	must(t, run(r, "commit", "-qm", "docs: add notes"))

	result, acErr := runLog(r, "HEAD~1..HEAD", false)
	if acErr != nil {
		t.Fatalf("runLog: %v", acErr)
	}
	hunks := result.Commits[0].Files[0].Hunks
	if len(hunks) != 1 {
		t.Fatalf("plain text must stay one hunk, got %d", len(hunks))
	}
}

// TestIsFunctionSectionHeuristic covers the boundary filter: function-like
// declarations open groups, scaffold contexts do not.
func TestIsFunctionSectionHeuristic(t *testing.T) {
	cases := []struct {
		section string
		want    bool
	}{
		{"func TestCreate(t *testing.T) {", true},
		{"public function test_it_stores(): void", true},
		{"def test_models(self):", true},
		{"it('stores the order', () => {", true},
		{"static int parse(const char *s)", true},
		{"package m", false},
		{"import (", false},
		{"use Tests\\TestCase;", false},
		{"class StoreOrderActionTest extends TestCase", false},
		{"type Config struct {", false},
		{"const (", false},
		{"if err != nil {", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isFunctionSection(c.section); got != c.want {
			t.Errorf("isFunctionSection(%q) = %v, want %v", c.section, got, c.want)
		}
	}
}

const newFilePHPTests = `<?php

namespace Tests\Machines;

use PHPUnit\Framework\Attributes\Test;
use Tests\TestCase;

class StoreOrderActionTest extends TestCase
{
    protected function setUp(): void
    {
        parent::setUp();
    }

    private function makeProduct(): array
    {
        return ['id' => 1];
    }

    #[Test]
    public function it_stores_an_order(): void
    {
        $this->assertTrue(true);
    }

    private function runAction(): void
    {
    }

    #[Test]
    public function it_fires_the_event(): void
    {
        $this->assertTrue(true);
    }
}
`

// phpTestRepo commits newFilePHPTests as a brand-new file (with the php diff
// driver enabled) and returns the repo runner.
func phpTestRepo(t *testing.T) *git.Runner {
	t.Helper()
	dir := t.TempDir()
	r := initRepo(t, dir)
	must(t, os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte("*.php diff=php\n"), 0o644))
	must(t, run(r, "add", "-A"))
	must(t, run(r, "commit", "-qm", "base"))
	must(t, os.MkdirAll(filepath.Join(dir, "tests"), 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "tests/StoreOrderActionTest.php"), []byte(newFilePHPTests), 0o644))
	must(t, run(r, "add", "-A"))
	must(t, run(r, "commit", "-qm", "test: add StoreOrderActionTest"))
	return r
}

// TestNewFilePerTestGrouping verifies the PHP shape from production: helpers
// and setUp fold into the preceding group, #[Test] attributes ride with their
// test, and the class's closing brace becomes a "closing scaffold" hunk.
func TestNewFilePerTestGrouping(t *testing.T) {
	r := phpTestRepo(t)
	result, acErr := runLog(r, "HEAD~1..HEAD", false)
	if acErr != nil {
		t.Fatalf("runLog: %v", acErr)
	}
	hunks := result.Commits[0].Files[0].Hunks
	if len(hunks) != 3 {
		t.Fatalf("want 2 per-test hunks + closing scaffold, got %d: %+v", len(hunks), hunks)
	}
	if !strings.Contains(hunks[0].Section, "it_stores_an_order") ||
		!strings.Contains(hunks[1].Section, "it_fires_the_event") ||
		hunks[2].Section != "closing scaffold" {
		t.Fatalf("sections wrong: %q / %q / %q", hunks[0].Section, hunks[1].Section, hunks[2].Section)
	}
	if !strings.Contains(hunks[0].Content, "setUp") || !strings.Contains(hunks[0].Content, "makeProduct") {
		t.Errorf("helpers before the first test must fold into it:\n%s", hunks[0].Content)
	}
	if !strings.HasPrefix(strings.TrimPrefix(hunks[1].Content, "+"), "    #[Test]") {
		t.Errorf("the #[Test] attribute must ride with its test, got:\n%s", hunks[1].Content)
	}
	if !strings.Contains(hunks[0].Content, "runAction") {
		t.Errorf("a helper between tests must fold into the preceding group:\n%s", hunks[0].Content)
	}
}

// TestNewFileRewriteValidIntermediates splits the PHP file per test with the
// closing scaffold in the FIRST commit and verifies every intermediate file
// is brace-balanced (i.e. the class is closed at each step).
func TestNewFileRewriteValidIntermediates(t *testing.T) {
	r := phpTestRepo(t)
	sha, err := r.ResolveSHA("HEAD")
	must(t, err)

	planJSON := `{"rewrites":[{"commit":"` + sha + `","commits":[
		{"message":"test: it_stores_an_order","files":[{"path":"tests/StoreOrderActionTest.php","hunks":[0,2]}]},
		{"message":"test: it_fires_the_event","files":[{"path":"tests/StoreOrderActionTest.php","hunks":[1]}]}]}]}`
	res, acErr := runRewrite([]byte(planJSON), r, rewriteOpts{})
	if acErr != nil {
		t.Fatalf("runRewrite: %v | %s", acErr, acErr.Hint)
	}
	if !res.TreeIdentical || res.Summary.Replacements != 2 {
		t.Fatalf("unexpected result: %+v", res)
	}

	for _, ref := range []string{"HEAD~1", "HEAD"} {
		content, err := r.Run("show", ref+":tests/StoreOrderActionTest.php")
		must(t, err)
		if strings.Count(content, "{") != strings.Count(content, "}") {
			t.Errorf("%s: intermediate file is not brace-balanced:\n%s", ref, content)
		}
	}
	final, err := r.Run("show", "HEAD:tests/StoreOrderActionTest.php")
	must(t, err)
	if final != newFilePHPTests {
		t.Errorf("final content must be byte-identical to the original")
	}
}

// TestTrailingScaffoldHeuristic covers the indentation-based trailer cut:
// suffix lines less indented than the last declaration are scaffold;
// column-0 declarations produce no trailer.
func TestTrailingScaffoldHeuristic(t *testing.T) {
	mk := func(texts ...string) []diff.Line {
		lines := make([]diff.Line, len(texts))
		for i, s := range texts {
			lines[i] = diff.Line{Op: diff.OpAdd, Content: s + "\n"}
		}
		return lines
	}
	// Indented method, class brace at column 0: trailer = final brace.
	phpish := mk("class C {", "    function test_a() {", "        x;", "    }", "}")
	if got := trailingScaffoldStart(phpish, 1); got != 4 {
		t.Errorf("phpish trailer start = %d, want 4", got)
	}
	// Column-0 declaration (Go): no trailer.
	goish := mk("func TestA() {", "\tx()", "}")
	if got := trailingScaffoldStart(goish, 0); got != 3 {
		t.Errorf("goish trailer start = %d, want 3 (no trailer)", got)
	}
	// Nested closers (namespace + class) are all scaffold.
	nested := mk("namespace N {", "  class C {", "    void test_a() {", "    }", "  }", "}")
	if got := trailingScaffoldStart(nested, 2); got != 4 {
		t.Errorf("nested trailer start = %d, want 4", got)
	}
}
