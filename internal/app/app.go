// Package app is the API the frontend calls through Wails bindings.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"git-ui/internal/cmdlog"
	"git-ui/internal/gitcmd"
	"git-ui/internal/gitlog"
	"git-ui/internal/graph"
	"git-ui/internal/ops"
	"git-ui/internal/refs"
	"git-ui/internal/repos"
	"git-ui/internal/submodules"
	"git-ui/internal/terminal"
	"git-ui/internal/worktrees"
)

var (
	ErrBusy      = errors.New("another operation is already running on this repository")
	ErrStalePage = errors.New("log page is stale; reload from the start")
)

type RepoItem struct {
	repos.Repo
	Branch string `json:"branch"`
	// ParentID is the main repository this item is a linked worktree of,
	// when that repository is listed; "" for a top-level entry.
	ParentID string `json:"parentId,omitempty"`
	// Worktree marks a detected worktree: not a list entry, so it cannot be
	// removed, grouped or relocated.
	Worktree bool `json:"worktree,omitempty"`
	// Submodule marks a detected, initialised submodule: like Worktree, not
	// a list entry, nested under ParentID, which holds it at SubPath.
	Submodule bool `json:"submodule,omitempty"`
	// SubPath is Submodule's path relative to ParentID's repository,
	// slash-separated.
	SubPath string `json:"subPath,omitempty"`
	// SubmoduleCount is the number of submodules (initialised or not) this
	// item's own repository has, set on the item itself, not its children.
	SubmoduleCount int `json:"submoduleCount,omitempty"`
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
	// cmds is the log behind the Commands panel; aiWrites marks
	// repositories (by cmdlog.RepoKey) running an approved AI write.
	cmds     *cmdlog.Log
	aiWrites sync.Map
	// started is set by Startup; cmdEmit sends a logged command to the
	// frontend and is nil until then.
	started atomic.Bool
	cmdEmit func(cmdlog.Entry)
	// gitSettingsPath overrides gitsettings.DefaultPath() when set — empty
	// in production, a temp path in tests.
	gitSettingsPath string
	owedDrops       sync.Map // repo ID → stash index still to drop once resolved
	discards        sync.Map // repo ID → the last hunk/line discard's patch, for Undo
	term            *terminal.Manager
	// worktrees are the linked worktrees the last ListRepos detected, by id.
	// They are not list entries: nothing about them is stored, and the map
	// is replaced wholesale on every list read.
	wtMu      sync.Mutex
	worktrees map[string]repos.Repo
	// wtParent maps a detected worktree's id to its main repository's id,
	// replaced wholesale alongside worktrees on every list read. It is what
	// lets a worktree removal run `git worktree remove` from the main
	// repository's directory rather than the worktree's own.
	wtParent map[string]string
	// submodules are the initialised submodules the last ListRepos
	// detected, by id. Like worktrees, they are not list entries: nothing
	// about them is stored, and the map is replaced wholesale on every list
	// read.
	smMu       sync.Mutex
	submodules map[string]repos.Repo
}

func New(store *repos.Store) *App {
	a := &App{ctx: context.Background(), store: store, logs: map[string]*logState{}}
	a.term = terminal.NewManager(terminal.Callbacks{
		OnData:    func(tab, data string) { a.emit("terminal:data", TerminalData{tab, data}) },
		OnSettled: func(tab, repo string) { a.emit("terminal:settled", TerminalSettled{tab, repo}) },
		OnExit:    func(tab string, code int) { a.emit("terminal:exit", TerminalExit{tab, code}) },
	})
	a.cmds = cmdlog.New()
	// One recorder per process: the most recently created App owns it —
	// there is one App in the real application.
	gitcmd.SetRecorder(&gitcmd.Recorder{Begin: a.beginGit, End: a.recordGit})
	return a
}

// Startup receives the Wails runtime context.
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	a.cmdEmit = wailsCommandEmitter(ctx)
	a.started.Store(true)
}

func (a *App) dir(id string) (string, error) {
	r, ok := a.repo(id)
	if !ok {
		return "", repos.ErrUnknownRepo
	}
	return r.Path, nil
}

// repo resolves an id: a stored repository first, then a worktree the last
// ListRepos detected, then a submodule it detected. Every per-repository
// operation goes through here, so a worktree or a submodule works
// everywhere a repository does.
func (a *App) repo(id string) (repos.Repo, bool) {
	if r, ok := a.store.Get(id); ok {
		return r, true
	}
	a.wtMu.Lock()
	r, ok := a.worktrees[id]
	a.wtMu.Unlock()
	if ok {
		return r, true
	}
	a.smMu.Lock()
	defer a.smMu.Unlock()
	r, ok = a.submodules[id]
	return r, ok
}

// ---- Repos ----

func (a *App) ListRepos() []RepoItem {
	list := a.store.List()
	items := make([]RepoItem, len(list))
	byPath := map[string]int{}
	for i, r := range list {
		items[i] = RepoItem{Repo: r}
		if !r.Missing {
			items[i].Branch = refs.CurrentLabel(a.ctx, r.Path)
			byPath[canonical(r.Path)] = i
		}
	}

	// A stored repository that is a main working tree gets its linked
	// worktrees as children. git lists every worktree from any of them, so
	// a stored entry that is itself a linked worktree gets none — it is
	// nested under its main repository instead, when that one is listed.
	found := map[string]repos.Repo{}
	foundParent := map[string]string{}
	for _, r := range list {
		if r.Missing {
			continue
		}
		wts, err := worktrees.List(a.ctx, r.Path)
		if err != nil || len(wts) == 0 || canonical(wts[0].Path) != canonical(r.Path) {
			continue
		}
		for _, wt := range wts[1:] {
			if wt.Bare || wt.Prunable {
				continue
			}
			if _, err := os.Stat(wt.Path); err != nil {
				continue
			}
			if i, ok := byPath[canonical(wt.Path)]; ok {
				items[i].ParentID = r.ID
				continue
			}
			label := wt.Branch
			if label == "" {
				label = fmt.Sprintf("HEAD (%.7s)", wt.Head)
			}
			repo := repos.Repo{ID: repos.IDFor(wt.Path), Name: filepath.Base(wt.Path), Path: wt.Path}
			items = append(items, RepoItem{Repo: repo, Branch: label, ParentID: r.ID, Worktree: true})
			found[repo.ID] = repo
			foundParent[repo.ID] = r.ID
		}
	}
	a.wtMu.Lock()
	gone := []string{}
	for id := range a.worktrees {
		if _, ok := found[id]; !ok {
			gone = append(gone, id)
		}
	}
	a.worktrees = found
	a.wtParent = foundParent
	a.wtMu.Unlock()
	// A worktree that disappeared takes its shells and log paging with it,
	// the same as removing a repository from the list.
	for _, id := range gone {
		if _, stored := a.store.Get(id); !stored {
			a.term.CloseRepo(id)
			a.forgetLog(id)
		}
	}

	// Every item found so far (stored or a detected worktree), but not
	// itself a submodule, gets its own submodules detected: a count on the
	// item, plus a child item for each initialised one. Submodules are
	// recursive (submodules.List already flattens nested ones), so this
	// only scans the items present before this pass, never one it appends.
	n := len(items)
	foundSub := map[string]repos.Repo{}
	for i := 0; i < n; i++ {
		item := items[i]
		if item.Missing || item.Submodule || !submodules.HasAny(item.Path) {
			continue
		}
		list, err := submodules.List(a.ctx, item.Path)
		if err != nil {
			continue
		}
		items[i].SubmoduleCount = len(list)
		for _, s := range list {
			if !s.Initialised || !s.Configured {
				continue
			}
			abs := filepath.Join(item.Path, filepath.FromSlash(s.Path))
			if _, ok := byPath[canonical(abs)]; ok {
				// Already a stored repository of its own: no duplicate item.
				continue
			}
			repo := repos.Repo{ID: repos.IDFor(abs), Name: filepath.Base(abs), Path: abs}
			items = append(items, RepoItem{
				Repo:      repo,
				Branch:    refs.CurrentLabel(a.ctx, abs),
				ParentID:  item.ID,
				Submodule: true,
				SubPath:   s.Path,
			})
			foundSub[repo.ID] = repo
		}
	}
	a.smMu.Lock()
	goneSub := []string{}
	for id := range a.submodules {
		if _, ok := foundSub[id]; !ok {
			goneSub = append(goneSub, id)
		}
	}
	a.submodules = foundSub
	a.smMu.Unlock()
	// A submodule that disappeared (deinitialised, or its parent gone) takes
	// its shells and log paging with it, the same as a vanished worktree.
	for _, id := range goneSub {
		if _, stored := a.store.Get(id); !stored {
			a.term.CloseRepo(id)
			a.forgetLog(id)
		}
	}
	return items
}

// canonical resolves symlinks (macOS temp and home paths often differ only
// by /private) so the same directory compares equal however it was named.
func canonical(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return filepath.Clean(path)
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
	// Only list entries can be removed; a detected worktree is refused
	// before anything of its state (log paging, terminal tabs) is touched.
	if _, ok := a.store.Get(id); !ok {
		return repos.ErrUnknownRepo
	}
	a.forgetLog(id)
	a.term.CloseRepo(id)
	return a.store.Remove(id)
}

// SetRepoGroup assigns id to the named sidebar group, or clears it when
// group is "".
func (a *App) SetRepoGroup(id, group string) error {
	return a.store.SetGroup(id, group)
}

// RenameRepoGroup renames every repository in oldName to newName. See
// repos.Store.RenameGroup for the merge and no-op semantics.
func (a *App) RenameRepoGroup(oldName, newName string) error {
	return a.store.RenameGroup(oldName, newName)
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
	r, err := refs.List(a.ctx, dir)
	if err != nil {
		return r, err
	}
	// Mark local branches another worktree has checked out — including one
	// whose directory is gone but not yet pruned: git still refuses to check
	// the branch out or delete it. A failure to read worktrees just leaves
	// no markers.
	if wts, err := worktrees.List(a.ctx, dir); err == nil {
		here := canonical(dir)
		for i, b := range r.Local {
			for _, wt := range wts {
				if wt.Branch == b.Name && canonical(wt.Path) != here {
					r.Local[i].Worktree = wt.Path
					r.Local[i].WorktreeGone = wt.Prunable
				}
			}
		}
	}
	return r, nil
}

// GetLog reads a page of commit history. order selects the walk order
// (gitlog.OrderTopo or gitlog.OrderDate) — the caller passes it in, rather
// than this reading a stored preference itself, the same way a pull
// strategy is threaded through from a caller in remote.go. It is folded into
// the paging key below, so switching order — like changing a filter —
// invalidates whatever page was in flight and restarts the log from the
// first page instead of mixing pages laid out for one order into another.
func (a *App) GetLog(id string, filters gitlog.Filters, order string, offset, limit int) (LogPage, error) {
	dir, err := a.dir(id)
	if err != nil {
		return LogPage{}, err
	}
	keyBytes, err := json.Marshal(struct {
		Filters gitlog.Filters
		Order   string
	}{filters, order})
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

	commits, err := gitlog.Get(a.ctx, dir, filters, order, offset, limit)
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

// writeAll runs fn under every id's write lock at once (TryLock on each, in
// order), for an operation such as a submodule write that must hold both the
// parent repository's lock and the lock of each submodule it touches. Any id
// already busy fails the whole call with ErrBusy and releases whatever locks
// it had already acquired, so a failed call never leaves a lock held.
func (a *App) writeAll(ids []string, fn func(ctx context.Context) error) error {
	var held []*sync.Mutex
	defer func() {
		for _, mu := range held {
			mu.Unlock()
		}
	}()
	for _, id := range ids {
		m, _ := a.writes.LoadOrStore(id, &sync.Mutex{})
		mu := m.(*sync.Mutex)
		if !mu.TryLock() {
			return ErrBusy
		}
		held = append(held, mu)
	}
	return fn(a.ctx)
}

func (a *App) Checkout(id, branch string) error {
	return a.write(id, func(ctx context.Context, dir string) error { return ops.Checkout(ctx, dir, branch) })
}

// CheckoutRemote returns an ops.CheckoutOutcome as a plain string: Wails
// generates no TypeScript for named string types.
func (a *App) CheckoutRemote(id, remote, name string) (string, error) {
	var outcome ops.CheckoutOutcome
	err := a.write(id, func(ctx context.Context, dir string) (err error) {
		outcome, err = ops.CheckoutRemote(ctx, dir, remote, name)
		return err
	})
	return string(outcome), err
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
