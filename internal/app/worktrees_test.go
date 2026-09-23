package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"git-ui/internal/ai/agent"
	"git-ui/internal/gitlog"
	"git-ui/internal/ops"
	"git-ui/internal/repos"
)

func realPath(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// withWorktree adds a linked worktree on branch feature to the test repo.
func withWorktree(t *testing.T, a *App, id string) (dir, wt string) {
	t.Helper()
	dir, err := a.dir(id)
	if err != nil {
		t.Fatal(err)
	}
	wt = filepath.Join(realPath(t, t.TempDir()), "wt")
	if out, err := exec.Command("git", "-C", dir, "worktree", "add", "-q", wt, "feature").CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v %s", err, out)
	}
	return dir, wt
}

func TestListReposDetectsWorktrees(t *testing.T) {
	a, id := newTestApp(t)
	_, wt := withWorktree(t, a, id)

	items := a.ListRepos()
	if len(items) != 2 {
		t.Fatalf("items = %+v", items)
	}
	child := items[1]
	if child.ParentID != id || !child.Worktree || child.Name != "wt" || child.Branch != "feature" || child.ID != repos.IDFor(wt) {
		t.Fatalf("child = %+v", child)
	}
	got, err := a.dir(child.ID)
	if err != nil || got != wt {
		t.Fatalf("dir = %q, %v", got, err)
	}
	if _, err := a.GetLog(child.ID, gitlog.Filters{}, gitlog.OrderTopo, 0, 10); err != nil {
		t.Fatal(err)
	}
	if err := a.RemoveRepo(child.ID); !errors.Is(err, repos.ErrUnknownRepo) {
		t.Fatalf("remove detected worktree: %v", err)
	}
	if err := a.SetRepoGroup(child.ID, "g"); !errors.Is(err, repos.ErrUnknownRepo) {
		t.Fatalf("group detected worktree: %v", err)
	}

	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}
	if items := a.ListRepos(); len(items) != 1 {
		t.Fatalf("after removing the worktree: %+v", items)
	}
	if _, err := a.dir(child.ID); err == nil {
		t.Fatal("a vanished worktree must not resolve")
	}
}

func TestListReposNestsAHandAddedWorktree(t *testing.T) {
	a, id := newTestApp(t)
	_, wt := withWorktree(t, a, id)
	stored, err := a.store.Add(context.Background(), wt)
	if err != nil {
		t.Fatal(err)
	}
	items := a.ListRepos()
	if len(items) != 2 {
		t.Fatalf("items = %+v", items)
	}
	var nested RepoItem
	for _, it := range items {
		if it.ID == stored.ID {
			nested = it
		}
	}
	if nested.ParentID != id || nested.Worktree {
		t.Fatalf("nested = %+v", nested)
	}
}

func TestGetRefsMarksBranchesCheckedOutElsewhere(t *testing.T) {
	a, id := newTestApp(t)
	dir, wt := withWorktree(t, a, id)
	items := a.ListRepos()
	wtID := items[1].ID

	check := func(repoID string, want map[string]string) {
		t.Helper()
		r, err := a.GetRefs(repoID)
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range r.Local {
			if w, ok := want[b.Name]; ok && realOr(b.Worktree) != w {
				t.Errorf("%s: branch %s worktree = %q, want %q", repoID, b.Name, b.Worktree, w)
			}
		}
	}
	check(id, map[string]string{"feature": wt, "main": ""})
	check(wtID, map[string]string{"main": realPath(t, dir), "feature": ""})
}

func realOr(p string) string {
	if p == "" {
		return ""
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

func TestCheckoutOfABranchInAnotherWorktreeExplains(t *testing.T) {
	a, id := newTestApp(t)
	_, wt := withWorktree(t, a, id)
	err := a.Checkout(id, "feature")
	var elsewhere *ops.ErrCheckedOutElsewhere
	if !errors.As(err, &elsewhere) {
		t.Fatalf("err = %v", err)
	}
	if !strings.HasPrefix(err.Error(), "feature is checked out in another worktree (") || realOr(elsewhere.Path) != wt {
		t.Fatalf("err = %q path %q", err.Error(), elsewhere.Path)
	}
}

func TestChatResolvesADetectedWorktree(t *testing.T) {
	a, id, ev := newAIApp(t, fakeOllama(t, nil).URL)
	withWorktree(t, a, id)
	wtID := a.ListRepos()[1].ID
	if err := a.SendChat(wtID, "hi", "r1"); err != nil {
		t.Fatal(err)
	}
	ev.wait(t, agent.EventDone)
}

func TestBranchHeldByARemovedWorktreeIsStillMarked(t *testing.T) {
	a, id := newTestApp(t)
	_, wt := withWorktree(t, a, id)
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}
	r, err := a.GetRefs(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range r.Local {
		if b.Name == "feature" && (b.Worktree == "" || !b.WorktreeGone) {
			t.Fatalf("feature = %+v, want marked as held by a gone worktree", b)
		}
	}
	var elsewhere *ops.ErrCheckedOutElsewhere
	if err := a.Checkout(id, "feature"); !errors.As(err, &elsewhere) {
		t.Fatalf("checkout err = %v", err)
	}
}

func TestVanishedWorktreeLosesItsTerminals(t *testing.T) {
	a, id := newTestApp(t)
	// Terminal events need an emitter; the plain test app has none.
	WithAI(a, AIDeps{Emit: newEvents().emit})
	t.Cleanup(func() { a.Shutdown(nil) })
	_, wt := withWorktree(t, a, id)
	wtID := a.ListRepos()[1].ID
	tab, err := a.TerminalOpen(wtID, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}
	a.ListRepos()
	if err := a.TerminalWrite(tab, "x"); err == nil {
		t.Fatal("the vanished worktree's terminal tab is still open")
	}
}
