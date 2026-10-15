// Package scanrepos finds the git repositories directly inside a folder, for
// adding several to the sidebar at once. It only looks at the file system
// (no git), so it is instant even for a folder with hundreds of entries.
package scanrepos

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"git-ui/internal/clone"
)

// Candidate is a direct child of the scanned folder that is a repository.
type Candidate struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// Result is what Scan found. Repos is sorted by name (case-insensitive) and
// never nil. Skipped counts the non-hidden children that are not candidates.
type Result struct {
	Parent  string      `json:"parent"`
	Repos   []Candidate `json:"repos"`
	Skipped int         `json:"skipped"`
}

var (
	ErrParentMissing    = errors.New("That folder doesn't exist.")
	ErrParentNotDir     = errors.New("That path isn't a folder.")
	ErrParentUnreadable = errors.New("That folder can't be read")
)

// Scan lists the repositories among parent's direct children. parent is
// taken as typed: trimmed, with a leading ~ expanded (clone.ExpandParent);
// a relative path is refused. A child is a candidate when its name does not
// start with ".", it is a folder (or a symlink to one) and its .git is a
// folder; a .git file (linked worktree, submodule checkout) does not count.
// The parent itself is never a candidate.
func Scan(parent string) (Result, error) {
	dir, err := clone.ExpandParent(parent)
	if err != nil {
		return Result{}, err
	}
	info, err := os.Stat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Result{}, ErrParentMissing
	case err != nil:
		return Result{}, fmt.Errorf("%w: %v", ErrParentUnreadable, err)
	case !info.IsDir():
		return Result{}, ErrParentNotDir
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrParentUnreadable, err)
	}
	res := Result{Parent: dir, Repos: []Candidate{}}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		path := filepath.Join(dir, name)
		if isRepo(path) {
			res.Repos = append(res.Repos, Candidate{Name: name, Path: path})
		} else {
			res.Skipped++
		}
	}
	sort.SliceStable(res.Repos, func(i, j int) bool {
		a, b := strings.ToLower(res.Repos[i].Name), strings.ToLower(res.Repos[j].Name)
		if a != b {
			return a < b
		}
		return res.Repos[i].Name < res.Repos[j].Name
	})
	return res, nil
}

// isRepo: path is a folder (symlinks followed) whose .git is a folder.
func isRepo(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return false
	}
	git, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil && git.IsDir()
}
