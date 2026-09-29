package gitflow

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"git-ui/internal/merge"
	"git-ui/internal/ops"
)

var (
	ErrDirty      = errors.New("commit or stash your changes first")
	ErrInProgress = errors.New("finish the conflict in progress first")
)

type Step struct {
	Target string `json:"target"`
	Done   bool   `json:"done"`
}

type Plan struct {
	Branch string `json:"branch"`
	Type   string `json:"type"`
	Steps  []Step `json:"steps"`
	Ending string `json:"ending"`
}

type FinishResult struct {
	Outcome   string   `json:"outcome"` // "finished" | "conflicted"
	Target    string   `json:"target"`
	Conflicts []string `json:"conflicts"`
	Merged    []string `json:"merged"`
	Notes     []string `json:"notes"`
}

func lookup(ctx context.Context, dir, branch string) (Flow, FlowBranch, error) {
	f, err := Read(ctx, dir)
	if err != nil {
		return f, FlowBranch{}, err
	}
	if !f.Initialized {
		return f, FlowBranch{}, ErrNotInitialized
	}
	if f.Problem != "" {
		return f, FlowBranch{}, errors.New(f.Problem)
	}
	for _, b := range f.Branches {
		if strings.EqualFold(b.Name, branch) {
			return f, b, nil
		}
	}
	return f, FlowBranch{}, fmt.Errorf("%w: %s", ErrNotFlowBranch, branch)
}

// PlanFinish says, without changing anything, where Finish would merge
// branch and which of those targets already have it.
func PlanFinish(ctx context.Context, dir, branch string, releases []string) (Plan, error) {
	f, b, err := lookup(ctx, dir, branch)
	if err != nil {
		return Plan{}, err
	}
	ts, err := targets(f, b, releases)
	if err != nil {
		return Plan{}, err
	}
	p := Plan{Branch: branch, Type: b.Type, Steps: []Step{}, Ending: ending(f, b, ts)}
	for _, t := range ts {
		done, err := isAncestor(ctx, dir, branch, t)
		if err != nil {
			return Plan{}, err
		}
		p.Steps = append(p.Steps, Step{Target: t, Done: done})
	}
	return p, nil
}

// Finish merges branch into each target that does not contain it yet, in
// order, then checks out the ending branch and deletes branch. A conflict
// stops it on that target with the merge in progress; once the merge is
// committed, calling Finish again carries on from the next target.
func Finish(ctx context.Context, dir, branch string, releases []string) (FinishResult, error) {
	res := FinishResult{Conflicts: []string{}, Merged: []string{}, Notes: []string{}}
	fail := func(err error) (FinishResult, error) {
		if len(res.Merged) > 0 {
			err = fmt.Errorf("%w (already merged into %s; finish again to continue)", err, strings.Join(res.Merged, ", "))
		}
		return res, err
	}
	if st, err := merge.Status(ctx, dir); err != nil {
		return res, err
	} else if st.Kind != "" {
		return res, ErrInProgress
	}
	if out, err := git(ctx, dir, "status", "--porcelain", "--untracked-files=no"); err != nil {
		return res, err
	} else if out != "" {
		return res, ErrDirty
	}
	f, b, err := lookup(ctx, dir, branch)
	if err != nil {
		return res, err
	}
	ts, err := targets(f, b, releases)
	if err != nil {
		return res, err
	}
	branch = b.Name
	res.Notes = append(res.Notes, fetchNotes(ctx, dir)...)
	// Everything that can stop the finish is checked before anything
	// moves: a branch busy in another worktree, a diverged target.
	elsewhere, err := worktreeBranches(ctx, dir)
	if err != nil {
		return res, err
	}
	if path, ok := elsewhere[branch]; ok {
		return res, fmt.Errorf("%s is checked out in another worktree (%s); switch away from it there first", branch, path)
	}
	var pending []string
	for _, t := range ts {
		done, err := isAncestor(ctx, dir, branch, t)
		if err != nil {
			return res, err
		}
		if done {
			continue
		}
		if path, ok := elsewhere[t]; ok {
			return res, fmt.Errorf("%s is checked out in another worktree (%s); switch away from it there first", t, path)
		}
		if err := checkBranch(ctx, dir, t); err != nil {
			return res, err
		}
		pending = append(pending, t)
	}
	for _, t := range pending {
		note, err := syncBranch(ctx, dir, t)
		if err != nil {
			return res, err
		}
		if note != "" {
			res.Notes = append(res.Notes, note)
		}
	}
	for _, t := range ts {
		done, err := isAncestor(ctx, dir, branch, t)
		if err != nil {
			return fail(err)
		}
		if done {
			continue
		}
		if err := ops.Checkout(ctx, dir, t); err != nil {
			return fail(err)
		}
		m, err := merge.Start(ctx, dir, branch)
		if err != nil {
			return fail(err)
		}
		if m.Outcome == merge.Conflicted {
			res.Outcome, res.Target = "conflicted", t
			if m.Conflicts != nil {
				res.Conflicts = m.Conflicts
			}
			return res, nil
		}
		res.Merged = append(res.Merged, t)
	}
	if err := ops.Checkout(ctx, dir, ending(f, b, ts)); err != nil {
		return fail(err)
	}
	// -D, not -d: -d compares against the branch's own upstream, which a
	// finished branch may not have caught up with. This check is what makes
	// the force safe.
	for _, t := range ts {
		if done, err := isAncestor(ctx, dir, branch, t); err != nil || !done {
			return fail(fmt.Errorf("%s is not in %s; not deleting it", branch, t))
		}
	}
	if _, err := git(ctx, dir, "branch", "-D", branch); err != nil {
		return fail(err)
	}
	if b.baseKey != "" {
		if _, err := git(ctx, dir, "config", "--unset", b.baseKey); err != nil {
			return fail(err)
		}
	}
	res.Outcome = "finished"
	return res, nil
}

// worktreeBranches maps each branch checked out in another worktree of the
// repository to that worktree's path.
func worktreeBranches(ctx context.Context, dir string) (map[string]string, error) {
	top, err := git(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	out, err := git(ctx, dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	here := realPath(top)
	busy := map[string]string{}
	path := ""
	for _, line := range strings.Split(out, "\n") {
		if p, ok := strings.CutPrefix(line, "worktree "); ok {
			path = p
		} else if ref, ok := strings.CutPrefix(line, "branch refs/heads/"); ok && realPath(path) != here {
			busy[ref] = path
		}
	}
	return busy, nil
}

func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}
