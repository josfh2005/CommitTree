package app

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"git-ui/internal/ai/prompts"
	"git-ui/internal/ai/reposettings"
	"git-ui/internal/ai/settings"
	"git-ui/internal/repos"
)

// ErrAIOff is what every AI action answers in a repository whose AI is off.
var ErrAIOff = errors.New("AI is off for this repository")

// commitRun is a running commit message generation. A run removes only its
// own entry when it ends, as a newer generation may have replaced it.
type commitRun struct {
	cancel context.CancelFunc
	// superseded is set when a newer generation replaced this one, so its
	// cancellation is not reported as the AI having been turned off.
	superseded atomic.Bool
}

// cancelMessage is what a cancelled generation reports: the AI being turned
// off, unless a newer generation replaced it.
func (r *commitRun) cancelMessage() string {
	if r.superseded.Load() {
		return context.Canceled.Error()
	}
	return ErrAIOff.Error()
}

func (a *App) repoAIStore() *reposettings.Store {
	a.ai.mu.Lock()
	defer a.ai.mu.Unlock()
	if a.ai.repoAI == nil {
		a.ai.repoAI = reposettings.New(reposettings.PathNextTo(a.ai.deps.SettingsPath))
	}
	return a.ai.repoAI
}

// settingsKey is the repository whose private AI settings apply to id: a
// linked worktree, detected or added to the list, uses its main
// repository's (as of the last ListRepos).
func (a *App) settingsKey(id string) string {
	a.wtMu.Lock()
	defer a.wtMu.Unlock()
	if parent, ok := a.settingsParent[id]; ok {
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

// EventRepoAIChanged tells the frontend a repository's AI settings or the
// approval of its instructions changed.
const EventRepoAIChanged = "repo-ai:changed"

type RepoAIChangedEvent struct {
	RepoID string `json:"repoID"`
}

var ErrInstructionsChanged = errors.New("the repository's instructions changed; review them again")

type RepoAIInfo struct {
	Effective        settings.Settings             `json:"effective"`
	Global           settings.Settings             `json:"global"`
	Overrides        reposettings.Override         `json:"overrides"`
	AIOff            bool                          `json:"aiOff"`
	RepoInstructions reposettings.RepoInstructions `json:"repoInstructions"`
	State            string                        `json:"state"`
	Error            string                        `json:"error,omitempty"`
}

// GetRepoAISettings is what the AI tab and the per-repository stores show.
// An unreadable repo-ai.json is reported in Error with AIOff set, so the
// interface hides the AI rather than offer actions that will be refused.
func (a *App) GetRepoAISettings(repoID string) (RepoAIInfo, error) {
	if a.ai == nil {
		return RepoAIInfo{}, ErrAIDisabled
	}
	repo, ok := a.repo(repoID)
	if !ok {
		return RepoAIInfo{}, repos.ErrUnknownRepo
	}
	g, err := settings.Load(a.ai.deps.SettingsPath)
	if err != nil {
		return RepoAIInfo{}, err
	}
	info := RepoAIInfo{Global: g, Effective: g, RepoInstructions: reposettings.ReadRepo(repo.Path)}
	o, err := a.repoAIStore().Get(a.settingsKey(repoID))
	if err != nil {
		info.AIOff, info.Error = true, err.Error()
		info.State = reposettings.StateNone
		return info, nil
	}
	if o.Instructions == nil {
		o.Instructions = map[string]string{}
	}
	info.Overrides, info.AIOff = o, o.AIOff
	info.Effective = reposettings.Merge(g, o)
	info.State = reposettings.StateOf(o.Approval, info.RepoInstructions.Hash)
	return info, nil
}

// SaveRepoAISettings replaces the repository's overrides. Turning the AI
// off stops what is running there.
func (a *App) SaveRepoAISettings(repoID string, o reposettings.Override) error {
	if a.ai == nil {
		return ErrAIDisabled
	}
	if _, ok := a.repo(repoID); !ok {
		return repos.ErrUnknownRepo
	}
	if err := a.repoAIStore().Set(a.settingsKey(repoID), o); err != nil {
		return err
	}
	if o.AIOff {
		a.stopRepoAI(repoID)
	}
	a.emit(EventRepoAIChanged, RepoAIChangedEvent{RepoID: repoID})
	return nil
}

func (a *App) ApproveRepoInstructions(repoID, hash string) error {
	return a.setApproval(repoID, hash, hash)
}

func (a *App) IgnoreRepoInstructions(repoID, hash string) error {
	return a.setApproval(repoID, hash, reposettings.IgnoredPrefix+hash)
}

// setApproval stores value only if the files still hash to seen, the hash
// the user was shown: an edit while the tab was open is never approved.
func (a *App) setApproval(repoID, seen, value string) error {
	if a.ai == nil {
		return ErrAIDisabled
	}
	repo, ok := a.repo(repoID)
	if !ok {
		return repos.ErrUnknownRepo
	}
	if seen == "" || reposettings.ReadRepo(repo.Path).Hash != seen {
		return ErrInstructionsChanged
	}
	if err := a.repoAIStore().SetApproval(a.settingsKey(repoID), value); err != nil {
		return err
	}
	a.emit(EventRepoAIChanged, RepoAIChangedEvent{RepoID: repoID})
	return nil
}

// stopRepoAI cancels the chat answer, explanation or conflict resolution
// (they all hold the chat slot), the commit message and the pending
// suggestions of the repository whose settings repoID uses, and of every
// linked worktree sharing them.
func (a *App) stopRepoAI(repoID string) {
	key := a.settingsKey(repoID)
	ids := []string{key}
	a.wtMu.Lock()
	for wt, parent := range a.settingsParent {
		if parent == key {
			ids = append(ids, wt)
		}
	}
	a.wtMu.Unlock()
	if repoID != key {
		ids = append(ids, repoID)
	}
	for _, id := range ids {
		_ = a.StopChat(id)
		a.cancelSuggestions(id)
		a.ai.mu.Lock()
		if run, ok := a.ai.commits[id]; ok {
			run.cancel()
		}
		a.ai.mu.Unlock()
	}
}
