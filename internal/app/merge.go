package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

// mergePaths lists every path this merge touches: the ones still unmerged,
// and the ones already staged into it. Paths come from git verbatim, so a
// caller's path is accepted only when it matches one exactly — a crafted
// pathspec such as ":(glob)*" can never equal one.
func mergePaths(ctx context.Context, dir string, st merge.State) (map[string]bool, error) {
	paths := map[string]bool{}
	for _, p := range st.Conflicts {
		paths[p] = true
	}
	for _, p := range st.Manual {
		paths[p] = true
	}
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "diff", "--cached", "--name-only", "-z", "HEAD")
	if err != nil {
		return nil, err
	}
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			paths[p] = true
		}
	}
	return paths, nil
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
	// Build the set of paths this merge touches; accept only exact matches.
	paths, err := mergePaths(a.ctx, dir, st)
	if err != nil {
		return ConflictFile{}, err
	}
	if !paths[path] {
		return ConflictFile{}, fmt.Errorf("%q is not part of this merge", path)
	}
	// Check if the path is in the Conflicts list.
	for _, p := range st.Conflicts {
		if p == path {
			data, err := os.ReadFile(filepath.Join(dir, path))
			if err != nil {
				return ConflictFile{}, err
			}
			return ConflictFile{Path: path, Text: string(data)}, nil
		}
	}
	// Check if the path is in the Manual list.
	for _, p := range st.Manual {
		if p == path {
			return ConflictFile{Path: path, Text: "This file has no conflict markers to edit here. Resolve it in your editor."}, nil
		}
	}
	// Otherwise it is staged into the merge: return the diff.
	out, err := gitcmd.Run(a.ctx, dir, gitcmd.ReadTimeout, "diff", "--cached", "--", path)
	if err != nil {
		return ConflictFile{}, err
	}
	return ConflictFile{Path: path, Resolved: true, Text: out}, nil
}
