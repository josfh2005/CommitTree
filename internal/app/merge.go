package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"git-ui/internal/gitcmd"
	"git-ui/internal/merge"
)

// ConflictFile is one file of a merge as the UI shows it: the raw content
// with markers while it is conflicted, and the staged diff once it is not.
type ConflictFile struct {
	Path     string `json:"path"`
	Resolved bool   `json:"resolved"`
	Text     string `json:"text"`
}

// MergeBranch merges branch into the repository's current branch. A
// conflicted merge is left in place for the user or the agent to resolve.
func (a *App) MergeBranch(id, branch string) (merge.Result, error) {
	var result merge.Result
	err := a.write(id, func(ctx context.Context, dir string) error {
		var err error
		result, err = merge.Start(ctx, dir, branch)
		return err
	})
	return result, err
}

func (a *App) GetMergeState(id string) (merge.State, error) {
	dir, err := a.dir(id)
	if err != nil {
		return merge.State{}, err
	}
	return merge.Status(a.ctx, dir)
}

func (a *App) AbortMerge(id string) error {
	return a.write(id, func(ctx context.Context, dir string) error { return merge.Abort(ctx, dir) })
}

func (a *App) CommitMerge(id string) error {
	return a.write(id, func(ctx context.Context, dir string) error { return merge.Commit(ctx, dir) })
}

// GetConflictFile returns what the merge view shows for one file. Only files
// belonging to the merge in progress can be read, so a path from the
// renderer can't be used to read the disk.
func (a *App) GetConflictFile(id, path string) (ConflictFile, error) {
	dir, err := a.dir(id)
	if err != nil {
		return ConflictFile{}, err
	}
	st, err := merge.Status(a.ctx, dir)
	if err != nil {
		return ConflictFile{}, err
	}
	conflicted, known := false, false
	for _, p := range st.Conflicts {
		if p == path {
			conflicted, known = true, true
		}
	}
	for _, p := range st.Manual {
		if p == path {
			known = true
		}
	}
	if !known {
		// Not unmerged any more: it was resolved and staged during this
		// merge, so show the staged diff. Anything git doesn't know is
		// refused below.
		out, err := gitcmd.Run(a.ctx, dir, gitcmd.ReadTimeout, "diff", "--cached", "--", path)
		if err != nil {
			return ConflictFile{}, err
		}
		if out == "" {
			return ConflictFile{}, fmt.Errorf("%q is not part of this merge", path)
		}
		return ConflictFile{Path: path, Resolved: true, Text: out}, nil
	}
	if !conflicted {
		return ConflictFile{Path: path, Text: "This file has no conflict markers to edit here. Resolve it in your editor."}, nil
	}
	data, err := os.ReadFile(filepath.Join(dir, path))
	if err != nil {
		return ConflictFile{}, err
	}
	return ConflictFile{Path: path, Text: string(data)}, nil
}
