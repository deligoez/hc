package git

import (
	"fmt"
	"strings"
)

// HashObjectWrite stores data as a blob in the object database and returns
// its full SHA. The object is written unfiltered: callers pass repo-side
// (already clean-filtered) content reconstructed from index blobs and diff
// lines, so no worktree conversion must be applied.
func (r *Runner) HashObjectWrite(data []byte) (string, error) {
	out, err := r.RunWithStdin(data, "hash-object", "-w", "--stdin")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// HashWorktreeFile hashes the working-tree file as git add would store it
// (clean filters applied), without writing the object.
func (r *Runner) HashWorktreeFile(path string) (string, error) {
	out, err := r.Run("hash-object", "--", path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// StageBlob points the index entry for path at the given blob and mode.
func (r *Runner) StageBlob(mode, sha, path string) error {
	_, err := r.Run("update-index", "--add", "--cacheinfo", fmt.Sprintf("%s,%s,%s", mode, sha, path))
	return err
}

// RemoveFromIndex stages the deletion of path.
func (r *Runner) RemoveFromIndex(path string) error {
	_, err := r.Run("update-index", "--force-remove", "--", path)
	return err
}

// IndexEntryMode returns the mode of the stage-0 index entry for path
// (e.g. "100644"), or "" if the path has no index entry.
func (r *Runner) IndexEntryMode(path string) (string, error) {
	out, err := r.Run("ls-files", "-s", "--", path)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return "", nil
	}
	return fields[0], nil
}

// IndexBlob returns the stage-0 index content of path.
func (r *Runner) IndexBlob(path string) ([]byte, error) {
	out, err := r.Run("show", ":"+path)
	if err != nil {
		return nil, err
	}
	return []byte(out), nil
}

// BaseBlob returns the content of path at ref, or the index content when ref
// is empty.
//
// A path absent from ref reads as empty rather than as an error: that is a
// file the plan creates, and its base really is nothing -- the same content
// the empty intent-to-add index blob would have given. A genuinely broken
// object store lands in the same branch, but it cannot pass unnoticed: the
// caller then verifies base + every hunk against the working tree.
func (r *Runner) BaseBlob(ref, path string) ([]byte, error) {
	if ref == "" {
		return r.IndexBlob(path)
	}
	spec := ref + ":" + path
	if _, err := r.Run("cat-file", "-e", spec); err != nil {
		return []byte{}, nil
	}
	out, err := r.Run("show", spec)
	if err != nil {
		return nil, err
	}
	return []byte(out), nil
}
