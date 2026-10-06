package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"git-ui/internal/ai/agent"
	"git-ui/internal/ai/keys"
	"git-ui/internal/ai/reposettings"
	"git-ui/internal/ai/settings"
	"git-ui/internal/repos"
)

func setRepoAI(t *testing.T, a *App, id string, o reposettings.Override) {
	t.Helper()
	if err := a.repoAIStore().Set(id, o); err != nil {
		t.Fatal(err)
	}
}

func TestAISettingsForMergesOverride(t *testing.T) {
	a, id, _ := newAIApp(t, "http://127.0.0.1:1")
	setRepoAI(t, a, id, reposettings.Override{TaskProvider: settings.ProviderOllama, TaskModel: "qwen3:14b"})
	cfg, _, err := a.aiSettingsFor(id)
	if err != nil || cfg.TaskModel != "qwen3:14b" || cfg.ChatModel != settings.DefaultModel {
		t.Fatalf("got %+v, %v", cfg, err)
	}
}

func TestAIOffRefusesEveryEntryPoint(t *testing.T) {
	a, id, _ := newAIApp(t, "http://127.0.0.1:1")
	setRepoAI(t, a, id, reposettings.Override{AIOff: true})
	checks := map[string]error{
		"chat":    a.SendChat(id, "hi", "run1"),
		"explain": a.ExplainInChat(id, "HEAD", "", "run2"),
		"lines":   a.ExplainLinesInChat(id, "", "f.txt", 1, 1, "", "run3"),
		"commit":  a.GenerateCommitMessage(id, "run4"),
		"resolve": a.ResolveConflicts(id, "run5"),
	}
	for name, err := range checks {
		if !errors.Is(err, ErrAIOff) {
			t.Errorf("%s: want ErrAIOff, got %v", name, err)
		}
	}
}

func TestCorruptRepoAIRefusesInsteadOfGlobal(t *testing.T) {
	a, id, _ := newAIApp(t, "http://127.0.0.1:1")
	_ = os.WriteFile(reposettings.PathNextTo(a.ai.deps.SettingsPath), []byte("{oops"), 0o644)
	_, _, err := a.aiSettingsFor(id)
	if !errors.Is(err, reposettings.ErrUnreadable) {
		t.Fatalf("got %v", err)
	}
	if err := a.SendChat(id, "hi", "run1"); !errors.Is(err, reposettings.ErrUnreadable) {
		t.Fatalf("SendChat: %v", err)
	}
}

func TestChatUsesRepoModelAndInstructions(t *testing.T) {
	var mu sync.Mutex
	var model, system string
	srv := fakeOllama(t, func(req map[string]any) {
		mu.Lock()
		defer mu.Unlock()
		model = req["model"].(string)
		system = req["messages"].([]any)[0].(map[string]any)["content"].(string)
	})
	a, id, ev := newAIApp(t, srv.URL)
	setRepoAI(t, a, id, reposettings.Override{ChatProvider: settings.ProviderOllama, ChatModel: "qwen2.5:7b",
		Instructions: map[string]string{"chat": "Answer in Spanish."}})
	repo, _ := a.repo(id)
	_ = os.MkdirAll(filepath.Join(repo.Path, ".committree"), 0o755)
	_ = os.WriteFile(filepath.Join(repo.Path, ".committree", "instructions.md"), []byte("Project rule."), 0o644)
	if err := a.SendChat(id, "hola", "run1"); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, "chat:done")
	mu.Lock()
	defer mu.Unlock()
	if model != "qwen2.5:7b" {
		t.Fatalf("model %q", model)
	}
	if !strings.Contains(system, "## Your instructions for this repository\n\nAnswer in Spanish.") {
		t.Fatalf("private instructions missing:\n%s", system)
	}
	if strings.Contains(system, "Project rule.") {
		t.Fatal("unapproved repository instructions were used")
	}
}

func TestChatUsesApprovedRepoInstructions(t *testing.T) {
	var mu sync.Mutex
	var system string
	srv := fakeOllama(t, func(req map[string]any) {
		mu.Lock()
		defer mu.Unlock()
		system = req["messages"].([]any)[0].(map[string]any)["content"].(string)
	})
	a, id, ev := newAIApp(t, srv.URL)
	repo, _ := a.repo(id)
	_ = os.MkdirAll(filepath.Join(repo.Path, ".committree"), 0o755)
	_ = os.WriteFile(filepath.Join(repo.Path, ".committree", "instructions.md"), []byte("Project rule."), 0o644)
	if err := a.repoAIStore().SetApproval(id, reposettings.ReadRepo(repo.Path).Hash); err != nil {
		t.Fatal(err)
	}
	if err := a.SendChat(id, "hola", "run1"); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, "chat:done")
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(system, "## Project instructions\n\nProject rule.") {
		t.Fatalf("approved repository instructions missing:\n%s", system)
	}
}

func TestMissingKeyNamesRepoSettings(t *testing.T) {
	a, id, _ := newAIApp(t, "http://127.0.0.1:1")
	restore := keys.UseStore(&fakeKeys{items: map[string]string{}}) // no key stored
	defer restore()
	setRepoAI(t, a, id, reposettings.Override{ChatProvider: settings.ProviderAnthropic, ChatModel: "claude-opus-5"})
	err := a.SendChat(id, "hi", "run1")
	if err == nil || !strings.HasSuffix(err.Error(), "(set in this repository's settings)") {
		t.Fatalf("got %v", err)
	}
}

func TestRemoveRepoDropsItsEntry(t *testing.T) {
	a, id, _ := newAIApp(t, "http://127.0.0.1:1")
	setRepoAI(t, a, id, reposettings.Override{AIOff: true})
	if err := a.RemoveRepo(id); err != nil {
		t.Fatal(err)
	}
	if o, _ := a.repoAIStore().Get(id); !o.IsEmpty() {
		t.Fatalf("entry left: %+v", o)
	}
}

func TestWorktreeUsesMainRepoSettings(t *testing.T) {
	a, id, _ := newAIApp(t, "http://127.0.0.1:1")
	_, wt := withWorktree(t, a, id)
	wtID := repos.IDFor(wt)
	var found bool
	for _, it := range a.ListRepos() { // detects the worktree
		if it.ID == wtID && it.Worktree {
			found = true
		}
	}
	if !found {
		t.Fatal("worktree not detected")
	}
	setRepoAI(t, a, id, reposettings.Override{AIOff: true})
	if _, _, err := a.aiSettingsFor(wtID); !errors.Is(err, ErrAIOff) {
		t.Fatalf("worktree: %v", err)
	}
}

// A linked worktree the user also added to the list is still matched by
// path rather than detected, but uses its main repository's settings.
func TestAddedWorktreeUsesMainRepoSettings(t *testing.T) {
	a, id, _ := newAIApp(t, "http://127.0.0.1:1")
	_, wt := withWorktree(t, a, id)
	added, err := a.store.Add(context.Background(), wt)
	if err != nil {
		t.Fatal(err)
	}
	a.ListRepos()
	setRepoAI(t, a, id, reposettings.Override{AIOff: true})
	if _, _, err := a.aiSettingsFor(added.ID); !errors.Is(err, ErrAIOff) {
		t.Fatalf("added worktree: %v", err)
	}
}

func TestGetRepoAISettings(t *testing.T) {
	a, id, _ := newAIApp(t, "http://127.0.0.1:1")
	repo, _ := a.repo(id)
	_ = os.MkdirAll(filepath.Join(repo.Path, ".committree"), 0o755)
	_ = os.WriteFile(filepath.Join(repo.Path, ".committree", "instructions.md"), []byte("Rule."), 0o644)
	if err := a.SaveRepoAISettings(id, reposettings.Override{SuggestReplies: settings.SuggestAuto}); err != nil {
		t.Fatal(err)
	}
	info, err := a.GetRepoAISettings(id)
	if err != nil {
		t.Fatal(err)
	}
	if info.Effective.SuggestReplies != settings.SuggestAuto || info.Global.SuggestReplies != settings.SuggestOff ||
		info.State != reposettings.StatePending || len(info.RepoInstructions.Files) != 1 || info.AIOff {
		t.Fatalf("got %+v", info)
	}
}

func TestGetRepoAISettingsCorruptIsOffWithError(t *testing.T) {
	a, id, _ := newAIApp(t, "http://127.0.0.1:1")
	_ = os.WriteFile(reposettings.PathNextTo(a.ai.deps.SettingsPath), []byte("{oops"), 0o644)
	info, err := a.GetRepoAISettings(id)
	if err != nil || !info.AIOff || !strings.HasPrefix(info.Error, "Per-repository AI settings can't be read") {
		t.Fatalf("got %+v, %v", info, err)
	}
}

func TestApproveAndIgnore(t *testing.T) {
	a, id, ev := newAIApp(t, "http://127.0.0.1:1")
	repo, _ := a.repo(id)
	_ = os.MkdirAll(filepath.Join(repo.Path, ".committree"), 0o755)
	_ = os.WriteFile(filepath.Join(repo.Path, ".committree", "instructions.md"), []byte("Rule."), 0o644)
	info, _ := a.GetRepoAISettings(id)
	if err := a.IgnoreRepoInstructions(id, info.RepoInstructions.Hash); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.GetRepoAISettings(id); got.State != reposettings.StateIgnored {
		t.Fatalf("state %s", got.State)
	}
	if err := a.ApproveRepoInstructions(id, info.RepoInstructions.Hash); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.GetRepoAISettings(id); got.State != reposettings.StateApproved {
		t.Fatalf("state %s", got.State)
	}
	ev.wait(t, EventRepoAIChanged)
}

func TestApproveRefusesStaleHash(t *testing.T) {
	a, id, _ := newAIApp(t, "http://127.0.0.1:1")
	repo, _ := a.repo(id)
	_ = os.MkdirAll(filepath.Join(repo.Path, ".committree"), 0o755)
	p := filepath.Join(repo.Path, ".committree", "instructions.md")
	_ = os.WriteFile(p, []byte("Seen."), 0o644)
	seen, _ := a.GetRepoAISettings(id)
	_ = os.WriteFile(p, []byte("Changed after."), 0o644)
	if err := a.ApproveRepoInstructions(id, seen.RepoInstructions.Hash); !errors.Is(err, ErrInstructionsChanged) {
		t.Fatalf("got %v", err)
	}
	if got, _ := a.GetRepoAISettings(id); got.State != reposettings.StatePending {
		t.Fatalf("state %s", got.State)
	}
}

func TestSaveRepoAISettingsRejectsInvalid(t *testing.T) {
	a, id, _ := newAIApp(t, "http://127.0.0.1:1")
	if err := a.SaveRepoAISettings(id, reposettings.Override{ChatProvider: settings.ProviderOllama}); !errors.Is(err, reposettings.ErrInvalid) {
		t.Fatalf("got %v", err)
	}
}

func TestTurningAIOffCancelsRunning(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			fmt.Fprint(w, `{"models":[{"name":"qwen2.5:7b"}]}`)
			return
		}
		writeLines(w, `{"message":{"role":"assistant","content":"partial"},"done":false}`)
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	a, id, ev := newAIApp(t, srv.URL)
	if err := a.SendChat(id, "hi", "run1"); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventDelta)
	if err := a.SaveRepoAISettings(id, reposettings.Override{AIOff: true}); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventDone) // a stopped answer ends with done (context.Canceled)
	if a.aiBusy(id) {
		t.Fatal("chat still running")
	}
}

// Turning a main repository off also stops the commit message and the
// pending suggestions of its linked worktrees, detected or added to the list.
func TestTurningAIOffStopsLinkedWorktrees(t *testing.T) {
	a, id, _ := newAIApp(t, "http://127.0.0.1:1")
	_, wt := withWorktree(t, a, id)
	added, err := a.store.Add(context.Background(), wt)
	if err != nil {
		t.Fatal(err)
	}
	a.ListRepos()
	wtCtx, wtCancel := context.WithCancel(context.Background())
	defer wtCancel()
	sugCtx, sugCancel := context.WithCancel(context.Background())
	defer sugCancel()
	run := &commitRun{cancel: wtCancel}
	a.ai.mu.Lock()
	a.ai.commits[added.ID] = run
	a.ai.suggestions[added.ID] = &pendingSuggestion{cancel: sugCancel}
	a.ai.mu.Unlock()

	if err := a.SaveRepoAISettings(id, reposettings.Override{AIOff: true}); err != nil {
		t.Fatal(err)
	}
	if wtCtx.Err() == nil {
		t.Error("the worktree's commit message was not cancelled")
	}
	if sugCtx.Err() == nil {
		t.Error("the worktree's suggestions were not cancelled")
	}
	if got := run.cancelMessage(); got != ErrAIOff.Error() {
		t.Errorf("cancel message %q", got)
	}
}

func TestApproveAndIgnoreWithoutInstructions(t *testing.T) {
	a, id, _ := newAIApp(t, "http://127.0.0.1:1")
	if err := a.ApproveRepoInstructions(id, ""); !errors.Is(err, ErrNoInstructions) {
		t.Fatalf("approve: %v", err)
	}
	if err := a.IgnoreRepoInstructions(id, "abc"); !errors.Is(err, ErrNoInstructions) {
		t.Fatalf("ignore with no files: %v", err)
	}
}

func TestIgnoreRefusesStaleHashAndTellsViews(t *testing.T) {
	a, id, ev := newAIApp(t, "http://127.0.0.1:1")
	repo, _ := a.repo(id)
	_ = os.MkdirAll(filepath.Join(repo.Path, ".committree"), 0o755)
	p := filepath.Join(repo.Path, ".committree", "instructions.md")
	_ = os.WriteFile(p, []byte("Seen."), 0o644)
	seen, _ := a.GetRepoAISettings(id)
	_ = os.WriteFile(p, []byte("Changed after."), 0o644)
	if err := a.IgnoreRepoInstructions(id, seen.RepoInstructions.Hash); !errors.Is(err, ErrInstructionsChanged) {
		t.Fatalf("got %v", err)
	}
	if got, _ := a.GetRepoAISettings(id); got.State != reposettings.StatePending {
		t.Fatalf("state %s", got.State)
	}
	// The refusal tells open views to reload the new content.
	ev.wait(t, EventRepoAIChanged)
}

// The AI can be turned off after a run's settings were read and before it
// registers itself; stopRepoAI then finds nothing to cancel, so the run must
// notice by itself and not stream.
func TestChatNotStartedWhenTurnedOffBeforeItRegisters(t *testing.T) {
	var mu sync.Mutex
	chatCalls := 0
	srv := fakeOllama(t, func(map[string]any) {
		mu.Lock()
		chatCalls++
		mu.Unlock()
	})
	a, id, ev := newAIApp(t, srv.URL)
	a.ai.afterSettings = func(string) {
		a.ai.afterSettings = nil
		if err := a.SaveRepoAISettings(id, reposettings.Override{AIOff: true}); err != nil {
			t.Error(err)
		}
	}
	if err := a.SendChat(id, "hi", "run1"); !errors.Is(err, ErrAIOff) {
		t.Fatalf("got %v", err)
	}
	if a.aiBusy(id) {
		t.Fatal("the chat slot was left taken")
	}
	mu.Lock()
	defer mu.Unlock()
	if chatCalls != 0 {
		t.Fatalf("the model was called %d times", chatCalls)
	}
	for _, n := range ev.names() {
		if n == agent.EventStart || n == agent.EventDelta {
			t.Fatalf("the run started: %v", ev.names())
		}
	}
}
