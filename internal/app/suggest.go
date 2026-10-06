package app

import (
	"context"
	"fmt"
	"time"

	"git-ui/internal/ai/agent"
	"git-ui/internal/ai/prompts"
	"git-ui/internal/ai/reposettings"
	"git-ui/internal/ai/settings"
	"git-ui/internal/ai/tasks"
	"git-ui/internal/refs"
)

// DefaultSuggestDelay is how long after an answer suggested replies wait, so
// a reply sent straight away cancels them before any call is made.
const DefaultSuggestDelay = 5 * time.Second

const suggestTimeout = 30 * time.Second

type pendingSuggestion struct{ cancel context.CancelFunc }

// suggestAllowed applies the suggested replies mode to the task provider,
// the one that makes the call: auto-local only spends a free, local model.
func suggestAllowed(mode, taskProvider string) bool {
	switch mode {
	case settings.SuggestAuto:
		return true
	case settings.SuggestAutoLocal:
		return taskProvider == settings.ProviderOllama
	}
	return false
}

// cancelSuggestions drops the repository's pending suggested replies, before
// their call or during it; a cancelled one emits nothing.
func (a *App) cancelSuggestions(repoID string) {
	a.ai.mu.Lock()
	defer a.ai.mu.Unlock()
	if p, ok := a.ai.suggestions[repoID]; ok {
		p.cancel()
		delete(a.ai.suggestions, repoID)
	}
}

// suggestReplies schedules suggested replies for the answer runID just
// finished, when the mode allows it: after the delay it asks the task model
// and emits chat:suggestions. It never takes the chat slot. Callers call it
// only for an answer that ended without any error (not stopped).
func (a *App) suggestReplies(repoID, runID string, cfg settings.Settings, o reposettings.Override) {
	if !suggestAllowed(cfg.SuggestReplies, cfg.TaskProvider) {
		return
	}
	delay := a.ai.deps.SuggestDelay
	if delay <= 0 {
		delay = DefaultSuggestDelay
	}
	ctx, cancel := context.WithCancel(a.ctx)
	p := &pendingSuggestion{cancel: cancel}
	a.ai.mu.Lock()
	if _, busy := a.ai.runs[repoID]; busy {
		// Another answer already started; these would be stale.
		a.ai.mu.Unlock()
		cancel()
		return
	}
	if old, ok := a.ai.suggestions[repoID]; ok {
		old.cancel()
	}
	a.ai.suggestions[repoID] = p
	a.ai.mu.Unlock()

	go func() {
		defer func() {
			a.ai.mu.Lock()
			if a.ai.suggestions[repoID] == p {
				delete(a.ai.suggestions, repoID)
			}
			a.ai.mu.Unlock()
			cancel()
		}()
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return
		}
		// AI may have been turned off while waiting, after the answer's run
		// was gone (so nothing was there to cancel): send nothing then.
		if a.stillOn(repoID) != nil {
			return
		}
		replies, err := a.generateReplies(ctx, repoID, cfg, o)
		if err != nil || ctx.Err() != nil {
			return // no suggestions is the whole failure mode
		}
		a.emit(agent.EventSuggestions, agent.SuggestionsEvent{RepoID: repoID, RunID: runID, Replies: replies})
	}()
}

func (a *App) generateReplies(ctx context.Context, repoID string, cfg settings.Settings, o reposettings.Override) ([]string, error) {
	repo, ok := a.repo(repoID)
	if !ok {
		return nil, fmt.Errorf("unknown repository %q", repoID)
	}
	history, err := a.ai.deps.Chats.Load(repoID)
	if err != nil {
		return nil, err
	}
	responder, err := a.responderFor(cfg.TaskProvider, cfg.TaskModel, cfg)
	if err != nil {
		return nil, repoNote(err, o.TaskProvider != "")
	}
	instructions, err := a.systemPrompt(repo, o, prompts.SuggestReplies, prompts.Vars{
		Repo: repo.Name, Path: repo.Path, Branch: refs.CurrentLabel(ctx, repo.Path), Date: time.Now().Format("2006-01-02"),
	}, "")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, suggestTimeout)
	defer cancel()
	return tasks.SuggestReplies(ctx, responder, instructions, history)
}
