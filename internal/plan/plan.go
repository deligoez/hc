package plan

// Plan is the top-level commit plan structure.
type Plan struct {
	Commits        []Commit `json:"commits"`
	AllowUnplanned []string `json:"allow_unplanned,omitempty"`
}

// Commit represents a single commit in the plan.
type Commit struct {
	Message string      `json:"message"`
	Files   []FileEntry `json:"files"`
}

// FileEntry represents a file to include in a commit.
type FileEntry struct {
	Path  string `json:"path"`
	Hunks []int  `json:"hunks,omitempty"` // nil or empty = full file
}

// IsFullFile returns true if the file should be staged entirely.
func (f FileEntry) IsFullFile() bool {
	return len(f.Hunks) == 0
}
