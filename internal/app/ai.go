package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"git-ui/internal/ai"
	"git-ui/internal/ai/agent"
	"git-ui/internal/ai/anthropic"
	"git-ui/internal/ai/chatstore"
	"git-ui/internal/ai/keys"
	"git-ui/internal/ai/ollama"
	"git-ui/internal/ai/openai"
	"git-ui/internal/ai/prompts"
	"git-ui/internal/ai/settings"
	"git-ui/internal/ai/tasks"
	"git-ui/internal/ai/tools"
	"git-ui/internal/ai/writetools"
	"git-ui/internal/gitlog"
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
	// Emit sends an event to the frontend; nil uses the Wails runtime.
	Emit func(name string, data any)
	// SuggestDelay is how long after an answer suggested replies wait
	// before being generated; 0 uses DefaultSuggestDelay.
	SuggestDelay time.Duration
}

type aiState struct {
	deps       AIDeps
	mu         sync.Mutex
	runs       map[string]context.CancelFunc // repo ID → running chat
	pullCancel context.CancelFunc
	confirms   map[string]*pendingConfirm // confirm ID → pending write proposal
	// suggestions holds, per repo ID, the suggested replies waiting or
	// being generated.
	suggestions map[string]*pendingSuggestion
}

type OllamaStatus struct {
	Running            bool           `json:"running"`
	URL                string         `json:"url"`
	ChatModel          string         `json:"chatModel"`
	Models             []ollama.Model `json:"models"`
	ChatModelInstalled bool           `json:"chatModelInstalled"`
	Error              string         `json:"error,omitempty"`
}

type ProviderStatus struct {
	Provider string `json:"provider"`
	HasKey   bool   `json:"hasKey"`
	KeyHint  string `json:"keyHint"`
	Error    string `json:"error,omitempty"`
}

type AIStatus struct {
	Ollama    OllamaStatus     `json:"ollama"`
	Providers []ProviderStatus `json:"providers"`
	// KeyStore is why keys cannot be saved on this system, when they cannot.
	KeyStore string `json:"keyStore,omitempty"`
}

type ModelProgress struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Completed int64  `json:"completed"`
	Total     int64  `json:"total"`
}

type ModelDone struct {
	Name     string `json:"name"`
	Error    string `json:"error,omitempty"`
	Canceled bool   `json:"canceled,omitempty"`
}

// WithAI turns on the AI API on a with the given dependencies. It is called
// once during wiring in main.go, not exposed as a Wails binding, so the
// renderer cannot invoke it with empty or arbitrary deps.
func WithAI(a *App, d AIDeps) {
	a.ai = &aiState{deps: d, runs: map[string]context.CancelFunc{}, confirms: map[string]*pendingConfirm{}, suggestions: map[string]*pendingSuggestion{}}
}

func (a *App) emit(name string, data any) {
	// a.ai is nil when AI is off; merge and log events still go out.
	if a.ai != nil && a.ai.deps.Emit != nil {
		a.ai.deps.Emit(name, data)
		return
	}
	runtime.EventsEmit(a.ctx, name, data)
}

func (a *App) aiSettings() (settings.Settings, error) {
	if a.ai == nil {
		return settings.Settings{}, ErrAIDisabled
	}
	return settings.Load(a.ai.deps.SettingsPath)
}

func (a *App) GetAISettings() (settings.Settings, error) { return a.aiSettings() }

func (a *App) SaveAISettings(s settings.Settings) error {
	if a.ai == nil {
		return ErrAIDisabled
	}
	return settings.Save(a.ai.deps.SettingsPath, s)
}

func (a *App) AIStatus() AIStatus {
	st := AIStatus{Ollama: OllamaStatus{Models: []ollama.Model{}}, Providers: []ProviderStatus{}}
	cfg, err := a.aiSettings()
	if err != nil {
		st.Ollama.Error = err.Error()
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
	if err := keys.Available(); err != nil {
		st.KeyStore = err.Error()
	}
	for _, p := range []string{settings.ProviderOpenAI, settings.ProviderAnthropic} {
		ps := ProviderStatus{Provider: p}
		switch key, err := keys.Get(p); {
		case err != nil:
			ps.Error = err.Error()
		case key != "":
			ps.HasKey, ps.KeyHint = true, keys.Mask(key)
		}
		st.Providers = append(st.Providers, ps)
	}
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
			done.Canceled = errors.Is(err, context.Canceled)
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

// chatProvider builds the provider for the chat and agent features from the
// settings, reading the API key of a hosted provider from the OS store.
func (a *App) chatProvider(cfg settings.Settings) (ai.Provider, error) {
	switch cfg.ChatProvider {
	case settings.ProviderOllama:
		return ollama.New(cfg.OllamaURL), nil
	case settings.ProviderOpenAI, settings.ProviderAnthropic:
		key, err := keys.Get(cfg.ChatProvider)
		if err != nil {
			return nil, err
		}
		if key == "" {
			return nil, fmt.Errorf("add an API key for %s in Settings", cfg.ChatProvider)
		}
		if cfg.ChatProvider == settings.ProviderOpenAI {
			return openai.New(key), nil
		}
		return anthropic.New(key), nil
	}
	return nil, fmt.Errorf("unknown provider %q", cfg.ChatProvider)
}

// responderFor builds the one-shot responder for tasks: commit messages and
// explanations.
func (a *App) responderFor(provider, model string, cfg settings.Settings) (ai.Responder, error) {
	switch provider {
	case settings.ProviderOllama:
		return ollama.New(cfg.OllamaURL).Responder(model), nil
	case settings.ProviderOpenAI, settings.ProviderAnthropic:
		key, err := keys.Get(provider)
		if err != nil {
			return nil, err
		}
		if key == "" {
			return nil, fmt.Errorf("add an API key for %s in Settings", provider)
		}
		if provider == settings.ProviderOpenAI {
			return openai.New(key).Responder(model), nil
		}
		return anthropic.New(key).Responder(model), nil
	}
	return nil, fmt.Errorf("unknown provider %q", provider)
}

// SetProviderKey stores an API key for a hosted provider. The key is never
// written to the settings file and never returned to the frontend.
func (a *App) SetProviderKey(provider, key string) error { return keys.Set(provider, key) }

func (a *App) DeleteProviderKey(provider string) error { return keys.Delete(provider) }

// ListModels returns the models the given provider offers. Ollama lists what
// is installed; a hosted provider needs its key.
func (a *App) ListModels(provider string) ([]string, error) {
	cfg, err := a.aiSettings()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()
	switch provider {
	case settings.ProviderOllama:
		models, err := ollama.New(cfg.OllamaURL).ListModels(ctx)
		if err != nil {
			return nil, err
		}
		out := []string{}
		for _, m := range models {
			out = append(out, m.Name)
		}
		return out, nil
	case settings.ProviderOpenAI, settings.ProviderAnthropic:
		key, err := keys.Get(provider)
		if err != nil {
			return nil, err
		}
		if key == "" {
			return nil, fmt.Errorf("add an API key for %s in Settings", provider)
		}
		if provider == settings.ProviderOpenAI {
			return openai.New(key).ListModels(ctx)
		}
		return anthropic.New(key).ListModels(ctx)
	}
	return nil, fmt.Errorf("unknown provider %q", provider)
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
	repo, ok := a.repo(repoID)
	if !ok {
		return fmt.Errorf("unknown repository %q", repoID)
	}
	cfg, err := a.aiSettings()
	if err != nil {
		return err
	}
	provider, err := a.chatProvider(cfg)
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
	a.cancelSuggestions(repoID)
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

	a.emit(agent.EventStart, agent.StartEvent{RepoID: repoID, RunID: runID, Text: text, Provider: cfg.ChatProvider, Model: cfg.ChatModel})

	go func() {
		// skipStep is the step of a write call whose result did not start
		// with "done:" (rejected, failed or skipped): every later write call
		// in that same model response is refused without Prepare and
		// without a card, so a rejected or failed write doesn't let the rest
		// of the batch run anyway. A later step (a new model response)
		// clears it.
		skipStep := -1
		run := agent.Run{
			RepoID: repoID, RunID: runID,
			Provider: provider, Model: cfg.ChatModel, System: system,
			Tools: append(tools.Specs(), writetools.Specs()...),
			RunTool: func(ctx context.Context, call ai.ToolCall, step int) string {
				ctx = aiToolContext(ctx)
				if !writetools.IsWrite(call.Name) {
					return tools.Run(ctx, repo.Path, call)
				}
				if step == skipStep {
					return "error: skipped because the previous change was not approved or failed"
				}
				result := a.runWriteTool(ctx, repoID, runID, repo.Path, call)
				if !strings.HasPrefix(result, "done:") {
					skipStep = step
				}
				return result
			},
			Emit: a.emit,
		}
		updated, runErr := agent.Execute(ctx, run, history)
		at := answerTime()
		stampAnswer(updated[len(history):], cfg.ChatProvider, cfg.ChatModel, at)
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
			a.emit(agent.EventDone, agent.DoneEvent{RepoID: repoID, RunID: runID, At: at})
			// A stopped answer also ends here, with context.Canceled.
			if runErr == nil {
				a.suggestReplies(repoID, runID, cfg)
			}
		}
	}()
	return nil
}

// stampAnswer records on the assistant messages of an answer which provider
// and model produced them, and when it finished.
func stampAnswer(answer []ai.Message, provider, model, at string) {
	for i := range answer {
		if answer[i].Role == ai.RoleAssistant {
			answer[i].Provider, answer[i].Model, answer[i].At = provider, model, at
		}
	}
}

// answerTime is the time stored on an answer that just finished.
func answerTime() string { return time.Now().UTC().Format(time.RFC3339) }

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
	a.cancelSuggestions(repoID)
	return a.ai.deps.Chats.Clear(repoID)
}

// ExplainInChat explains a commit and writes the answer into the repository's
// chat, so the question and the answer stay in the conversation. provider is
// "" (use settings), settings.ProviderOllama, settings.ProviderOpenAI or
// settings.ProviderAnthropic.
func (a *App) ExplainInChat(repoID, hash, provider, runID string) error {
	if a.ai == nil {
		return ErrAIDisabled
	}
	if hash == "" {
		return errors.New("commit and run id are required")
	}
	return a.explainTask(repoID, provider, runID, prompts.ExplainCommit, func(ctx context.Context, dir string) (string, string, error) {
		question, err := explainQuestion(ctx, dir, hash)
		if err != nil {
			return "", "", err
		}
		prompt, err := tasks.ExplainContext(ctx, dir, hash, tasks.OllamaDiffBudget)
		return question, prompt, err
	})
}

// ExplainLinesInChat explains lines start..end of path at rev ("" = the
// working tree) into the repository's chat, the way ExplainInChat does for
// a commit.
func (a *App) ExplainLinesInChat(repoID, rev, path string, start, end int, provider, runID string) error {
	if a.ai == nil {
		return ErrAIDisabled
	}
	if path == "" || start < 1 || end < start {
		return errors.New("a file and a line range are required")
	}
	return a.explainTask(repoID, provider, runID, prompts.ExplainLines, func(ctx context.Context, dir string) (string, string, error) {
		prompt, err := tasks.ExplainLinesContext(ctx, dir, rev, path, start, end, tasks.OllamaDiffBudget)
		return explainLinesQuestion(rev, path, start, end), prompt, err
	})
}

// explainLinesQuestion is the user message stored for a lines explanation.
func explainLinesQuestion(rev, path string, start, end int) string {
	lines := fmt.Sprintf("lines %d\u2013%d", start, end)
	if start == end {
		lines = fmt.Sprintf("line %d", start)
	}
	at := "(working tree)"
	if rev != "" {
		at = "(at " + rev[:min(7, len(rev))] + ")"
	}
	return fmt.Sprintf("Explain %s of %s %s", lines, path, at)
}

// explainTask runs a one-shot explanation into the repository's chat: it
// takes the chat slot, builds the question and the prompt, stores the
// question, streams the answer as chat events and stores it too. build runs
// once the slot is taken, with the run's context, and returns the question
// to store and the prompt to send; an error from build (including a git
// error while blaming or diffing) is returned directly, before anything is
// written to the chat, and releases the slot. StopChat during build cancels
// it and returns nil. The frontend shows the chat as preparing meanwhile.
func (a *App) explainTask(repoID, provider, runID, promptName string, build func(ctx context.Context, dir string) (question, prompt string, err error)) error {
	if a.ai == nil {
		return ErrAIDisabled
	}
	if runID == "" {
		return errors.New("run id is required")
	}
	repo, ok := a.repo(repoID)
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
	responder, err := a.responderFor(provider, cfg.TaskModel, cfg)
	if err != nil {
		return err
	}
	instructions, err := a.ai.deps.Prompts.Get(promptName, prompts.Vars{
		Repo: repo.Name, Path: repo.Path, Branch: refs.CurrentLabel(a.ctx, repo.Path), Date: time.Now().Format("2006-01-02"),
	})
	if err != nil {
		return err
	}

	// An explanation is an answer in the chat, so it takes the repo's chat slot.
	a.ai.mu.Lock()
	if _, busy := a.ai.runs[repoID]; busy {
		a.ai.mu.Unlock()
		return ErrChatBusy
	}
	ctx, cancel := context.WithTimeout(a.ctx, 3*time.Minute)
	a.ai.runs[repoID] = cancel
	a.ai.mu.Unlock()
	a.cancelSuggestions(repoID)
	finish := func() {
		a.ai.mu.Lock()
		delete(a.ai.runs, repoID)
		a.ai.mu.Unlock()
		cancel()
	}

	question, prompt, err := build(ctx, repo.Path)
	if err != nil {
		// Stop pressed while the context was being built: nothing was
		// written to the chat yet, so there is nothing to report. Checked
		// before finish, which cancels ctx itself.
		stopped := errors.Is(ctx.Err(), context.Canceled)
		finish()
		if stopped {
			return nil
		}
		return err
	}
	history, err := a.ai.deps.Chats.Load(repoID)
	if err == nil {
		history = append(history, ai.Message{Role: ai.RoleUser, Content: question})
		err = a.ai.deps.Chats.Save(repoID, history)
	}
	if err != nil {
		finish()
		return err
	}
	a.emit(agent.EventStart, agent.StartEvent{RepoID: repoID, RunID: runID, Text: question, Provider: provider, Model: cfg.TaskModel})

	go func() {
		answer, runErr := streamIntoString(ctx, a, repoID, runID, responder, instructions, prompt)
		at := answerTime()
		var saveErr error
		if answer != "" || runErr == nil {
			saveErr = a.ai.deps.Chats.Save(repoID, append(history, ai.Message{
				Role: ai.RoleAssistant, Content: answer, Stopped: runErr != nil && errors.Is(runErr, context.Canceled),
				Provider: provider, Model: cfg.TaskModel, At: at,
			}))
		}
		finish()
		switch {
		case runErr != nil && !errors.Is(runErr, context.Canceled):
			a.emit(agent.EventError, agent.ErrorEvent{RepoID: repoID, RunID: runID, Message: runErr.Error(), Code: chatErrorCode(runErr)})
		case saveErr != nil:
			a.emit(agent.EventError, agent.ErrorEvent{RepoID: repoID, RunID: runID, Message: saveErr.Error(), Code: "other"})
		default:
			a.emit(agent.EventDone, agent.DoneEvent{RepoID: repoID, RunID: runID, At: at})
			// A stopped answer also ends here, with context.Canceled.
			if runErr == nil {
				a.suggestReplies(repoID, runID, cfg)
			}
		}
	}()
	return nil
}

// explainQuestion is the user message stored for an explanation.
func explainQuestion(ctx context.Context, dir, hash string) (string, error) {
	commits, err := gitlog.Get(ctx, dir, gitlog.Filters{Branch: hash}, gitlog.OrderTopo, 0, 1)
	if err != nil {
		return "", err
	}
	if len(commits) == 0 {
		return "", fmt.Errorf("unknown commit %q", hash)
	}
	return fmt.Sprintf("Explain commit %s: %s", commits[0].Short, commits[0].Subject), nil
}

// streamIntoString forwards an explanation to the chat as it arrives and
// returns the full text.
func streamIntoString(ctx context.Context, a *App, repoID, runID string, r ai.Responder, instructions, prompt string) (string, error) {
	stream, err := r.Respond(ctx, instructions, prompt)
	if err != nil {
		return "", err
	}
	var text strings.Builder
	var streamErr error
	for chunk := range stream {
		switch {
		case chunk.Err != nil:
			streamErr = chunk.Err
		case chunk.Delta != "":
			text.WriteString(chunk.Delta)
			a.emit(agent.EventDelta, agent.DeltaEvent{RepoID: repoID, RunID: runID, Text: chunk.Delta})
		}
	}
	if ctx.Err() != nil {
		return text.String(), ctx.Err()
	}
	return text.String(), streamErr
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
	return openFolder(a.ai.deps.Prompts.Dir())
}

func (a *App) ResetPrompt(name string) error {
	if a.ai == nil {
		return ErrAIDisabled
	}
	return a.ai.deps.Prompts.Reset(name)
}
