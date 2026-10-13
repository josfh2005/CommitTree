package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"git-ui/internal/cmdlog"
	"git-ui/internal/credprompt"
	"git-ui/internal/ops"
	"git-ui/internal/testrepo"
)

// credApp is newPlainApp plus a credential server whose UI goes through the
// app's own events, answered by answer (nil: fail the test on any request).
// The answering goroutine and the server end with the test.
func credApp(t *testing.T, answer *credprompt.Answer) (*App, *testrepo.Repo, string, *events) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("credential prompts are off on Windows")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	a, r, id := newPlainApp(t)
	ev := newEvents()
	a.ai.deps.Emit = ev.emit
	exe, _ := os.Executable()
	if err := WithCredentials(a, credprompt.Options{Exe: exe, Timeout: 10 * time.Second}); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			case e := <-ev.ch:
				if e.name != "credential:request" {
					continue
				}
				req := e.data.(credprompt.Request)
				if answer == nil {
					t.Errorf("unexpected credential request %+v", req)
					_ = a.CancelCredential(req.ID)
					continue
				}
				_ = a.AnswerCredential(req.ID, *answer)
			}
		}
	}()
	t.Cleanup(func() {
		close(stop)
		wg.Wait()
		a.creds.Shutdown()
	})
	return a, r, id, ev
}

func lockedOrigin(t *testing.T, r *testrepo.Repo, token string) {
	t.Helper()
	bare := testrepo.NewBareFrom(t, r)
	url, _ := testrepo.AuthServer(t, bare, "me", token)
	r.Git("remote", "add", "origin", url)
}

func TestFetchAsksForCredentials(t *testing.T) {
	a, r, id, ev := credApp(t, &credprompt.Answer{Username: "me", Secret: "tok"})
	lockedOrigin(t, r, "tok")
	if err := a.Fetch(id); err != nil {
		t.Fatal(err)
	}
	req := ev.find(t, "credential:request").data.(credprompt.Request)
	if req.Context != "Fetch · "+filepath.Base(r.Dir) || req.RepoID != id || req.FromAI {
		t.Fatalf("request %+v", req)
	}
}

func TestAutoFetchNeverAsks(t *testing.T) {
	a, r, id, _ := credApp(t, nil)
	lockedOrigin(t, r, "tok")
	res, err := a.AutoFetch(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.AuthFailed) != 1 || res.AuthFailed[0] != "origin" {
		t.Fatalf("AuthFailed %v", res.AuthFailed)
	}
}

func TestCredentialBindingsWithoutAServer(t *testing.T) {
	a, _, _ := newPlainApp(t)
	if err := a.AnswerCredential("x", credprompt.Answer{}); err != credprompt.ErrUnknownRequest {
		t.Fatalf("AnswerCredential = %v", err)
	}
	if err := a.CancelCredential("x"); err != credprompt.ErrUnknownRequest {
		t.Fatalf("CancelCredential = %v", err)
	}
}

func TestRemoteTimeoutOutlastsDialog(t *testing.T) {
	if ops.RemoteTestTimeout <= credprompt.DefaultTimeout {
		t.Fatalf("Test's %v must outlast the %v dialog", ops.RemoteTestTimeout, credprompt.DefaultTimeout)
	}
}

func TestSecretNeverLeaves(t *testing.T) {
	sentinel := fmt.Sprintf("SENTINEL-%d", time.Now().UnixNano())
	a, r, id, ev := credApp(t, &credprompt.Answer{Username: "me", Secret: sentinel})
	note := &fakeNotifier{authorized: true}
	a.startNotifications(note)
	lockedOrigin(t, r, sentinel)
	r.Commit("more")
	branch := r.Git("rev-parse", "--abbrev-ref", "HEAD")
	res, err := a.PushBranch(id, branch)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != ops.PushPushed {
		t.Fatalf("push did not succeed: %+v", res)
	}
	ev.find(t, "credential:request")

	dir, _ := a.dir(id)
	entries := a.cmds.List(cmdlog.RepoKey(dir))
	if len(entries) == 0 {
		t.Fatal("no command was logged")
	}
	for _, e := range entries {
		b, _ := json.Marshal(e)
		if strings.Contains(string(b), sentinel) {
			t.Fatalf("command log entry holds the secret: %s", b)
		}
		out, _ := a.cmds.Output(cmdlog.RepoKey(dir), e.ID)
		if b, _ := json.Marshal(out); strings.Contains(string(b), sentinel) {
			t.Fatalf("command output holds the secret: %s", b)
		}
	}
	ev.mu.Lock()
	defer ev.mu.Unlock()
	for _, e := range ev.list {
		b, _ := json.Marshal(e.data)
		if strings.Contains(string(b), sentinel) {
			t.Fatalf("event %s holds the secret: %s", e.name, b)
		}
	}
	b, _ := json.Marshal([]any{note.sent, note.sentWithActions})
	if strings.Contains(string(b), sentinel) {
		t.Fatalf("a notification holds the secret: %s", b)
	}
}
