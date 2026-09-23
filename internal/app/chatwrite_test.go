package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"git-ui/internal/ai"
	"git-ui/internal/ai/agent"
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
