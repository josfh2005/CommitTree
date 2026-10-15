package app

import (
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"git-ui/internal/repos"
	"git-ui/internal/scanrepos"
)

// ScanCandidate is a repository found by ScanReposIn; Added when the
// sidebar already lists it.
type ScanCandidate struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Added bool   `json:"added"`
}

// ScanResult is scanrepos.Result with each candidate marked Added or not.
type ScanResult struct {
	Parent  string          `json:"parent"`
	Repos   []ScanCandidate `json:"repos"`
	Skipped int             `json:"skipped"`
}

// AddReposResult is what AddRepos added and what it could not; both never nil.
type AddReposResult struct {
	Added  []repos.Repo       `json:"added"`
	Failed []repos.AddFailure `json:"failed"`
}

// ScanReposIn lists the repositories directly inside parent (as typed: ~
// expanded, relative refused) for "Add from folder…". A candidate is Added
// when a stored entry has the same path once symlinks are resolved.
func (a *App) ScanReposIn(parent string) (ScanResult, error) {
	res, err := scanrepos.Scan(parent)
	if err != nil {
		return ScanResult{}, err
	}
	listed := map[string]bool{}
	for _, r := range a.store.List() {
		listed[canonical(r.Path)] = true
	}
	out := ScanResult{Parent: res.Parent, Repos: make([]ScanCandidate, 0, len(res.Repos)), Skipped: res.Skipped}
	for _, c := range res.Repos {
		out.Repos = append(out.Repos, ScanCandidate{Name: c.Name, Path: c.Path, Added: listed[canonical(c.Path)]})
	}
	return out, nil
}

// AddRepos adds paths to the list in group ("" for none) in one write. See
// repos.Store.AddMany; a failed write is the error and nothing is added.
func (a *App) AddRepos(paths []string, group string) (AddReposResult, error) {
	added, failed, err := a.store.AddMany(a.ctx, paths, group)
	if err != nil {
		return AddReposResult{}, err
	}
	return AddReposResult{Added: added, Failed: failed}, nil
}

// PickReposParent asks for the folder to scan, starting at start; "" when
// the user cancels.
func (a *App) PickReposParent(start string) (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Add repositories from folder", DefaultDirectory: start})
}

// DefaultReposParent is the folder offered before any scan: home.
func (a *App) DefaultReposParent() string {
	return a.DefaultCloneParent()
}
