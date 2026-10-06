package app

import (
	"context"
	"errors"
	"fmt"

	"git-ui/internal/ai/prompts"
	"git-ui/internal/ai/reposettings"
	"git-ui/internal/ai/settings"
	"git-ui/internal/repos"
)

// ErrAIOff is what every AI action answers in a repository whose AI is off.
var ErrAIOff = errors.New("AI is off for this repository")

// commitRun is a running commit message generation. A run removes only its
// own entry when it ends, as a newer generation may have replaced it.
type commitRun struct{ cancel context.CancelFunc }

func (a *App) repoAIStore() *reposettings.Store {
	a.ai.mu.Lock()
	defer a.ai.mu.Unlock()
	if a.ai.repoAI == nil {
		a.ai.repoAI = reposettings.New(reposettings.PathNextTo(a.ai.deps.SettingsPath))
	}
	return a.ai.repoAI
}

// settingsKey is the repository whose private AI settings apply to id: a
// linked worktree uses its main repository's.
func (a *App) settingsKey(id string) string {
	a.wtMu.Lock()
	defer a.wtMu.Unlock()
	if parent, ok := a.wtParent[id]; ok {
		return parent
	}
	return id
}

// aiSettingsFor is the global settings with repoID's overrides on top. An
// unreadable repo-ai.json is an error, never the global settings: a
// repository that was off or limited to the local model must not silently
// use another provider.
func (a *App) aiSettingsFor(repoID string) (settings.Settings, reposettings.Override, error) {
	if a.ai == nil {
		return settings.Settings{}, reposettings.Override{}, ErrAIDisabled
	}
	g, err := settings.Load(a.ai.deps.SettingsPath)
	if err != nil {
		return settings.Settings{}, reposettings.Override{}, err
	}
	o, err := a.repoAIStore().Get(a.settingsKey(repoID))
	if err != nil {
		return settings.Settings{}, reposettings.Override{}, err
	}
	if o.AIOff {
		return settings.Settings{}, o, ErrAIOff
	}
	return reposettings.Merge(g, o), o, nil
}

// systemPrompt is the named prompt with extra (e.g. merge-kind guidance)
// and then the repository's approved and private instructions.
func (a *App) systemPrompt(repo repos.Repo, o reposettings.Override, name string, v prompts.Vars, extra string) (string, error) {
	base, err := a.ai.deps.Prompts.Get(name, v)
	if err != nil {
		return "", err
	}
	if extra != "" {
		base += "\n\n" + extra
	}
	ri := reposettings.ReadRepo(repo.Path)
	approved := reposettings.StateOf(o.Approval, ri.Hash) == reposettings.StateApproved
	return reposettings.Assemble(base, name, ri, approved, o.Instructions), nil
}

// repoNote marks an error caused by a repository override so the user
// knows where to fix it.
func repoNote(err error, overridden bool) error {
	if err == nil || !overridden {
		return err
	}
	return fmt.Errorf("%w (set in this repository's settings)", err)
}
