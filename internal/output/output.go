package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/fatih/color"
	"github.com/mattn/go-isatty"
)

// Result is the top-level output for hc run.
type Result struct {
	Committed int            `json:"committed"`
	Total     int            `json:"total"`
	Commits   []CommitResult `json:"commits"`
	Warnings  []string       `json:"warnings,omitempty"`
	Error     string         `json:"error,omitempty"`
	Code      int            `json:"code,omitempty"`
	Hint      string         `json:"hint,omitempty"`
}

// CommitResult is the output for a single commit.
type CommitResult struct {
	Index   int          `json:"index"`
	Message string       `json:"message"`
	SHA     string       `json:"sha,omitempty"`
	Status  string       `json:"status"` // "committed" or "failed"
	Files   []FileResult `json:"files"`
	Error   string       `json:"error,omitempty"`
	Hint    string       `json:"hint,omitempty"`
}

// FileResult is the output for a single file within a commit.
type FileResult struct {
	Path     string `json:"path"`
	Strategy string `json:"strategy"` // "full" or "hunks"
	Hunks    []int  `json:"hunks,omitempty"`
}

// DryRunResult is the JSON output for hc run --dry-run.
type DryRunResult struct {
	Valid         bool          `json:"valid"`
	Commits       int           `json:"commits"`
	Files         int           `json:"files"`
	HunksTotal    int           `json:"hunks_total"`
	HunksAssigned int           `json:"hunks_assigned"`
	Warnings      []string      `json:"warnings,omitempty"`
	Issues        []DryRunIssue `json:"issues"`
}

// DryRunIssue describes a validation issue in dry-run output.
type DryRunIssue struct {
	Type    string `json:"type"`
	File    string `json:"file,omitempty"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

// RewriteResult is the top-level output for hc rewrite.
type RewriteResult struct {
	Branch    string `json:"branch"`
	OldHead   string `json:"old_head"`
	NewHead   string `json:"new_head"`
	BackupRef string `json:"backup_ref,omitempty"`
	// TreeIdentical is always true on success: the rebuilt head's tree is
	// byte-for-byte the old head's tree, so build/test results are
	// guaranteed unchanged -- no re-verification needed.
	TreeIdentical bool             `json:"tree_identical"`
	Summary       RewriteSummary   `json:"summary"`
	Rewrites      []RewriteMapping `json:"rewrites,omitempty"`
	DryRun        bool             `json:"dry_run,omitempty"`
}

// RewriteSummary counts what the rewrite did within the rebuilt range.
type RewriteSummary struct {
	// Split is the number of original commits that were split.
	Split int `json:"split"`
	// Replacements is the total number of commits the splits produced.
	Replacements int `json:"replacements"`
	// Kept is the number of untouched commits re-parented as-is.
	Kept int `json:"kept"`
	// TotalAfter is the rebuilt range's commit count (kept + replacements).
	TotalAfter int `json:"total_after"`
}

// RewriteMapping maps one original commit to its replacements.
type RewriteMapping struct {
	Commit       string           `json:"commit"`
	Replacements []RewrittenEntry `json:"replacements"`
}

// RewrittenEntry is one replacement commit.
type RewrittenEntry struct {
	SHA     string `json:"sha"`
	Message string `json:"message"`
}

// ACError is a structured error with an exit code and hint.
type ACError struct {
	Message string `json:"error"`
	Code    int    `json:"code"`
	Hint    string `json:"hint"`
}

func (e *ACError) Error() string {
	return e.Message
}

// NewValidationError creates a validation error (exit 2).
func NewValidationError(message, hint string) *ACError {
	return &ACError{Message: message, Code: 2, Hint: hint}
}

// NewExecutionError creates an execution error (exit 3).
func NewExecutionError(message, hint string) *ACError {
	return &ACError{Message: message, Code: 3, Hint: hint}
}

// Printer handles TTY vs JSON output.
type Printer struct {
	Out       io.Writer
	ErrOut    io.Writer
	IsTTY     bool
	ForceJSON bool
	Quiet     bool
	NoColor   bool
}

// NewPrinter creates a printer with auto-detected TTY mode.
func NewPrinter() *Printer {
	isTTY := isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
	return &Printer{
		Out:    os.Stdout,
		ErrOut: os.Stderr,
		IsTTY:  isTTY,
	}
}

// UseJSON returns true if output should be JSON.
func (p *Printer) UseJSON() bool {
	return p.ForceJSON || !p.IsTTY
}

// PrintJSON outputs a value as JSON: pretty-printed on a TTY for humans,
// compact otherwise -- agents parse it anyway and indentation only costs
// tokens.
func (p *Printer) PrintJSON(v any) error {
	enc := json.NewEncoder(p.Out)
	if p.IsTTY {
		enc.SetIndent("", "  ")
	}
	return enc.Encode(v)
}

// PrintError outputs a structured error.
func (p *Printer) PrintError(err *ACError) {
	if p.UseJSON() {
		p.PrintJSON(err)
		return
	}
	if !p.NoColor {
		color.New(color.FgRed, color.Bold).Fprintf(p.ErrOut, "error: ")
	} else {
		fmt.Fprint(p.ErrOut, "error: ")
	}
	fmt.Fprintln(p.ErrOut, err.Message)
	if err.Hint != "" {
		fmt.Fprintln(p.ErrOut, "hint:", err.Hint)
	}
}

// Info prints an informational message (suppressed by --quiet).
func (p *Printer) Info(format string, args ...any) {
	if p.Quiet {
		return
	}
	fmt.Fprintf(p.Out, format+"\n", args...)
}
