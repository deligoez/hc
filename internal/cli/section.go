package cli

import (
	"path/filepath"
	"strings"

	"github.com/deligoez/hc/internal/diff"
)

// Git attributes a hunk to the nearest declaration STRICTLY BEFORE its first
// changed line. A hunk that changes a declaration line itself -- adding a
// parameter to a signature is the common case -- is therefore labeled with
// the PRECEDING function, which makes an atomic signature change look like it
// spans two sections. The hunk's own changed lines carry the truth, so this
// file re-attributes from them before any section signal is derived.

// declKeywords introduce a named function or method. Type-level keywords
// (class, struct, ...) are deliberately absent: a hunk touching a type
// declaration alongside method hunks really does span two sections.
var declKeywords = map[string]bool{
	"func": true, "function": true, "def": true, "defp": true,
	"fn": true, "sub": true, "proc": true, "method": true,
}

// importKeywords introduce a line that imports, includes or aliases
// something. A hunk made only of these is not a section of its own: imports
// ride with the code that needs them.
var importKeywords = map[string]bool{
	"import": true, "use": true, "using": true, "from": true,
	"require": true, "require_once": true, "include": true, "include_once": true,
}

// hunkSectionLabel returns the short label of the section a hunk really
// belongs to: for a prose document the heading it sits under, otherwise the
// function it declares if its changed lines declare one, "" if it only moves
// imports around, and git's reported section as the fallback.
func hunkSectionLabel(path string, h diff.Hunk) string {
	if isProseFile(path) {
		return headingLabel(h.Section)
	}
	if _, name := declaringLine(h.Lines); name != "" {
		return name
	}
	if isImportOnlyHunk(h.Lines) {
		return ""
	}
	return sectionLabel(h.Section)
}

// hunkSection returns the raw section line to display for a hunk: the heading
// for a prose document, else the declaration its own lines carry, else git's
// reported one. Keeping the displayed section and the derived label on the
// same source avoids showing an agent one section while grouping by another.
func hunkSection(path string, h diff.Hunk) string {
	if isProseFile(path) {
		if headingLabel(h.Section) == "" {
			return ""
		}
		return h.Section
	}
	if line, name := declaringLine(h.Lines); name != "" {
		return line
	}
	return h.Section
}

// declaringLine finds the declaration a hunk's own change makes, and returns
// that line with the name it declares.
//
// Only the hunk's LEADING changed line can be misattributed: git labels a
// hunk by the nearest declaration strictly before its first changed line, so
// the label is wrong exactly when that first line is itself a declaration. A
// declaration further down the hunk is a separate block the hunk merely
// contains -- git's label for the start still holds, and overriding it would
// also clobber the synthetic sections of an expanded new file, whose first
// group deliberately carries the preamble and the helpers before its test.
//
// A modified declaration appears as its old line then its new one; the added
// spelling wins, so a renamed function is attributed to the name it now has.
func declaringLine(lines []diff.Line) (line, name string) {
	var first, firstAdd *diff.Line
	for i := range lines {
		switch lines[i].Op {
		case diff.OpContext:
			continue
		case diff.OpAdd:
			if first == nil {
				first = &lines[i]
			}
			firstAdd = &lines[i]
		case diff.OpDelete:
			if first == nil {
				first = &lines[i]
			}
			continue
		}
		break
	}
	for _, l := range []*diff.Line{firstAdd, first} {
		if l == nil {
			continue
		}
		text := strings.TrimRight(l.Content, "\r\n")
		if name := declaredName(text); name != "" {
			return strings.TrimSpace(text), name
		}
	}
	return "", ""
}

// declaredName returns the name a source line declares, or "" when the line
// declares nothing nameable. Two guards keep incidental keywords out:
// everything before the keyword must be plain modifier words (so comments,
// string literals, assignments and inline `foo(function ...)` callbacks are
// rejected), and the keyword must be followed by an identifier (so anonymous
// closures are rejected). A Go method receiver between the two is skipped.
func declaredName(line string) string {
	for i := 0; i < len(line); {
		if !isIdentByte(line[i]) {
			if !isSpaceByte(line[i]) {
				return "" // punctuation before the keyword: not a declaration
			}
			i++
			continue
		}
		j := i
		for j < len(line) && isIdentByte(line[j]) {
			j++
		}
		if declKeywords[strings.ToLower(line[i:j])] {
			return nameAfterKeyword(line[j:])
		}
		i = j
	}
	return ""
}

// nameAfterKeyword reads the declared name out of the text following a
// declaration keyword, skipping a leading parenthesized group (Go's method
// receiver) when one is present.
func nameAfterKeyword(rest string) string {
	i := 0
	for i < len(rest) && isSpaceByte(rest[i]) {
		i++
	}
	if i < len(rest) && rest[i] == '(' {
		depth := 0
		for ; i < len(rest); i++ {
			if rest[i] == '(' {
				depth++
			} else if rest[i] == ')' {
				if depth--; depth == 0 {
					i++
					break
				}
			}
		}
		for i < len(rest) && isSpaceByte(rest[i]) {
			i++
		}
	}
	j := i
	for j < len(rest) && isIdentByte(rest[j]) {
		j++
	}
	name := rest[i:j]
	if name == "" || (name[0] >= '0' && name[0] <= '9') {
		return ""
	}
	return name
}

// isImportOnlyHunk reports whether every changed line in a hunk is an import,
// include or `use` statement.
func isImportOnlyHunk(lines []diff.Line) bool {
	changed := 0
	for _, l := range lines {
		if l.Op == diff.OpContext {
			continue
		}
		s := strings.TrimSpace(strings.TrimRight(l.Content, "\r\n"))
		if s == "" {
			continue
		}
		first := strings.ToLower(strings.Fields(s)[0])
		if !importKeywords[strings.TrimSuffix(first, ";")] {
			return false
		}
		changed++
	}
	return changed > 0
}

// proseExtensions are documents whose sections are HEADINGS, not code. Git's
// DEFAULT funcname regex treats any line starting with a letter as context,
// so an ordinary English sentence becomes a hunk's "section" -- and because
// technical prose names functions with their parens attached
// (“ `definition()` “ in a doc), no code-shape test can tell the two apart.
// Attribution for these files therefore comes from headings alone.
var proseExtensions = map[string]bool{
	".md": true, ".markdown": true, ".mdown": true, ".mkd": true, ".mkdn": true,
	".rst": true, ".adoc": true, ".asciidoc": true, ".txt": true, ".text": true,
}

// isProseFile reports whether a path is a prose document by extension.
func isProseFile(path string) bool {
	return proseExtensions[strings.ToLower(filepath.Ext(path))]
}

// headingLabel returns the title of an ATX markdown heading ("## Errors" ->
// "Errors"), or "" for anything else -- which is every prose line, so a
// document only yields sections where it really has them.
//
// Git reports headings as funcnames only when the file uses its built-in
// markdown driver, i.e. the repository maps it in .gitattributes:
//
//	*.md diff=markdown
//
// This is the same opt-in that indented-method languages already need (see
// `*.php diff=php`). Without it a prose file simply has no sections, and
// `hc plan` falls back to contiguity gaps.
func headingLabel(section string) string {
	s := strings.TrimSpace(section)
	level := 0
	for level < len(s) && s[level] == '#' {
		level++
	}
	if level == 0 || level > 6 || level == len(s) {
		return ""
	}
	if s[level] != ' ' && s[level] != '\t' {
		return ""
	}
	return strings.TrimSpace(strings.TrimRight(s[level:], " \t#"))
}

func isIdentByte(b byte) bool {
	return b == '_' || b == '$' ||
		(b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

func isSpaceByte(b byte) bool { return b == ' ' || b == '\t' }
