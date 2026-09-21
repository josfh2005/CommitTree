// Package app is the API the frontend calls through Wails bindings.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"git-ui/internal/gitlog"
	"git-ui/internal/graph"
	"git-ui/internal/ops"
	"git-ui/internal/refs"
	"git-ui/internal/repos"
)

var (
	ErrBusy      = errors.New("another operation is already running on this repository")
	ErrStalePage = errors.New("log page is stale; reload from the start")
)

type RepoItem struct {
	repos.Repo
	Branch string `json:"branch"`
}

type LogRow struct {
	gitlog.Commit
	Lane    int          `json:"lane"`
	Color   int          `json:"color"`
	Edges   []graph.Edge `json:"edges"`
	IsMerge bool         `json:"isMerge"`
	IsHead  bool         `json:"isHead"`
}

type LogPage struct {
	Rows         []LogRow `json:"rows"`
	HasMore      bool     `json:"hasMore"`
	GraphVisible bool     `json:"graphVisible"`
}

type logState struct {
	key    string
	next   int
	layout *graph.Layout
}

type App struct {
	ctx    context.Context
	store  *repos.Store
	mu     sync.Mutex
	logs   map[string]*logState
	writes sync.Map // repo ID → *sync.Mutex
	ai     *aiState
}

func New(store *repos.Store) *App {
	return &App{ctx: context.Background(), store: store, logs: map[string]*logState{}}
}

// Startup receives the Wails runtime context.
func (a *App) Startup(ctx context.Context) { a.ctx = ctx }

func (a *App) dir(id string) (string, error) {
	r, ok := a.store.Get(id)
	if !ok {
		return "", repos.ErrUnknownRepo
	}
	return r.Path, nil
}

// ---- Repos ----

func (a *App) ListRepos() []RepoItem {
	list := a.store.List()
	items := make([]RepoItem, len(list))
	for i, r := range list {
		items[i] = RepoItem{Repo: r}
		if !r.Missing {
			items[i].Branch = refs.CurrentLabel(a.ctx, r.Path)
		}
	}
	return items
}

func (a *App) AddRepo() (repos.Repo, error) {
	path, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Add repository"})
	if err != nil || path == "" {
		return repos.Repo{}, err
	}
	return a.store.Add(a.ctx, path)
}

func (a *App) RelocateRepo(id string) (repos.Repo, error) {
	path, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Locate repository"})
	if err != nil || path == "" {
		return repos.Repo{}, err
	}
	a.forgetLog(id)
	return a.store.Relocate(a.ctx, id, path)
}

func (a *App) RemoveRepo(id string) error {
	a.forgetLog(id)
	return a.store.Remove(id)
}

func (a *App) forgetLog(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.logs, id)
}

// ---- Reads ----

func (a *App) GetRefs(id string) (refs.Refs, error) {
	dir, err := a.dir(id)
	if err != nil {
		return refs.Refs{}, err
	}
	return refs.List(a.ctx, dir)
}

func (a *App) GetLog(id string, filters gitlog.Filters, offset, limit int) (LogPage, error) {
	dir, err := a.dir(id)
	if err != nil {
		return LogPage{}, err
	}
	keyBytes, err := json.Marshal(filters)
	if err != nil {
		return LogPage{}, err
	}
	key := string(keyBytes)

	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.logs[id]
	if offset == 0 {
		st = &logState{key: key, layout: graph.New()}
		a.logs[id] = st
	} else if st == nil || st.key != key || st.next != offset {
		return LogPage{}, ErrStalePage
	}

	commits, err := gitlog.Get(a.ctx, dir, filters, offset, limit)
	if err != nil {
		return LogPage{}, err
	}
	page := LogPage{
		Rows:         make([]LogRow, len(commits)),
		HasMore:      len(commits) == limit,
		GraphVisible: filters.GraphVisible(),
	}
	var layout []graph.Row
	if page.GraphVisible {
		nodes := make([]graph.Node, len(commits))
		for i, c := range commits {
			nodes[i] = graph.Node{Hash: c.Hash, Parents: c.Parents}
		}
		layout = st.layout.Add(nodes)
	}
	for i, c := range commits {
		row := LogRow{Commit: c, Edges: []graph.Edge{}, IsMerge: len(c.Parents) > 1, IsHead: c.IsHead()}
		if layout != nil {
			row.Lane, row.Color, row.Edges = layout[i].Lane, layout[i].Color, layout[i].Edges
		}
		page.Rows[i] = row
	}
	st.next = offset + len(commits)
	return page, nil
}

func (a *App) GetDetails(id, hash string) (gitlog.Details, error) {
	dir, err := a.dir(id)
	if err != nil {
		return gitlog.Details{}, err
	}
	return gitlog.GetDetails(a.ctx, dir, hash)
}

func (a *App) GetDiff(id, parent, hash string, paths []string) (string, error) {
	dir, err := a.dir(id)
	if err != nil {
		return "", err
	}
	return gitlog.Diff(a.ctx, dir, parent, hash, paths)
}

func (a *App) GetAuthors(id string) ([]string, error) {
	dir, err := a.dir(id)
	if err != nil {
		return nil, err
	}
	return gitlog.Authors(a.ctx, dir)
}

func (a *App) ResolveCommit(id, text string) (string, error) {
	dir, err := a.dir(id)
	if err != nil {
		return "", err
	}
	return gitlog.ResolveCommit(a.ctx, dir, text)
}

func (a *App) IsShallow(id string) (bool, error) {
	dir, err := a.dir(id)
	if err != nil {
		return false, err
	}
	return gitlog.IsShallow(a.ctx, dir)
}

func (a *App) Fingerprint(id string) (string, error) {
	dir, err := a.dir(id)
	if err != nil {
		return "", err
	}
	return refs.Fingerprint(a.ctx, dir)
}

// ---- Writes ----

func (a *App) write(id string, fn func(ctx context.Context, dir string) error) error {
	dir, err := a.dir(id)
	if err != nil {
		return err
	}
	m, _ := a.writes.LoadOrStore(id, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	if !mu.TryLock() {
		return ErrBusy
	}
	defer mu.Unlock()
	return fn(a.ctx, dir)
}

func (a *App) Checkout(id, branch string) error {
	return a.write(id, func(ctx context.Context, dir string) error { return ops.Checkout(ctx, dir, branch) })
}

func (a *App) CheckoutRemote(id, remote, name string) error {
	return a.write(id, func(ctx context.Context, dir string) error { return ops.CheckoutRemote(ctx, dir, remote, name) })
}

func (a *App) CheckoutDetached(id, hash string) error {
	return a.write(id, func(ctx context.Context, dir string) error { return ops.CheckoutDetached(ctx, dir, hash) })
}

// ResetBranch moves the current branch to hash; mode is soft, mixed or hard.
func (a *App) ResetBranch(id, hash, mode string) error {
	return a.write(id, func(ctx context.Context, dir string) error { return ops.Reset(ctx, dir, hash, ops.ResetMode(mode)) })
}

// GetResetPreview counts what ResetBranch to hash would undo, for its
// confirmation.
func (a *App) GetResetPreview(id, hash string) (ops.ResetInfo, error) {
	dir, err := a.dir(id)
	if err != nil {
		return ops.ResetInfo{}, err
	}
	return ops.ResetPreview(a.ctx, dir, hash)
}

func (a *App) CreateBranch(id, name, target string, checkout bool) error {
	return a.write(id, func(ctx context.Context, dir string) error {
		return refs.CreateBranch(ctx, dir, name, target, checkout)
	})
}

func (a *App) DeleteBranch(id, name string, force bool) error {
	return a.write(id, func(ctx context.Context, dir string) error { return refs.DeleteBranch(ctx, dir, name, force) })
}

func (a *App) DeleteRemoteBranch(id, remote, name string) error {
	return a.write(id, func(ctx context.Context, dir string) error {
		return refs.DeleteRemoteBranch(ctx, dir, remote, name)
	})
}

func (a *App) CreateTag(id, name, target, message string) error {
	return a.write(id, func(ctx context.Context, dir string) error {
		return refs.CreateTag(ctx, dir, name, target, message)
	})
}

func (a *App) DeleteTag(id, name string) error {
	return a.write(id, func(ctx context.Context, dir string) error { return refs.DeleteTag(ctx, dir, name) })
}
