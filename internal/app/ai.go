package app

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"git-ui/internal/ai"
	"git-ui/internal/ai/agent"
	"git-ui/internal/ai/apple"
	"git-ui/internal/ai/chatstore"
	"git-ui/internal/ai/ollama"
	"git-ui/internal/ai/prompts"
	"git-ui/internal/ai/settings"
	"git-ui/internal/ai/tasks"
	"git-ui/internal/ai/tools"
	"git-ui/internal/refs"
)

var (
	ErrAIDisabled = errors.New("AI features are not enabled")
	ErrChatBusy   = errors.New("a chat answer is already running for this repository")
	ErrPullBusy   = errors.New("a model download is already running")
)

type AIDeps struct {
	SettingsPath string
	Chats        *chatstore.Store
	Prompts      *prompts.Store
	Apple        *apple.Client
	// Emit sends an event to the frontend; nil uses the Wails runtime.
	Emit func(name string, data any)
}

type aiState struct {
	deps       AIDeps
	mu         sync.Mutex
	runs       map[string]context.CancelFunc // repo ID → running chat
	pullCancel context.CancelFunc
	explains   map[string]*explainRun // run ID → running explain
}

// explainRun tracks a running ExplainCommit so a later run for the same
// repo can cancel it and CancelExplain can cancel it by run ID.
type explainRun struct {
	repoID string
	cancel context.CancelFunc
}

type OllamaStatus struct {
	Running            bool           `json:"running"`
	URL                string         `json:"url"`
	ChatModel          string         `json:"chatModel"`
	Models             []ollama.Model `json:"models"`
	ChatModelInstalled bool           `json:"chatModelInstalled"`
	Error              string         `json:"error,omitempty"`
}

type AIStatus struct {
	Ollama OllamaStatus       `json:"ollama"`
	Apple  apple.Availability `json:"apple"`
}

type ModelProgress struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Completed int64  `json:"completed"`
	Total     int64  `json:"total"`
}

type ModelDone struct {
	Name  string `json:"name"`
	Error string `json:"error,omitempty"`
}

type ExplainDelta struct {
	RunID string `json:"runID"`
	Text  string `json:"text"`
}

type ExplainDone struct {
	RunID string `json:"runID"`
}

type ExplainError struct {
	RunID   string `json:"runID"`
	Message string `json:"message"`
}

// WithAI turns on the AI API on a with the given dependencies. It is called
// once during wiring in main.go, not exposed as a Wails binding, so the
// renderer cannot invoke it with empty or arbitrary deps.
func WithAI(a *App, d AIDeps) {
	a.ai = &aiState{deps: d, runs: map[string]context.CancelFunc{}, explains: map[string]*explainRun{}}
}

func (a *App) emit(name string, data any) {
	if a.ai.deps.Emit != nil {
		a.ai.deps.Emit(name, data)
		return
	}
	runtime.EventsEmit(a.ctx, name, data)
}

func (a *App) aiSettings() (settings.Settings, error) {
	if a.ai == nil {
		return settings.Settings{}, ErrAIDisabled
	}
	return settings.Load(a.ai.deps.SettingsPath, func() bool { return a.ai.deps.Apple.Status(a.ctx).Available })
}

func (a *App) GetAISettings() (settings.Settings, error) { return a.aiSettings() }

func (a *App) SaveAISettings(s settings.Settings) error {
	if a.ai == nil {
		return ErrAIDisabled
	}
	return settings.Save(a.ai.deps.SettingsPath, s)
}

func (a *App) AIStatus() AIStatus {
	st := AIStatus{Ollama: OllamaStatus{Models: []ollama.Model{}}}
	cfg, err := a.aiSettings()
	if err != nil {
		st.Ollama.Error = err.Error()
		st.Apple = apple.Availability{Reason: "unknown"}
		return st
	}
	st.Ollama.URL, st.Ollama.ChatModel = cfg.OllamaURL, cfg.ChatModel
	ctx, cancel := context.WithTimeout(a.ctx, 2*time.Second)
	defer cancel()
	if models, err := ollama.New(cfg.OllamaURL).ListModels(ctx); err != nil {
		st.Ollama.Error = err.Error()
	} else {
		st.Ollama.Running, st.Ollama.Models = true, models
		for _, m := range models {
			if m.Name == cfg.ChatModel || m.Name == cfg.ChatModel+":latest" {
				st.Ollama.ChatModelInstalled = true
			}
		}
	}
	st.Apple = a.ai.deps.Apple.Status(a.ctx)
	return st
}

func (a *App) PullModel(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("model name is required")
	}
	cfg, err := a.aiSettings()
	if err != nil {
		return err
	}
	a.ai.mu.Lock()
	if a.ai.pullCancel != nil {
		a.ai.mu.Unlock()
		return ErrPullBusy
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.ai.pullCancel = cancel
	a.ai.mu.Unlock()

	go func() {
		defer func() {
			a.ai.mu.Lock()
			a.ai.pullCancel = nil
			a.ai.mu.Unlock()
			cancel()
		}()
		var last time.Time
		lastStatus := ""
		err := ollama.New(cfg.OllamaURL).Pull(ctx, name, func(status string, completed, total int64) {
			if status == lastStatus && time.Since(last) < 150*time.Millisecond {
				return
			}
			last, lastStatus = time.Now(), status
			a.emit("model:progress", ModelProgress{Name: name, Status: status, Completed: completed, Total: total})
		})
		done := ModelDone{Name: name}
		if err != nil {
			done.Error = err.Error()
		}
		a.emit("model:done", done)
	}()
	return nil
}

func (a *App) CancelPull() error {
	if a.ai == nil {
		return ErrAIDisabled
	}
	a.ai.mu.Lock()
	defer a.ai.mu.Unlock()
	if a.ai.pullCancel != nil {
		a.ai.pullCancel()
	}
	return nil
}

func (a *App) GetChat(repoID string) ([]ai.Message, error) {
	if a.ai == nil {
		return nil, ErrAIDisabled
	}
	return a.ai.deps.Chats.Load(repoID)
}

func (a *App) SendChat(repoID, text, runID string) error {
	if a.ai == nil {
		return ErrAIDisabled
	}
	text = strings.TrimSpace(text)
	if text == "" || runID == "" {
		return errors.New("message and run id are required")
	}
	repo, ok := a.store.Get(repoID)
	if !ok {
		return fmt.Errorf("unknown repository %q", repoID)
	}
	cfg, err := a.aiSettings()
	if err != nil {
		return err
	}

	a.ai.mu.Lock()
	if _, busy := a.ai.runs[repoID]; busy {
		a.ai.mu.Unlock()
		return ErrChatBusy
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.ai.runs[repoID] = cancel
	a.ai.mu.Unlock()
	finish := func() {
		a.ai.mu.Lock()
		delete(a.ai.runs, repoID)
		a.ai.mu.Unlock()
		cancel()
	}

	history, err := a.ai.deps.Chats.Load(repoID)
	if err == nil {
		history = append(history, ai.Message{Role: ai.RoleUser, Content: text})
		err = a.ai.deps.Chats.Save(repoID, history)
	}
	var system string
	if err == nil {
		system, err = a.ai.deps.Prompts.Get(prompts.Chat, prompts.Vars{
			Repo: repo.Name, Path: repo.Path, Branch: refs.CurrentLabel(ctx, repo.Path), Date: time.Now().Format("2006-01-02"),
		})
	}
	if err != nil {
		finish()
		return err
	}

	go func() {
		run := agent.Run{
			RepoID: repoID, RunID: runID,
			Provider: ollama.New(cfg.OllamaURL), Model: cfg.ChatModel, System: system,
			Tools:   tools.Specs(),
			RunTool: func(ctx context.Context, call ai.ToolCall) string { return tools.Run(ctx, repo.Path, call) },
			Emit:    a.emit,
		}
		updated, runErr := agent.Execute(ctx, run, history)
		saveErr := a.ai.deps.Chats.Save(repoID, updated)
		// Release the repo before announcing the end so a new message can
		// be sent as soon as the frontend sees done/error.
		finish()
		switch {
		case runErr != nil && !errors.Is(runErr, context.Canceled):
			a.emit(agent.EventError, agent.ErrorEvent{RepoID: repoID, RunID: runID, Message: runErr.Error(), Code: chatErrorCode(runErr)})
		case saveErr != nil:
			a.emit(agent.EventError, agent.ErrorEvent{RepoID: repoID, RunID: runID, Message: saveErr.Error(), Code: "other"})
		default:
			a.emit(agent.EventDone, agent.DoneEvent{RepoID: repoID, RunID: runID})
		}
	}()
	return nil
}

func chatErrorCode(err error) string {
	switch {
	case errors.Is(err, ollama.ErrUnreachable):
		return "ollama_down"
	case errors.Is(err, ollama.ErrModelNotFound):
		return "model_missing"
	case errors.Is(err, ollama.ErrNoToolSupport):
		return "no_tool_support"
	}
	return "other"
}

func (a *App) StopChat(repoID string) error {
	if a.ai == nil {
		return ErrAIDisabled
	}
	a.ai.mu.Lock()
	defer a.ai.mu.Unlock()
	if cancel, ok := a.ai.runs[repoID]; ok {
		cancel()
	}
	return nil
}

func (a *App) ClearChat(repoID string) error {
	if a.ai == nil {
		return ErrAIDisabled
	}
	a.ai.mu.Lock()
	_, busy := a.ai.runs[repoID]
	a.ai.mu.Unlock()
	if busy {
		return ErrChatBusy
	}
	return a.ai.deps.Chats.Clear(repoID)
}

// ExplainCommit streams an explanation of a commit. provider is "" (use
// settings), settings.ProviderApple or settings.ProviderOllama.
func (a *App) ExplainCommit(repoID, hash, provider, runID string) error {
	if a.ai == nil {
		return ErrAIDisabled
	}
	if runID == "" {
		return errors.New("run id is required")
	}
	repo, ok := a.store.Get(repoID)
	if !ok {
		return fmt.Errorf("unknown repository %q", repoID)
	}
	cfg, err := a.aiSettings()
	if err != nil {
		return err
	}
	if provider == "" {
		provider = cfg.TaskProvider
	}
	var responder ai.Responder
	budget := tasks.OllamaDiffBudget
	switch provider {
	case settings.ProviderApple:
		responder, budget = a.ai.deps.Apple, tasks.AppleDiffBudget
	case settings.ProviderOllama:
		responder = ollama.New(cfg.OllamaURL).Responder(cfg.TaskModel)
	default:
		return fmt.Errorf("unknown provider %q", provider)
	}
	instructions, err := a.ai.deps.Prompts.Get(prompts.ExplainCommit, prompts.Vars{
		Repo: repo.Name, Path: repo.Path, Branch: refs.CurrentLabel(a.ctx, repo.Path), Date: time.Now().Format("2006-01-02"),
	})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(a.ctx, 3*time.Minute)
	a.ai.mu.Lock()
	for id, run := range a.ai.explains {
		if run.repoID == repoID {
			run.cancel()
			delete(a.ai.explains, id)
		}
	}
	a.ai.explains[runID] = &explainRun{repoID: repoID, cancel: cancel}
	a.ai.mu.Unlock()
	finish := func() {
		a.ai.mu.Lock()
		delete(a.ai.explains, runID)
		a.ai.mu.Unlock()
		cancel()
	}

	go func() {
		defer finish()
		stream, err := tasks.Explain(ctx, responder, instructions, repo.Path, hash, budget)
		if err != nil {
			a.emit("explain:error", ExplainError{RunID: runID, Message: err.Error()})
			return
		}
		var streamErr error
		for chunk := range stream {
			switch {
			case chunk.Err != nil:
				streamErr = chunk.Err
			case chunk.Delta != "":
				a.emit("explain:delta", ExplainDelta{RunID: runID, Text: chunk.Delta})
			}
		}
		if streamErr != nil {
			a.emit("explain:error", ExplainError{RunID: runID, Message: streamErr.Error()})
			return
		}
		a.emit("explain:done", ExplainDone{RunID: runID})
	}()
	return nil
}

// CancelExplain cancels a running explain by its run ID. It is a no-op if
// no such run is active (already finished, or never started).
func (a *App) CancelExplain(runID string) error {
	if a.ai == nil {
		return ErrAIDisabled
	}
	a.ai.mu.Lock()
	defer a.ai.mu.Unlock()
	if run, ok := a.ai.explains[runID]; ok {
		run.cancel()
	}
	return nil
}

func (a *App) ListPrompts() ([]prompts.Info, error) {
	if a.ai == nil {
		return nil, ErrAIDisabled
	}
	return a.ai.deps.Prompts.List(), nil
}

func (a *App) OpenPromptsFolder() error {
	if a.ai == nil {
		return ErrAIDisabled
	}
	if err := a.ai.deps.Prompts.EnsureFiles(); err != nil {
		return err
	}
	return exec.Command("open", a.ai.deps.Prompts.Dir()).Start()
}

func (a *App) ResetPrompt(name string) error {
	if a.ai == nil {
		return ErrAIDisabled
	}
	return a.ai.deps.Prompts.Reset(name)
}
