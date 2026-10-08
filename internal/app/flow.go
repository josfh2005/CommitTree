package app

import (
	"context"

	"git-ui/internal/gitflow"
)

// GetFlow reads the repository's git-flow config and branches.
func (a *App) GetFlow(id string) (gitflow.Flow, error) {
	dir, err := a.dir(id)
	if err != nil {
		return gitflow.Flow{}, err
	}
	return gitflow.Read(a.ctx, dir)
}

func (a *App) InitFlow(id string, cfg gitflow.Config) error {
	return a.write(id, func(ctx context.Context, dir string) error { return gitflow.Init(ctx, dir, cfg) })
}

// GetFlowSettings is the Git-flow tab of Repository settings
// (docs/spec/12-repository-settings.md): the branch roles, or the defaults
// when git-flow is not set up.
func (a *App) GetFlowSettings(id string) (gitflow.Settings, error) {
	dir, err := a.dir(id)
	if err != nil {
		return gitflow.Settings{}, err
	}
	return gitflow.ReadSettings(a.ctx, dir)
}

// SaveFlowSettings rewrites the gitflow.* keys of a repository that is set
// up; it creates no branch. Setting up goes through InitFlow.
func (a *App) SaveFlowSettings(id string, cfg gitflow.Config) error {
	return a.write(id, func(ctx context.Context, dir string) error { return gitflow.SaveSettings(ctx, dir, cfg) })
}

func (a *App) StartFlow(id, typ, name, base string) (gitflow.StartResult, error) {
	var res gitflow.StartResult
	err := a.write(id, func(ctx context.Context, dir string) (err error) {
		res, err = gitflow.Start(ctx, dir, typ, name, base)
		return err
	})
	return res, err
}

// PlanFinish is read-only: what FinishFlow would do, for its dialog.
func (a *App) PlanFinish(id, branch string, releases []string) (gitflow.Plan, error) {
	dir, err := a.dir(id)
	if err != nil {
		return gitflow.Plan{}, err
	}
	return gitflow.PlanFinish(a.ctx, dir, branch, releases)
}

// FinishFlow merges branch into its targets; a conflicted result leaves the
// merge in progress for the Merge view, like MergeBranch.
func (a *App) FinishFlow(id, branch string, releases []string) (gitflow.FinishResult, error) {
	var res gitflow.FinishResult
	err := a.write(id, func(ctx context.Context, dir string) (err error) {
		res, err = gitflow.Finish(ctx, dir, branch, releases)
		return err
	})
	return res, err
}
