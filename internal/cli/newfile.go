package cli

import (
	"os"
	"strings"

	"github.com/deligoez/hc/internal/diff"
	"github.com/deligoez/hc/internal/git"
	"github.com/deligoez/hc/internal/plan"
)

// A brand-new file arrives as ONE whole-file addition hunk, so it could never
// be split per function the way modified files are. expandNewFileHunks fixes
// that for history-facing commands (hc log / split / rewrite): it rewrites
// the single hunk into per-section synthetic hunks, detected with git's own
// funcname machinery. Staging is unaffected -- Reconstruct treats the pieces
// as ordinary insertion hunks against an empty base, and selecting all of
// them reproduces the original content byte-for-byte.
//
// Detection is skipped (file keeps its single hunk) for binary files, files
// where sections cannot discriminate, and pathological sizes.

// newFileExpandMaxLines guards detection cost on pathological inputs; nobody
// reviews a per-function split of a file this large anyway.
const newFileExpandMaxLines = 20000

// expandNewFileHunks splits each new file's whole-file addition hunk by
// enclosing section, in place. Files that cannot or need not split are left
// untouched. Detection failures degrade silently to the unsplit hunk: the
// draft quality drops, correctness does not.
func expandNewFileHunks(runner *git.Runner, files []diff.FileDiff) {
	for i := range files {
		fd := &files[i]
		if !fd.IsNew || fd.IsBinary || len(fd.Hunks) != 1 {
			continue
		}
		h := fd.Hunks[0]
		if h.OldCount != 0 || len(h.Lines) < 2 || len(h.Lines) > newFileExpandMaxLines {
			continue
		}
		expanded := splitAdditionBySection(runner, fd.Path, h)
		if len(expanded) < 2 {
			continue
		}
		fd.Hunks = expanded
	}
}

// insertion hunks. Returns nil when sections cannot discriminate (fewer than
// two function-like groups).
//
// Three refinements keep intermediate commits meaningful:
//   - In TEST files (per isTestFile) only test-like functions open groups;
//     helpers, setUp and other support functions fold into the group before
//     them, so the split is per-TEST, not per-function.
//   - Decoration lines immediately above a declaration (attributes like
//     #[Test], docblocks, comments) ride with the function they decorate.
//   - A trailing closing scaffold (lines less indented than the last
//     declaration -- a class's closing brace) is carved into its own final
//     hunk, labeled "closing scaffold". Committing it FIRST keeps every
//     intermediate file syntactically closed: later hunks insert their
//     lines before it by construction.
func splitAdditionBySection(runner *git.Runner, path string, h diff.Hunk) []diff.Hunk {
	sections, err := detectLineSections(runner, path, h.Lines)
	if err != nil {
		return nil
	}
	testFile := isTestFile(path)

	// Group boundaries open at lines whose enclosing section is a NEW
	// function-like declaration; everything before the first boundary
	// (package/imports/class scaffold) rides with the first group.
	type span struct {
		start     int // 0-based first line index (after decoration walk-back)
		declStart int // 0-based declaration line index
		section   string
	}
	var spans []span
	current := ""
	for k, sec := range sections {
		if sec == current || !isFunctionSection(sec) {
			continue
		}
		current = sec
		start := decorationStart(h.Lines, k)
		if testFile && !isTestFunction(sec, h.Lines, start, k) {
			continue // helper/support function: fold into the previous group
		}
		// Decoration walk-back must never reach back into the previous
		// group's declaration; keep starts strictly increasing.
		if n := len(spans); n > 0 && start <= spans[n-1].declStart {
			start = spans[n-1].declStart + 1
		}
		spans = append(spans, span{start: start, declStart: k, section: sec})
	}
	if len(spans) < 2 {
		return nil
	}
	spans[0].start = 0 // preamble rides with the first function

	// Carve the trailing closing scaffold: the suffix of lines less indented
	// than the last declaration (e.g. a class's closing brace).
	trailerStart := trailingScaffoldStart(h.Lines, spans[len(spans)-1].declStart)

	// Sanity: every span must own at least one line, in order. If detection
	// degenerated (it should not), keep the file whole rather than emit
	// zero-length hunks.
	for i := range spans {
		end := trailerStart
		if i+1 < len(spans) {
			end = spans[i+1].start
		}
		if end <= spans[i].start {
			return nil
		}
	}

	hunks := make([]diff.Hunk, 0, len(spans)+1)
	for i, sp := range spans {
		end := trailerStart
		if i+1 < len(spans) {
			end = spans[i+1].start
		}
		hunks = append(hunks, diff.Hunk{
			Index:    i,
			OldStart: 0,
			OldCount: 0,
			NewStart: int64(sp.start + 1),
			NewCount: int64(end - sp.start),
			Section:  sp.section,
			Lines:    h.Lines[sp.start:end],
		})
	}
	if trailerStart < len(h.Lines) {
		hunks = append(hunks, diff.Hunk{
			Index:    len(spans),
			OldStart: 0,
			OldCount: 0,
			NewStart: int64(trailerStart + 1),
			NewCount: int64(len(h.Lines) - trailerStart),
			Section:  trailerSectionLabel,
			Lines:    h.Lines[trailerStart:],
		})
	}
	return hunks
}

// trailerSectionLabel marks the synthetic closing-scaffold hunk of an
// expanded new file. Assign it to the FIRST replacement commit: every
// intermediate file then stays syntactically closed, because later hunks
// insert their lines before it (reconstruction is by original coordinates).
const trailerSectionLabel = "closing scaffold"

// trailingScaffoldStart returns the 0-based index where the file's trailing
// closing scaffold begins: the maximal suffix of blank lines and lines less
// indented than the last declaration. len(lines) means "no trailer" --
// column-0 declarations (Go, C, Python) end up here, and their files are
// already valid without one.
func trailingScaffoldStart(lines []diff.Line, lastDecl int) int {
	declIndent := indentWidth(lines[lastDecl].Content)
	if declIndent == 0 {
		return len(lines)
	}
	start := len(lines)
	for k := len(lines) - 1; k > lastDecl; k-- {
		text := strings.TrimRight(lines[k].Content, "\r\n")
		if strings.TrimSpace(text) != "" && indentWidth(text) >= declIndent {
			break
		}
		start = k
	}
	return start
}

// indentWidth counts leading whitespace, tabs weighted as 4 columns.
func indentWidth(s string) int {
	w := 0
	for _, r := range s {
		switch r {
		case ' ':
			w++
		case '\t':
			w += 4
		default:
			return w
		}
	}
	return w
}

// decorationStart walks back from a declaration line over the decoration
// lines immediately above it -- attributes (#[Test]), annotations (@Test),
// docblock/comment lines -- and returns where the function's block really
// starts. Blank lines stop the walk.
func decorationStart(lines []diff.Line, decl int) int {
	start := decl
	for k := decl - 1; k >= 0; k-- {
		if !isDecorationLine(lines[k].Content) {
			break
		}
		start = k
	}
	return start
}

// isDecorationLine reports whether a line is attribute/annotation/comment
// decoration that belongs to the declaration below it.
func isDecorationLine(content string) bool {
	s := strings.TrimSpace(strings.TrimRight(content, "\r\n"))
	if s == "" {
		return false
	}
	for _, p := range []string{"#[", "@", "/**", "/*", "*/", "*", "//", "'''", "\"\"\""} {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// isTestFunction reports whether a declaration in a test file is an actual
// test: its name matches a test-naming convention, or a decoration line
// directly above it carries a test attribute/annotation.
func isTestFunction(section string, lines []diff.Line, start, decl int) bool {
	name := strings.ToLower(funcNameOf(section))
	switch {
	case strings.HasPrefix(name, "test"),
		strings.HasPrefix(name, "should"),
		strings.HasPrefix(name, "spec_"),
		strings.HasPrefix(name, "benchmark"),
		strings.HasPrefix(name, "example"),
		strings.HasPrefix(name, "fuzz"),
		name == "it", strings.HasPrefix(name, "it_"):
		return true
	}
	for k := start; k < decl; k++ {
		s := strings.ToLower(lines[k].Content)
		for _, marker := range []string{"#[test", "@test", "[fact]", "[theory]"} {
			if strings.Contains(s, marker) {
				return true
			}
		}
	}
	return false
}

// funcNameOf extracts the bare function name from a raw funcname line:
// everything before the parameter list, last whitespace-separated field.
func funcNameOf(section string) string {
	s := section
	if i := strings.Index(s, "("); i >= 0 {
		s = s[:i]
	}
	if fields := strings.Fields(s); len(fields) > 0 {
		return fields[len(fields)-1]
	}
	return ""
}

// newFileMarker is appended to lines when probing sections; it only needs to
// make the line differ. A single character keeps the truncation window of
// git's ~80-byte funcname excerpt as small as possible (see sectionKey).
const newFileMarker = "~"

// sectionKeyMax trims harvested funcnames below git's excerpt cap so that a
// marked and an unmarked probe of the SAME declaration always normalize to
// the SAME string. Git cuts funcnames at ~80 bytes; the marker can displace
// the tail of a long declaration in one probe but not the other, so anything
// near the cap is untrustworthy and cut away.
const sectionKeyMax = 72

// sectionKey normalizes a funcname harvested from a marker-perturbed probe:
// strip the marker (whole, or cut mid-way by the excerpt cap), then truncate
// below the cap so both probes of one declaration agree byte-for-byte.
func sectionKey(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.ReplaceAll(s, newFileMarker, "")
	if len(s) > sectionKeyMax {
		s = s[:sectionKeyMax]
	}
	return strings.TrimSpace(s)
}

// detectLineSections returns, for each added line, the raw funcname of the
// section that ENCLOSES it, computed by git's own machinery: two tree diffs
// against marker-perturbed copies of the content yield git's function context
// for every line. Git reports the nearest declaration STRICTLY BEFORE a
// changed line, so the section enclosing line k is what git reports for line
// k+1 -- a sentinel line is appended so k+1 exists for the last line.
func detectLineSections(runner *git.Runner, path string, lines []diff.Line) ([]string, error) {
	n := len(lines)

	// Normalized content: every line newline-terminated, plus the sentinel.
	// Both probe copies share the normalization, so it never shows up as a
	// difference; the actual hunk lines are NOT modified by any of this.
	build := func(parity int) []byte {
		var b strings.Builder
		for k := 0; k < n+1; k++ {
			var text string
			if k < n {
				text = strings.TrimRight(lines[k].Content, "\n")
			} else {
				text = "~hc-sentinel~"
			}
			if parity >= 0 && k%2 == parity {
				text += newFileMarker
			}
			b.WriteString(text)
			b.WriteString("\n")
		}
		return []byte(b.String())
	}

	realTree, tmpClose, err := treeWithBlob(runner, path, build(-1))
	if err != nil {
		return nil, err
	}
	defer tmpClose()

	report := make([]string, n+2) // report[k] = git's funcname for a hunk at 1-based line k
	for parity := 0; parity <= 1; parity++ {
		markedTree, cl, err := treeWithBlob(runner, path, build(parity))
		if err != nil {
			return nil, err
		}
		cl()
		raw, err := runner.DiffTrees(markedTree, realTree)
		if err != nil {
			return nil, err
		}
		files, err := diff.Parse(raw)
		if err != nil || len(files) != 1 {
			return nil, err
		}
		for _, ph := range files[0].Hunks {
			if ph.OldCount == 1 && int(ph.OldStart) < len(report) {
				report[ph.OldStart] = sectionKey(ph.Section)
			}
		}
	}

	sections := make([]string, n)
	for k := 1; k <= n; k++ {
		sections[k-1] = report[k+1]
	}
	return sections, nil
}

// treeWithBlob writes content as the sole entry (at path) of a fresh tree
// object, via a throwaway index. The returned cleanup removes the index file.
func treeWithBlob(runner *git.Runner, path string, content []byte) (string, func(), error) {
	noop := func() {}
	sha, err := runner.HashObjectWrite(content)
	if err != nil {
		return "", noop, err
	}
	tmp, err := os.CreateTemp("", "hc-sections-index-*")
	if err != nil {
		return "", noop, err
	}
	tmpPath := tmp.Name()
	tmp.Close()
	_ = os.Remove(tmpPath)
	cleanup := func() { _ = os.Remove(tmpPath) }
	tempRunner := &git.Runner{Dir: runner.Dir, Env: []string{"GIT_INDEX_FILE=" + tmpPath}}
	if err := tempRunner.StageBlob("100644", sha, path); err != nil {
		cleanup()
		return "", noop, err
	}
	tree, err := tempRunner.WriteTree()
	if err != nil {
		cleanup()
		return "", noop, err
	}
	return tree, cleanup, nil
}

// isFunctionSection reports whether a raw funcname line plausibly declares a
// function or method -- the only boundaries worth a per-section commit in a
// new file. Scaffold contexts (package, imports, class/type declarations)
// must NOT open groups: they ride with the function that follows them.
// Usually the parameter list is the signal; when a very long declaration's
// "(" falls beyond git's ~80-byte funcname excerpt (and our sectionKey cut),
// declaration keywords decide instead.
func isFunctionSection(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	first := strings.ToLower(strings.Fields(s)[0])
	switch strings.TrimSuffix(first, ":") {
	case "import", "package", "use", "using", "require", "requires", "include",
		"from", "module", "namespace", "return", "if", "else", "for", "foreach",
		"while", "switch", "match", "var", "const", "let", "type":
		return false
	}
	if strings.Contains(s, "(") {
		return true
	}
	if len(s) >= sectionKeyMax-8 { // likely truncated by the excerpt cap
		lower := " " + strings.ToLower(s)
		for _, kw := range []string{" function ", " func ", " def ", " fn ", " sub "} {
			if strings.Contains(lower+" ", kw) {
				return true
			}
		}
	}
	return false
}

// isExpandedNewFile reports whether a new file's hunks are synthetic
// per-section expansions (multiple pure-insertion hunks).
func isExpandedNewFile(fd *diff.FileDiff) bool {
	if len(fd.Hunks) < 2 {
		return false
	}
	for _, h := range fd.Hunks {
		if h.OldCount != 0 {
			return false
		}
	}
	return true
}

// newFileSplitCommits builds the draft replacement commits for an expanded
// new file: one commit per section, with the closing-scaffold hunk riding in
// the FIRST commit so every intermediate file stays syntactically closed
// (later hunks insert before it by original-coordinate reconstruction).
func newFileSplitCommits(template, subject string, fd diff.FileDiff) []plan.Commit {
	trailer := -1
	if last := fd.Hunks[len(fd.Hunks)-1]; last.Section == trailerSectionLabel {
		trailer = last.Index
	}
	var commits []plan.Commit
	for i, h := range fd.Hunks {
		if h.Index == trailer {
			continue
		}
		indices := []int{h.Index}
		if i == 0 && trailer >= 0 {
			indices = append(indices, trailer)
		}
		commits = append(commits, plan.Commit{
			Message: renderSplitMessage(template, subject, fd.Path, sectionLabel(h.Section)),
			Files:   []plan.FileEntry{{Path: fd.Path, Hunks: indices}},
		})
	}
	return commits
}
