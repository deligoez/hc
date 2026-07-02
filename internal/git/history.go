package git

import (
	"fmt"
	"strings"
)

// CommitInfo describes one commit for history rewriting.
type CommitInfo struct {
	SHA        string
	Tree       string
	Parents    []string
	AuthorName string
	AuthorMail string
	AuthorDate string // ISO-8601
	Message    string // full raw message
}

// ReadCommit loads commit metadata.
func (r *Runner) ReadCommit(rev string) (*CommitInfo, error) {
	out, err := r.Run("show", "-s", "--format=%H%x00%T%x00%P%x00%an%x00%ae%x00%aI%x00%B", rev)
	if err != nil {
		return nil, err
	}
	parts := strings.SplitN(out, "\x00", 7)
	if len(parts) != 7 {
		return nil, fmt.Errorf("unexpected git show output for %s", rev)
	}
	ci := &CommitInfo{
		SHA:        strings.TrimSpace(parts[0]),
		Tree:       strings.TrimSpace(parts[1]),
		AuthorName: parts[3],
		AuthorMail: parts[4],
		AuthorDate: strings.TrimSpace(parts[5]),
		Message:    strings.TrimRight(parts[6], "\n"),
	}
	if p := strings.TrimSpace(parts[2]); p != "" {
		ci.Parents = strings.Fields(p)
	}
	return ci, nil
}

// ResolveSHA resolves rev to a full commit SHA.
func (r *Runner) ResolveSHA(rev string) (string, error) {
	out, err := r.Run("rev-parse", "--verify", rev+"^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// FirstParentChain lists commit SHAs from HEAD following first parents,
// newest first, up to limit (0 = no limit).
func (r *Runner) FirstParentChain(head string, limit int) ([]string, error) {
	args := []string{"rev-list", "--first-parent"}
	if limit > 0 {
		args = append(args, fmt.Sprintf("--max-count=%d", limit))
	}
	args = append(args, head)
	out, err := r.Run(args...)
	if err != nil {
		return nil, err
	}
	return strings.Fields(out), nil
}

// DiffCommit returns the -U0 diff a commit introduces over its first parent.
func (r *Runner) DiffCommit(sha string) (string, error) {
	return r.Run("diff", "-U0", "--no-renames", "--no-ext-diff", sha+"^", sha)
}

// BlobAt returns the content of path at rev ("" content and ok=false when the
// path does not exist there).
func (r *Runner) BlobAt(rev, path string) ([]byte, bool, error) {
	out, err := r.Run("show", rev+":"+path)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") ||
			strings.Contains(err.Error(), "exists on disk") ||
			strings.Contains(err.Error(), "not in") {
			return nil, false, nil
		}
		return nil, false, err
	}
	return []byte(out), true, nil
}

// TreeEntry returns (mode, blobSHA, true) for path in rev's tree, or ok=false
// when the path is absent.
func (r *Runner) TreeEntry(rev, path string) (string, string, bool, error) {
	out, err := r.Run("ls-tree", rev, "--", path)
	if err != nil {
		return "", "", false, err
	}
	fields := strings.Fields(out)
	if len(fields) < 3 {
		return "", "", false, nil
	}
	return fields[0], fields[2], true, nil
}

// ReadTree resets the runner's index to the given tree.
func (r *Runner) ReadTree(tree string) error {
	_, err := r.Run("read-tree", tree)
	return err
}

// WriteTree writes the runner's index as a tree object and returns its SHA.
func (r *Runner) WriteTree() (string, error) {
	out, err := r.Run("write-tree")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// CommitTree creates a commit object for tree with the given parents and
// message, preserving the original author identity/date. It returns the new
// commit SHA. The committer stays the current user (rebase semantics).
// Multiple parents re-create merge commits (their extra parents are kept
// verbatim when a merge is re-parented across a rewrite).
func (r *Runner) CommitTree(tree string, parents []string, message string, author *CommitInfo) (string, error) {
	saved := r.Env
	defer func() { r.Env = saved }()
	r.Env = append(append([]string{}, saved...),
		"GIT_AUTHOR_NAME="+author.AuthorName,
		"GIT_AUTHOR_EMAIL="+author.AuthorMail,
		"GIT_AUTHOR_DATE="+author.AuthorDate,
	)
	args := []string{"commit-tree", tree}
	for _, p := range parents {
		args = append(args, "-p", p)
	}
	args = append(args, "-F", "-")
	out, err := r.RunWithStdin([]byte(message), args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// IsAncestor reports whether ancestor is reachable from descendant.
func (r *Runner) IsAncestor(ancestor, descendant string) (bool, error) {
	_, err := r.Run("merge-base", "--is-ancestor", ancestor, descendant)
	if err == nil {
		return true, nil
	}
	// merge-base --is-ancestor exits 1 for "no"; other failures bubble up.
	if strings.Contains(err.Error(), "merge-base") && strings.TrimSpace(err.Error()) != "" &&
		!strings.Contains(err.Error(), "fatal") {
		return false, nil
	}
	return false, err
}

// UpdateRef points ref at sha, recording oldSHA as the expected previous
// value when non-empty (compare-and-swap safety).
func (r *Runner) UpdateRef(ref, sha, oldSHA string) error {
	args := []string{"update-ref", ref, sha}
	if oldSHA != "" {
		args = append(args, oldSHA)
	}
	_, err := r.Run(args...)
	return err
}

// RemoteBranchesContaining lists remote branches that already contain sha.
func (r *Runner) RemoteBranchesContaining(sha string) ([]string, error) {
	out, err := r.Run("branch", "-r", "--contains", sha)
	if err != nil {
		return nil, err
	}
	var branches []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "* "))
		if line != "" {
			branches = append(branches, line)
		}
	}
	return branches, nil
}

// CurrentBranch returns the checked-out branch name, or "" when detached.
func (r *Runner) CurrentBranch() (string, error) {
	out, err := r.Run("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(out)
	if name == "HEAD" {
		return "", nil
	}
	return name, nil
}
