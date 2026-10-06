package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

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
