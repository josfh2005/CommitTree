package gitflow

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
)

type StartResult struct {
	Branch string   `json:"branch"`
	Notes  []string `json:"notes"`
}

// Start creates <prefix><name> from the type's base — develop for a feature
// or release, master for a hotfix, base (a local release) for a warmfix —
// after bringing that base up to date, checks it out and records the base
// the way SourceTree does.
func Start(ctx context.Context, dir, typ, name, base string) (StartResult, error) {
	res := StartResult{Notes: []string{}}
	f, err := Read(ctx, dir)
	if err != nil {
		return res, err
	}
	if !f.Initialized {
		return res, ErrNotInitialized
	}
	if f.Problem != "" {
		return res, errors.New(f.Problem)
	}
	prefix, ok := f.Prefixes.of(typ)
	if !ok {
		return res, fmt.Errorf("unknown git-flow type %q", typ)
	}
	name = strings.TrimSpace(name)
	branch := prefix + name
	if name == "" || strings.HasPrefix(name, "-") {
		return res, fmt.Errorf("%w: %q", ErrInvalidName, branch)
	}
	if _, err := git(ctx, dir, "check-ref-format", "--branch", branch); err != nil {
		return res, fmt.Errorf("%w: %q", ErrInvalidName, branch)
	}
	if branchExists(ctx, dir, branch) {
		return res, fmt.Errorf("%s already exists", branch)
	}
	switch typ {
	case Feature, Release:
		base = f.Develop
	case Hotfix:
		base = f.Master
	case Warmfix:
		if !slices.Contains(f.Releases, base) {
			return res, fmt.Errorf("%w: choose a release to start %s from", ErrNoRelease, branch)
		}
	}
	res.Notes = append(res.Notes, fetchNotes(ctx, dir)...)
	note, err := syncBranch(ctx, dir, base)
	if err != nil {
		return res, err
	}
	if note != "" {
		res.Notes = append(res.Notes, note)
	}
	if _, err := git(ctx, dir, "switch", "-c", branch, base); err != nil {
		return res, err
	}
	if _, err := git(ctx, dir, "config", "gitflow.branch."+branch+".base", base); err != nil {
		return res, err
	}
	res.Branch = branch
	return res, nil
}
