// Package config loads optional repo-level hc configuration from .hc.json
// at the repository root.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/deligoez/hc/internal/output"
)

// FileName is the repo-root config file hc looks for.
const FileName = ".hc.json"

// Config is the root of .hc.json.
type Config struct {
	Commit CommitConfig `json:"commit"`
}

// CommitConfig controls commit message handling.
type CommitConfig struct {
	// Prefix is prepended to every commit message in a plan (unless the
	// message already starts with it). The literal "${ticket}" is replaced
	// with the first match of TicketFromBranch against the current branch
	// name; if the pattern does not match, prefixing is skipped with a
	// warning. Example: "${ticket}: " on branch feature/WB-1234-login with
	// ticket_from_branch "[A-Z]+-\\d+" turns "feat(auth): add login" into
	// "WB-1234: feat(auth): add login".
	Prefix string `json:"prefix,omitempty"`
	// TicketFromBranch is a regular expression whose first match against the
	// current branch name replaces "${ticket}" in Prefix.
	TicketFromBranch string `json:"ticket_from_branch,omitempty"`
}

// Load reads .hc.json from dir. A missing file returns (nil, nil): all
// configuration is optional. A malformed file is a validation error so the
// plan is rejected before any git state changes.
func Load(dir string) (*Config, error) {
	if dir == "" {
		dir = "."
	}
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, output.NewValidationError(
			fmt.Sprintf("cannot read %s: %v", FileName, err),
			"Fix the file permissions or remove the file.",
		)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, output.NewValidationError(
			fmt.Sprintf("%s parse error: %v", FileName, err),
			"Fix the JSON syntax in .hc.json or remove the file.",
		)
	}
	return &cfg, nil
}
