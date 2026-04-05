package diff

// FileDiff represents a single file's diff from git diff -U0 output.
type FileDiff struct {
	Path      string
	Hunks     []Hunk
	IsBinary  bool
	IsNew     bool
	IsDeleted bool
	IsRenamed bool
	OldPath   string // only if renamed
	OldMode   string // e.g., "100644"
	NewMode   string // e.g., "100755"
}

// Hunk represents a single @@ block in a diff.
type Hunk struct {
	Index       int
	OldStart    int64
	OldCount    int64
	NewStart    int64
	NewCount    int64
	Lines       []Line
	Fingerprint string
}

// LineOp represents a diff line operation.
type LineOp int

const (
	OpContext LineOp = iota
	OpAdd
	OpDelete
)

// Line represents a single line in a diff hunk.
type Line struct {
	Op      LineOp
	Content string
}
