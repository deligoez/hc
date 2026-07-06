package cli

import (
	"os"
	"strings"

	"github.com/deligoez/hc/internal/diff"
	"github.com/deligoez/hc/internal/git"
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

// splitAdditionBySection cuts one whole-file insertion hunk into per-section
// insertion hunks. Returns nil when sections cannot discriminate (fewer than
// two function-like groups).
func splitAdditionBySection(runner *git.Runner, path string, h diff.Hunk) []diff.Hunk {
	sections, err := detectLineSections(runner, path, h.Lines)
	if err != nil {
		return nil
	}

	// Group boundaries open at lines whose enclosing section is a NEW
	// function-like declaration; everything before the first boundary
	// (package/imports/class scaffold) rides with the first group.
	type span struct {
		start   int // 0-based first line index
		section string
	}
	var spans []span
	current := ""
	for k, sec := range sections {
		if sec != current && isFunctionSection(sec) {
			current = sec
			spans = append(spans, span{start: k, section: sec})
		}
	}
	if len(spans) < 2 {
		return nil
	}
	spans[0].start = 0 // preamble rides with the first function

	hunks := make([]diff.Hunk, 0, len(spans))
	for i, sp := range spans {
		end := len(h.Lines)
		if i+1 < len(spans) {
			end = spans[i+1].start
		}
		nh := diff.Hunk{
			Index:    i,
			OldStart: 0,
			OldCount: 0,
			NewStart: int64(sp.start + 1),
			NewCount: int64(end - sp.start),
			Section:  sp.section,
			Lines:    h.Lines[sp.start:end],
		}
		hunks = append(hunks, nh)
	}
	return hunks
}

// newFileMarker is appended to lines when probing sections; it only needs to
// make the line differ, and it is stripped from every harvested section.
const newFileMarker = "~hc-sec~"

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
				sec := strings.ReplaceAll(ph.Section, newFileMarker, "")
				report[ph.OldStart] = strings.TrimSpace(sec)
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
func isFunctionSection(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || !strings.Contains(s, "(") {
		return false
	}
	first := strings.ToLower(strings.Fields(s)[0])
	switch strings.TrimSuffix(first, ":") {
	case "import", "package", "use", "using", "require", "requires", "include",
		"from", "module", "namespace", "return", "if", "else", "for", "foreach",
		"while", "switch", "match", "var", "const", "let", "type":
		return false
	}
	return true
}
