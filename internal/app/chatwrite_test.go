package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"git-ui/internal/ai"
	"git-ui/internal/ai/agent"
	"git-ui/internal/ai/chatstore"
	"git-ui/internal/ai/prompts"
	"git-ui/internal/ai/writetools"
	"git-ui/internal/gitsettings"
	"git-ui/internal/repos"
	"git-ui/internal/testrepo"
)

// fakeWriteOllama asks for tool(args) on the first turn and answers "ok"
// once the tool result is in the history.
func fakeWriteOllama(t *testing.T, tool, argsJSON string) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			fmt.Fprint(w, `{"models":[{"name":"qwen2.5:7b","size":1}]}`)
		case "/api/chat":
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			msgs := req["messages"].([]any)
			if msgs[len(msgs)-1].(map[string]any)["role"] != "tool" {
				writeLines(w, `{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","function":{"name":"`+tool+`","arguments":`+argsJSON+`}}]},"done":false}`, `{"message":{"content":""},"done":true}`)
				return
			}
			writeLines(w, `{"message":{"role":"assistant","content":"ok"},"done":false}`, `{"message":{"content":""},"done":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// toolResult returns the content of the saved tool message.
func toolResult(t *testing.T, a *App, id string) string {
	t.Helper()
	history, err := a.GetChat(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range history {
		if m.Role == ai.RoleTool {
			return m.Content
		}
	}
	t.Fatalf("no tool message in %+v", history)
	return ""
}

// toolResults returns the content of every saved tool message, in order.
func toolResults(t *testing.T, a *App, id string) []string {
	t.Helper()
	history, err := a.GetChat(id)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, m := range history {
		if m.Role == ai.RoleTool {
			out = append(out, m.Content)
		}
	}
	return out
}

// fakeWriteOllamaBatch asks for two calls to tool in a single response (the
// same model turn), so a rejected/failed first call can be checked to skip
// the second instead of asking for it too.
func fakeWriteOllamaBatch(t *testing.T, tool, args1, args2 string) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			fmt.Fprint(w, `{"models":[{"name":"qwen2.5:7b","size":1}]}`)
		case "/api/chat":
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			msgs := req["messages"].([]any)
			if msgs[len(msgs)-1].(map[string]any)["role"] != "tool" {
				writeLines(w,
					`{"message":{"role":"assistant","content":"","tool_calls":[`+
						`{"id":"call_1","function":{"name":"`+tool+`","arguments":`+args1+`}},`+
						`{"id":"call_2","function":{"name":"`+tool+`","arguments":`+args2+`}}`+
						`]},"done":false}`,
					`{"message":{"content":""},"done":true}`)
				return
			}
			writeLines(w, `{"message":{"role":"assistant","content":"ok"},"done":false}`, `{"message":{"content":""},"done":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func branchExists(t *testing.T, dir, name string) bool {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--verify", "refs/heads/"+name)
	return cmd.Run() == nil
}

func TestChatWriteApprovedRuns(t *testing.T) {
	a, id, ev := newAIApp(t, fakeWriteOllama(t, "create_branch", `{"name":"topic"}`).URL)
	dir, _ := a.dir(id)

	if err := a.SendChat(id, "make a branch", "run-1"); err != nil {
		t.Fatal(err)
	}
	confirm := ev.wait(t, EventChatConfirm).data.(ConfirmEvent)
	if confirm.Tool != "create_branch" || !strings.HasPrefix(confirm.Title, "Create branch topic at") {
		t.Fatalf("confirm = %+v", confirm)
	}
	if confirm.RepoID != id || confirm.RunID != "run-1" || confirm.ConfirmID == "" {
		t.Fatalf("confirm = %+v", confirm)
	}
	got := a.GetChatConfirm(id)
	if got == nil || !reflect.DeepEqual(*got, confirm) {
		t.Fatalf("GetChatConfirm = %+v, want %+v", got, confirm)
	}

	if err := a.ConfirmChatAction(id, confirm.ConfirmID, true); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, EventRepoChanged)
	ev.wait(t, agent.EventDone)

	if !branchExists(t, dir, "topic") {
		t.Fatal("branch topic was not created")
	}
	if res := toolResult(t, a, id); !strings.HasPrefix(res, "done:") {
		t.Fatalf("tool result = %q", res)
	}
	if a.GetChatConfirm(id) != nil {
		t.Fatal("confirm still pending after being resolved")
	}
	if err := a.ConfirmChatAction(id, confirm.ConfirmID, true); err == nil {
		t.Fatal("want an error confirming the same id twice")
	}
}

func TestChatWriteRejectedDoesNothing(t *testing.T) {
	a, id, ev := newAIApp(t, fakeWriteOllama(t, "create_branch", `{"name":"topic"}`).URL)
	dir, _ := a.dir(id)

	if err := a.SendChat(id, "make a branch", "run-1"); err != nil {
		t.Fatal(err)
	}
	confirm := ev.wait(t, EventChatConfirm).data.(ConfirmEvent)

	if err := a.ConfirmChatAction(id, confirm.ConfirmID, false); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventDone)

	if res := toolResult(t, a, id); res != "rejected by the user" {
		t.Fatalf("tool result = %q", res)
	}
	if branchExists(t, dir, "topic") {
		t.Fatal("branch topic should not exist")
	}
	for _, n := range ev.names() {
		if n == EventRepoChanged {
			t.Fatal("repo:changed emitted for a rejected write")
		}
	}
}

func TestChatWriteRejectedSkipsLaterWritesInSameResponse(t *testing.T) {
	a, id, ev := newAIApp(t, fakeWriteOllamaBatch(t, "create_branch", `{"name":"x"}`, `{"name":"y"}`).URL)
	dir, _ := a.dir(id)

	if err := a.SendChat(id, "make two branches", "run-1"); err != nil {
		t.Fatal(err)
	}
	confirm := ev.wait(t, EventChatConfirm).data.(ConfirmEvent)

	if err := a.ConfirmChatAction(id, confirm.ConfirmID, false); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventDone)

	confirms := 0
	for _, n := range ev.names() {
		if n == EventChatConfirm {
			confirms++
		}
	}
	if confirms != 1 {
		t.Fatalf("chat:confirm emitted %d times, want 1 (no card for the skipped call)", confirms)
	}

	results := toolResults(t, a, id)
	if len(results) != 2 {
		t.Fatalf("results = %v", results)
	}
	if results[0] != "rejected by the user" {
		t.Fatalf("results[0] = %q", results[0])
	}
	if results[1] != "error: skipped because the previous change was not approved or failed" {
		t.Fatalf("results[1] = %q", results[1])
	}
	if branchExists(t, dir, "x") || branchExists(t, dir, "y") {
		t.Fatal("neither branch should have been created")
	}
}

func TestChatWriteRefusesWhenTheRepoChanged(t *testing.T) {
	a, id, ev := newAIApp(t, fakeWriteOllama(t, "create_branch", `{"name":"topic"}`).URL)
	dir, _ := a.dir(id)

	if err := a.SendChat(id, "make a branch", "run-1"); err != nil {
		t.Fatal(err)
	}
	confirm := ev.wait(t, EventChatConfirm).data.(ConfirmEvent)

	cmd := exec.Command("git", "-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "--allow-empty", "-q", "-m", "moved")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit: %v: %s", err, out)
	}

	if err := a.ConfirmChatAction(id, confirm.ConfirmID, true); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventDone)

	res := toolResult(t, a, id)
	if !strings.HasPrefix(res, "error: the repository changed") {
		t.Fatalf("tool result = %q", res)
	}
	if branchExists(t, dir, "topic") {
		t.Fatal("branch topic should not exist")
	}
}

func TestChatWriteBusyLock(t *testing.T) {
	a, id, ev := newAIApp(t, fakeWriteOllama(t, "create_branch", `{"name":"topic"}`).URL)

	if err := a.SendChat(id, "make a branch", "run-1"); err != nil {
		t.Fatal(err)
	}
	confirm := ev.wait(t, EventChatConfirm).data.(ConfirmEvent)

	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- a.write(id, func(ctx context.Context, dir string) error {
			close(started)
			<-release
			return nil
		})
	}()
	<-started

	if err := a.ConfirmChatAction(id, confirm.ConfirmID, true); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventDone)

	res := toolResult(t, a, id)
	if !strings.HasPrefix(res, "error: another operation is running") {
		t.Fatalf("tool result = %q", res)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestChatWriteStopWhilePendingRejects(t *testing.T) {
	a, id, ev := newAIApp(t, fakeWriteOllama(t, "create_branch", `{"name":"topic"}`).URL)
	dir, _ := a.dir(id)

	if err := a.SendChat(id, "make a branch", "run-1"); err != nil {
		t.Fatal(err)
	}
	confirm := ev.wait(t, EventChatConfirm).data.(ConfirmEvent)

	if err := a.StopChat(id); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventDone)

	if branchExists(t, dir, "topic") {
		t.Fatal("branch topic should not exist")
	}
	if a.GetChatConfirm(id) != nil {
		t.Fatal("confirm still pending after stop")
	}
	if err := a.ConfirmChatAction(id, confirm.ConfirmID, true); err == nil {
		t.Fatal("want an error confirming after stop")
	}
}

func TestChatWriteInvalidArgsNeverAsks(t *testing.T) {
	a, id, ev := newAIApp(t, fakeWriteOllama(t, "create_branch", `{"name":"feature"}`).URL)

	if err := a.SendChat(id, "make a branch", "run-1"); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventDone)

	for _, n := range ev.names() {
		if n == EventChatConfirm {
			t.Fatal("chat:confirm emitted for an invalid call")
		}
	}
	res := toolResult(t, a, id)
	if !strings.HasPrefix(res, `error: branch "feature" already exists`) {
		t.Fatalf("tool result = %q", res)
	}
}

func TestConfirmChatActionUnknownID(t *testing.T) {
	a, id, _ := newAIApp(t, fakeWriteOllama(t, "create_branch", `{"name":"topic"}`).URL)
	if err := a.ConfirmChatAction(id, "nope", true); err == nil {
		t.Fatal("want an error for an unknown confirm id")
	}
}

func TestSendChatOffersWriteTools(t *testing.T) {
	var toolNames []string
	srv := fakeOllama(t, func(req map[string]any) {
		tools, ok := req["tools"].([]any)
		if !ok {
			return
		}
		toolNames = nil
		for _, tl := range tools {
			fn := tl.(map[string]any)["function"].(map[string]any)
			toolNames = append(toolNames, fn["name"].(string))
		}
	})
	a, id, ev := newAIApp(t, srv.URL)

	if err := a.SendChat(id, "hola", "run-1"); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventDone)

	joined := strings.Join(toolNames, ",")
	if !strings.Contains(joined, "list_refs") || !strings.Contains(joined, "push") {
		t.Fatalf("tools = %v", toolNames)
	}
}

// TestChatWriteEmitsRepoChangedForAnAttemptedButFailedWrite covers M1:
// repo:changed goes out whenever executeWrite was attempted, even when it
// errors, so a partial effect still refreshes the UI.
func TestChatWriteEmitsRepoChangedForAnAttemptedButFailedWrite(t *testing.T) {
	a, id, ev := newAIApp(t, fakeWriteOllama(t, "stage_files", `{"paths":["a.txt"]}`).URL)
	dir, _ := a.dir(id)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := a.SendChat(id, "stage a.txt", "run-1"); err != nil {
		t.Fatal(err)
	}
	confirm := ev.wait(t, EventChatConfirm).data.(ConfirmEvent)

	// The file vanishes between the proposal and the approval: Recheck
	// (which only looks at refs and, for commit, the staged set) doesn't
	// catch this, so execution is attempted and fails.
	if err := os.Remove(filepath.Join(dir, "a.txt")); err != nil {
		t.Fatal(err)
	}
	if err := a.ConfirmChatAction(id, confirm.ConfirmID, true); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, EventRepoChanged)
	ev.wait(t, agent.EventDone)

	if res := toolResult(t, a, id); !strings.HasPrefix(res, "error:") {
		t.Fatalf("tool result = %q", res)
	}
}

// TestExecuteWriteReportsAPartialCreateBranchCheckoutFailure covers the M1
// wording for create_branch+checkout specifically: when the branch was
// created but the switch failed, the result says so instead of a bare
// "switch" error that would suggest nothing happened.
func TestExecuteWriteReportsAPartialCreateBranchCheckoutFailure(t *testing.T) {
	store, err := repos.Open(filepath.Join(t.TempDir(), "repos.json"))
	if err != nil {
		t.Fatal(err)
	}
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("switch", "-q", "-c", "other")
	r.WriteFile("conflict.txt", "committed\n")
	r.Git("add", "conflict.txt")
	r.Git("commit", "-q", "-m", "other work")
	otherHash := r.Git("rev-parse", "HEAD")
	r.Git("switch", "-q", "main")
	// An untracked file at a path the target commit also has, with
	// different content: `git branch` succeeds, but the following
	// `git switch` refuses to overwrite it.
	r.WriteFile("conflict.txt", "untracked and different\n")

	repo, err := store.Add(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	a := New(store)

	_, err = a.executeWrite(repo.ID, "create_branch", writetools.Proposal{
		Title: "Create branch topic at " + otherHash[:7], Name: "topic", Start: otherHash, Checkout: true,
	})
	if err == nil || !strings.Contains(err.Error(), "created branch topic but could not switch to it") {
		t.Fatalf("err = %v", err)
	}
	if !branchExists(t, r.Dir, "topic") {
		t.Fatal("branch topic should have been created despite the failed switch")
	}
}

// TestPullStrategyIsReadFreshEachTimeRunWriteToolPrepares covers M2: the
// pull strategy is read at Prepare time (inside runWriteTool), not once for
// the whole SendChat run, so a setting saved between two write calls in the
// same run is picked up by the later one.
func TestPullStrategyIsReadFreshEachTimeRunWriteToolPrepares(t *testing.T) {
	src := testrepo.New(t)
	src.Commit("base")
	remote := testrepo.NewBareFrom(t, src)
	r := testrepo.Clone(t, remote)
	src.Commit("two")
	src.Git("push", "-q", remote, "main:main")
	r.Git("fetch", "-q", "origin")

	store, err := repos.Open(filepath.Join(t.TempDir(), "repos.json"))
	if err != nil {
		t.Fatal(err)
	}
	repo, err := store.Add(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	a := New(store)
	tmp := t.TempDir()
	ev := newEvents()
	WithAI(a, AIDeps{
		SettingsPath: filepath.Join(tmp, "ai.json"),
		Chats:        chatstore.New(filepath.Join(tmp, "chats")),
		Prompts:      prompts.New(filepath.Join(tmp, "prompts")),
		Emit:         ev.emit,
	})
	a.gitSettingsPath = filepath.Join(tmp, "git.json")

	s, err := a.GetGitSettings()
	if err != nil {
		t.Fatal(err)
	}
	s.PullStrategy = gitsettings.PullMerge
	if err := a.SaveGitSettings(s); err != nil {
		t.Fatal(err)
	}

	go a.runWriteTool(context.Background(), repo.ID, "run-1", r.Dir, ai.ToolCall{ID: "c1", Name: "pull"})
	confirm := ev.wait(t, EventChatConfirm).data.(ConfirmEvent)
	if !strings.Contains(confirm.Title, "(merge)") {
		t.Fatalf("title = %q, want the strategy in effect for this call", confirm.Title)
	}

	s.PullStrategy = gitsettings.PullRebase
	if err := a.SaveGitSettings(s); err != nil {
		t.Fatal(err)
	}
	if err := a.ConfirmChatAction(repo.ID, confirm.ConfirmID, false); err != nil {
		t.Fatal(err)
	}

	go a.runWriteTool(context.Background(), repo.ID, "run-2", r.Dir, ai.ToolCall{ID: "c2", Name: "pull"})
	confirm2 := ev.wait(t, EventChatConfirm).data.(ConfirmEvent)
	if !strings.Contains(confirm2.Title, "(rebase)") {
		t.Fatalf("title = %q, want the strategy saved after the first call, not the one read then", confirm2.Title)
	}
	if err := a.ConfirmChatAction(repo.ID, confirm2.ConfirmID, false); err != nil {
		t.Fatal(err)
	}
}
