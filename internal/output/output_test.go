package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestErrorConstructorsSetExitCodes(t *testing.T) {
	if e := NewValidationError("bad plan", "fix it"); e.Code != 2 {
		t.Errorf("validation error code = %d, want 2", e.Code)
	}
	if e := NewExecutionError("git broke", ""); e.Code != 3 {
		t.Errorf("execution error code = %d, want 3", e.Code)
	}
}

func TestACErrorImplementsError(t *testing.T) {
	e := NewValidationError("something failed", "")
	if e.Error() != "something failed" {
		t.Errorf("Error() = %q", e.Error())
	}
}

func TestPrintErrorJSON(t *testing.T) {
	var buf bytes.Buffer
	p := &Printer{Out: &buf, ErrOut: &buf, ForceJSON: true}

	p.PrintError(NewValidationError("bad hunk", "run hc diff"))

	var got ACError
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	if got.Message != "bad hunk" || got.Code != 2 || got.Hint != "run hc diff" {
		t.Errorf("unexpected error JSON: %+v", got)
	}
}

func TestPrintErrorTTY(t *testing.T) {
	var out, errOut bytes.Buffer
	p := &Printer{Out: &out, ErrOut: &errOut, IsTTY: true, NoColor: true}

	p.PrintError(NewValidationError("bad hunk", "run hc diff"))

	got := errOut.String()
	if !strings.Contains(got, "error: bad hunk") {
		t.Errorf("missing error line: %q", got)
	}
	if !strings.Contains(got, "hint: run hc diff") {
		t.Errorf("missing hint line: %q", got)
	}
}

func TestUseJSON(t *testing.T) {
	if (&Printer{IsTTY: true}).UseJSON() {
		t.Error("TTY without ForceJSON should not use JSON")
	}
	if !(&Printer{IsTTY: true, ForceJSON: true}).UseJSON() {
		t.Error("ForceJSON should win over TTY")
	}
	if !(&Printer{IsTTY: false}).UseJSON() {
		t.Error("non-TTY should default to JSON")
	}
}

func TestInfoRespectsQuiet(t *testing.T) {
	var buf bytes.Buffer
	p := &Printer{Out: &buf, Quiet: true}
	p.Info("should not appear")
	if buf.Len() != 0 {
		t.Errorf("quiet printer wrote output: %q", buf.String())
	}

	p.Quiet = false
	p.Info("count %d", 3)
	if buf.String() != "count 3\n" {
		t.Errorf("Info output = %q", buf.String())
	}
}
