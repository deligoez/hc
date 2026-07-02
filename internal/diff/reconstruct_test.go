package diff

import "testing"

func rcHunk(oldStart, oldCount int64, lines ...Line) Hunk {
	return Hunk{OldStart: oldStart, OldCount: oldCount, Lines: lines}
}

func rcDel(s string) Line { return Line{Op: OpDelete, Content: s} }
func rcAdd(s string) Line { return Line{Op: OpAdd, Content: s} }

func TestReconstructReplaceMiddle(t *testing.T) {
	base := []byte("a\nb\nc\n")
	got, err := Reconstruct(base, []Hunk{rcHunk(2, 1, rcDel("b\n"), rcAdd("B\n"))})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "a\nB\nc\n" {
		t.Fatalf("got %q", got)
	}
}

func TestReconstructInsertAtTopAndEnd(t *testing.T) {
	base := []byte("a\nb\n")
	got, err := Reconstruct(base, []Hunk{
		rcHunk(0, 0, rcAdd("top\n")),
		rcHunk(2, 0, rcAdd("end\n")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "top\na\nb\nend\n" {
		t.Fatalf("got %q", got)
	}
}

func TestReconstructSubsetOfHunks(t *testing.T) {
	base := []byte("1\n2\n3\n4\n5\n")
	all := []Hunk{
		rcHunk(1, 1, rcDel("1\n"), rcAdd("one\n")),
		rcHunk(3, 1, rcDel("3\n"), rcAdd("three\n")),
		rcHunk(5, 1, rcDel("5\n"), rcAdd("five\n")),
	}
	got, err := Reconstruct(base, []Hunk{all[0], all[2]})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "one\n2\n3\n4\nfive\n" {
		t.Fatalf("got %q", got)
	}
}

func TestReconstructNoEOL(t *testing.T) {
	base := []byte("a\nb") // no trailing newline
	got, err := Reconstruct(base, []Hunk{rcHunk(2, 1, rcDel("b"), rcAdd("B-changed"))})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "a\nB-changed" {
		t.Fatalf("got %q", got)
	}
}

func TestReconstructAddEOLToLastLine(t *testing.T) {
	base := []byte("a\nb")
	got, err := Reconstruct(base, []Hunk{rcHunk(2, 1, rcDel("b"), rcAdd("b\n"), rcAdd("c\n"))})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "a\nb\nc\n" {
		t.Fatalf("got %q", got)
	}
}

func TestReconstructEmptyBaseAllAdds(t *testing.T) {
	got, err := Reconstruct(nil, []Hunk{rcHunk(0, 0, rcAdd("x\n"), rcAdd("y\n"))})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "x\ny\n" {
		t.Fatalf("got %q", got)
	}
}

func TestReconstructDeleteEverything(t *testing.T) {
	base := []byte("a\nb\n")
	got, err := Reconstruct(base, []Hunk{rcHunk(1, 2, rcDel("a\n"), rcDel("b\n"))})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %q", got)
	}
}

func TestReconstructDriftDetected(t *testing.T) {
	base := []byte("a\nCHANGED\nc\n")
	_, err := Reconstruct(base, []Hunk{rcHunk(2, 1, rcDel("b\n"), rcAdd("B\n"))})
	if err == nil {
		t.Fatal("mismatched delete line must be rejected")
	}
}

func TestReconstructOverlapRejected(t *testing.T) {
	base := []byte("a\nb\nc\n")
	_, err := Reconstruct(base, []Hunk{
		rcHunk(1, 2, rcDel("a\n"), rcDel("b\n")),
		rcHunk(2, 1, rcDel("b\n")),
	})
	if err == nil {
		t.Fatal("overlapping hunks must be rejected")
	}
}
