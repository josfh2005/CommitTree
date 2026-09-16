# git-ui Core Viewer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Wails desktop app that lists saved git repos in a Claude-style sidebar (with branches/remotes/tags), shows a JetBrains-style graph log with filters and commit details, runs checkout/fetch/pull/branch/tag operations, and reserves a collapsible chat panel.

**Architecture:** Go backend shells out to the `git` CLI through one runner (`internal/gitcmd`). Pure lane layout lives in `internal/graph`; log parsing in `internal/gitlog`; refs and write ops in `internal/refs` and `internal/ops`; the persisted repo list in `internal/repos`. `internal/app` exposes everything to the Svelte + TypeScript frontend via Wails bindings. The frontend renders the graph on a canvas with a virtualized row list.

**Tech Stack:** Go 1.26, Wails v2, Svelte 3 + TypeScript + Vite (Wails `svelte-ts` template), Vitest, Node 22 via nvm, git CLI (≥ 2.28).

**Spec:** `docs/superpowers/specs/2026-09-16-git-ui-design.md`

## Global Constraints

- Go module path: `git-ui`. Go 1.26.
- Frontend: Node 22 (`source ~/.nvm/nvm.sh && nvm use 22` before any npm/wails command).
- Wails CLI at `~/go/bin/wails`.
- All git calls go through `gitcmd.Run`, which sets `GIT_TERMINAL_PROMPT=0` and `LC_ALL=C`.
- Timeouts: `gitcmd.ReadTimeout` = 10 s for reads and local writes; `gitcmd.NetworkTimeout` = 5 min for fetch, pull, push.
- Repo list file: `os.UserConfigDir()/git-ui/repos.json`.
- One write operation per repo at a time; a second one returns `app.ErrBusy`.
- AI rule: nothing in this sub-project calls an AI provider. The chat panel is a placeholder.
- Visual style: Claude desktop app. All colors are CSS custom properties in `frontend/src/theme.css`, dark mode via `prefers-color-scheme`. Graph lane colors are the only saturated colors.
- Commit messages: Conventional Commits. **Never add a `Co-Authored-By` line.**
- TDD for all Go packages and frontend `lib/*.ts` logic.

## Deviations from the spec (decided while planning)

- `graph` takes `[]graph.Node{Hash, Parents}` and returns `[]graph.Row{Lane, Color, Edges}`; `internal/app` joins rows with commits into `app.LogRow`.
- Edge kinds are `Line`, `ArrowDown`, `ArrowUp`. Each edge is drawn from the previous row's center to this row's center; merge-in and fork-out are both just `Line` edges with `From != To`.
- The graph is hidden (rows get lane 0 and no edges) when the text, author, since or until filter is set, because those filters drop commits without rewriting parents. Branch and path filters keep the graph.
- The sidebar is the branch picker: clicking a ref sets the log's branch filter, shown as a removable chip in the filter bar (no separate Branch dropdown).

## File Structure

```
main.go                          Wails entry: opens repo store, runs app
internal/gitcmd/gitcmd.go        Run git with timeout/env, typed *Error
internal/testrepo/testrepo.go    Test helper: temp repos, bare remotes, clones
internal/repos/repos.go          Persisted repo list (add/remove/relocate/list)
internal/graph/graph.go          Streaming lane layout
internal/gitlog/log.go           Filters → args, parse log, resolve, authors, shallow
internal/gitlog/details.go       Commit details, changed files, file diff
internal/refs/list.go            List branches/remotes/tags, fingerprint, label
internal/refs/mutate.go          Create/delete branch, remote branch, tag
internal/ops/ops.go              Checkout variants, fetch, pull
internal/app/app.go              Wails-bound API, log paging state, write lock
frontend/src/theme.css           Design tokens + base styles
frontend/src/main.ts             Mount App
frontend/src/App.svelte          Three-column shell
frontend/src/lib/types.ts        TS mirrors of Go JSON
frontend/src/lib/geometry.ts     Graph pixel math (tested)
frontend/src/lib/format.ts       Relative dates (tested)
frontend/src/lib/api.ts          Typed wrapper over generated bindings
frontend/src/lib/stores.ts       App state + loaders
frontend/src/lib/ui.ts           Toasts, dialogs, context menu, clipboard
frontend/src/lib/actions.ts      User actions with confirms + refresh
frontend/src/components/Icon.svelte
frontend/src/components/Splitter.svelte
frontend/src/components/ContextMenu.svelte
frontend/src/components/DialogHost.svelte
frontend/src/components/Toasts.svelte
frontend/src/components/Sidebar.svelte
frontend/src/components/RepoRefs.svelte
frontend/src/components/ChatPanel.svelte
frontend/src/components/LogView.svelte
frontend/src/components/FilterBar.svelte
frontend/src/components/LogList.svelte
frontend/src/components/CommitDetails.svelte
```

---

### Task 1: Scaffold the Wails project

**Files:**
- Create: Wails `svelte-ts` template files at the repo root (`main.go`, `app.go`, `wails.json`, `go.mod`, `go.sum`, `frontend/**`, `build/**`)
- Modify: `frontend/package.json` (scripts, vitest)

**Interfaces:**
- Produces: a buildable Wails app with module `git-ui`; `npm test` runs Vitest; `npm run check` runs svelte-check.

- [ ] **Step 1: Install the Wails CLI and check the toolchain**

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
source ~/.nvm/nvm.sh && nvm use 22 && ~/go/bin/wails doctor
```
Expected: doctor reports Go, Node 22, npm and Xcode CLT as installed ("Your system is ready for Wails development"). Fix anything it reports missing before continuing.

- [ ] **Step 2: Generate the template into a temp dir and copy it in**

```bash
source ~/.nvm/nvm.sh && nvm use 22
TMP=$(mktemp -d)
(cd "$TMP" && ~/go/bin/wails init -n git-ui -t svelte-ts)
rsync -a --exclude .git --exclude .gitignore "$TMP/git-ui/" /Users/josfh/playground/git-ui/
cd /Users/josfh/playground/git-ui && go mod edit -module git-ui && go mod tidy
```
Expected: `main.go`, `app.go`, `wails.json`, `frontend/` exist; `.gitignore` is unchanged (still ignores `.qartez/`, `build/bin/`, `frontend/node_modules/`, `frontend/dist/`).

- [ ] **Step 3: Add Vitest and scripts**

```bash
source ~/.nvm/nvm.sh && nvm use 22
cd /Users/josfh/playground/git-ui/frontend
npm install
npm install -D vitest@0.34.6
npm pkg set scripts.test="vitest run" scripts.check="svelte-check --tsconfig ./tsconfig.json"
```
If `npm install` fails on Node 22 because of the template's old Vite, run `npm install -D vite@^4 @sveltejs/vite-plugin-svelte@^2` and retry.

- [ ] **Step 4: Build to verify**

```bash
source ~/.nvm/nvm.sh && nvm use 22
cd /Users/josfh/playground/git-ui && ~/go/bin/wails build
```
Expected: ends with "Built '.../build/bin/git-ui.app'".

- [ ] **Step 5: Commit**

```bash
cd /Users/josfh/playground/git-ui
git add -A
git commit -m "chore: scaffold Wails svelte-ts app"
```

---

### Task 2: git runner and test repo helper

**Files:**
- Create: `internal/gitcmd/gitcmd.go`
- Create: `internal/testrepo/testrepo.go`
- Test: `internal/gitcmd/gitcmd_test.go`

**Interfaces:**
- Produces:
  - `gitcmd.ReadTimeout`, `gitcmd.NetworkTimeout time.Duration`
  - `gitcmd.ErrTimeout error`
  - `type gitcmd.Error struct { Args []string; Stderr string; ExitCode int; Err error }` (implements `error`, `Unwrap`)
  - `gitcmd.Run(ctx context.Context, dir string, timeout time.Duration, args ...string) (string, error)` — returns stdout; on failure a `*gitcmd.Error`
  - `testrepo.New(t testing.TB) *testrepo.Repo` (branch `main`, user "Test User" <test@example.com>)
  - `testrepo.NewBareFrom(t testing.TB, src *testrepo.Repo) string` — path of a bare clone
  - `testrepo.Clone(t testing.TB, remote string) *testrepo.Repo`
  - `(*Repo).Dir string`, `(*Repo).Git(args ...string) string` (trimmed output, fails test on error), `(*Repo).Commit(msg string) string` (writes `file-<n>.txt`, returns HEAD hash), `(*Repo).WriteFile(name, content string)`

- [ ] **Step 1: Write the test helper**

`internal/testrepo/testrepo.go`:
```go
// Package testrepo builds throwaway git repositories for tests.
package testrepo

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var baseDate = time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC)

type Repo struct {
	t    testing.TB
	Dir  string
	tick int
}

// New creates an empty repository on branch main.
func New(t testing.TB) *Repo {
	t.Helper()
	r := &Repo{t: t, Dir: t.TempDir()}
	r.Git("init", "-q", "-b", "main")
	r.configure()
	return r
}

// NewBareFrom returns the path of a bare clone of src, usable as a remote.
func NewBareFrom(t testing.TB, src *Repo) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "remote.git")
	run(t, "", nil, "clone", "-q", "--bare", src.Dir, dir)
	return dir
}

// Clone clones remote into a new temp directory.
func Clone(t testing.TB, remote string) *Repo {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "clone")
	run(t, "", nil, "clone", "-q", remote, dir)
	r := &Repo{t: t, Dir: dir}
	r.configure()
	return r
}

// Git runs git in the repo and returns trimmed combined output.
func (r *Repo) Git(args ...string) string {
	r.t.Helper()
	return run(r.t, r.Dir, nil, args...)
}

// Commit writes a new file, commits it with msg and returns the HEAD hash.
// Commit dates increase by one minute per commit so ordering is stable.
func (r *Repo) Commit(msg string) string {
	r.t.Helper()
	r.tick++
	name := fmt.Sprintf("file-%d.txt", r.tick)
	r.WriteFile(name, msg+"\n")
	r.Git("add", name)
	date := baseDate.Add(time.Duration(r.tick) * time.Minute).Format(time.RFC3339)
	run(r.t, r.Dir, []string{"GIT_AUTHOR_DATE=" + date, "GIT_COMMITTER_DATE=" + date},
		"commit", "-q", "-m", msg)
	return r.Git("rev-parse", "HEAD")
}

func (r *Repo) WriteFile(name, content string) {
	r.t.Helper()
	path := filepath.Join(r.Dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *Repo) configure() {
	r.Git("config", "user.name", "Test User")
	r.Git("config", "user.email", "test@example.com")
	r.Git("config", "commit.gpgsign", "false")
	r.Git("config", "tag.gpgsign", "false")
}

func run(t testing.TB, dir string, env []string, args ...string) string {
	t.Helper()
	if dir != "" {
		args = append([]string{"-C", dir}, args...)
	}
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0", "GIT_EDITOR=true")
	cmd.Env = append(cmd.Env, env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}
```

- [ ] **Step 2: Write the failing tests**

`internal/gitcmd/gitcmd_test.go`:
```go
package gitcmd_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"git-ui/internal/gitcmd"
	"git-ui/internal/testrepo"
)

func TestRunReturnsStdout(t *testing.T) {
	r := testrepo.New(t)
	hash := r.Commit("first")

	out, err := gitcmd.Run(context.Background(), r.Dir, gitcmd.ReadTimeout, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != hash {
		t.Fatalf("got %q, want %q", out, hash)
	}
}

func TestRunReturnsTypedErrorWithStderr(t *testing.T) {
	r := testrepo.New(t)

	_, err := gitcmd.Run(context.Background(), r.Dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "nope")

	var gerr *gitcmd.Error
	if !errors.As(err, &gerr) {
		t.Fatalf("want *gitcmd.Error, got %T: %v", err, err)
	}
	if gerr.ExitCode == 0 {
		t.Fatal("want non-zero exit code")
	}
	if !strings.Contains(gerr.Stderr, "Needed a single revision") {
		t.Fatalf("stderr = %q", gerr.Stderr)
	}
	if !strings.Contains(gerr.Error(), "Needed a single revision") {
		t.Fatalf("Error() = %q", gerr.Error())
	}
}

func TestRunTimeout(t *testing.T) {
	r := testrepo.New(t)

	_, err := gitcmd.Run(context.Background(), r.Dir, time.Nanosecond, "status")

	if !errors.Is(err, gitcmd.ErrTimeout) {
		t.Fatalf("want ErrTimeout, got %v", err)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/gitcmd/`
Expected: FAIL — `undefined: gitcmd.Run` (package has no non-test files).

- [ ] **Step 4: Implement**

`internal/gitcmd/gitcmd.go`:
```go
// Package gitcmd runs the git CLI.
package gitcmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	ReadTimeout    = 10 * time.Second
	NetworkTimeout = 5 * time.Minute
)

var ErrTimeout = errors.New("git command timed out")

// Error is returned when git exits unsuccessfully.
type Error struct {
	Args     []string
	Stderr   string
	ExitCode int
	Err      error
}

func (e *Error) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if msg == "" {
		msg = e.Err.Error()
	}
	return fmt.Sprintf("git %s: %s", strings.Join(e.Args, " "), msg)
}

func (e *Error) Unwrap() error { return e.Err }

// Run executes git with args in dir and returns stdout. Prompts are disabled
// so missing credentials fail instead of hanging, and output is in English so
// callers can match messages.
func Run(ctx context.Context, dir string, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	if err := cmd.Run(); err != nil {
		gerr := &Error{Args: args, Stderr: stderr.String(), ExitCode: -1, Err: err}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			gerr.ExitCode = exitErr.ExitCode()
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			gerr.Err = ErrTimeout
		}
		return stdout.String(), gerr
	}
	return stdout.String(), nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/gitcmd/`
Expected: `ok  git-ui/internal/gitcmd`

- [ ] **Step 6: Commit**

```bash
git add internal/gitcmd internal/testrepo
git commit -m "feat: add git command runner and test repo helper"
```

---

### Task 3: Persisted repo list

**Files:**
- Create: `internal/repos/repos.go`
- Test: `internal/repos/repos_test.go`

**Interfaces:**
- Consumes: `gitcmd.Run`, `gitcmd.ReadTimeout`
- Produces:
  - `type repos.Repo struct { ID, Name, Path string; Missing bool }` (JSON `id`, `name`, `path`, `missing`)
  - `repos.ErrNotRepo`, `repos.ErrUnknownRepo`
  - `repos.DefaultPath() (string, error)`
  - `repos.Open(path string) (*repos.Store, error)`
  - `(*Store).List() []Repo` (Missing computed), `(*Store).Get(id string) (Repo, bool)`, `(*Store).Add(ctx, path string) (Repo, error)`, `(*Store).Relocate(ctx, id, path string) (Repo, error)`, `(*Store).Remove(id string) error`

- [ ] **Step 1: Write the failing tests**

`internal/repos/repos_test.go`:
```go
package repos_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"git-ui/internal/repos"
	"git-ui/internal/testrepo"
)

var ctx = context.Background()

func realPath(t *testing.T, p string) string {
	t.Helper()
	out, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestAddListRemovePersist(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("init")
	sub := filepath.Join(r.Dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "cfg", "repos.json")

	s, err := repos.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	added, err := s.Add(ctx, sub)
	if err != nil {
		t.Fatal(err)
	}
	top := realPath(t, r.Dir)
	if added.Path != top || added.Name != filepath.Base(top) || added.ID == "" {
		t.Fatalf("added = %+v, want path %s", added, top)
	}

	again, err := s.Add(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != added.ID || len(s.List()) != 1 {
		t.Fatalf("duplicate add created a new entry: %+v", s.List())
	}

	reopened, err := repos.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	list := reopened.List()
	if len(list) != 1 || list[0].ID != added.ID || list[0].Missing {
		t.Fatalf("reopened list = %+v", list)
	}

	if err := reopened.Remove(added.ID); err != nil {
		t.Fatal(err)
	}
	final, _ := repos.Open(file)
	if len(final.List()) != 0 {
		t.Fatalf("after remove = %+v", final.List())
	}
	if err := final.Remove("nope"); !errors.Is(err, repos.ErrUnknownRepo) {
		t.Fatalf("remove unknown: %v", err)
	}
}

func TestAddRejectsNonRepo(t *testing.T) {
	s, _ := repos.Open(filepath.Join(t.TempDir(), "repos.json"))
	_, err := s.Add(ctx, t.TempDir())
	if !errors.Is(err, repos.ErrNotRepo) {
		t.Fatalf("want ErrNotRepo, got %v", err)
	}
}

func TestListFlagsMissing(t *testing.T) {
	r := testrepo.New(t)
	s, _ := repos.Open(filepath.Join(t.TempDir(), "repos.json"))
	if _, err := s.Add(ctx, r.Dir); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(r.Dir); err != nil {
		t.Fatal(err)
	}
	if list := s.List(); !list[0].Missing {
		t.Fatalf("want missing, got %+v", list)
	}
}

func TestRelocateKeepsID(t *testing.T) {
	a := testrepo.New(t)
	b := testrepo.New(t)
	s, _ := repos.Open(filepath.Join(t.TempDir(), "repos.json"))
	added, _ := s.Add(ctx, a.Dir)

	moved, err := s.Relocate(ctx, added.ID, b.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if moved.ID != added.ID || moved.Path != realPath(t, b.Dir) {
		t.Fatalf("moved = %+v", moved)
	}
	got, ok := s.Get(added.ID)
	if !ok || got.Path != moved.Path {
		t.Fatalf("Get = %+v %v", got, ok)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/repos/`
Expected: FAIL — `undefined: repos.Open`.

- [ ] **Step 3: Implement**

`internal/repos/repos.go`:
```go
// Package repos stores the user's list of repositories.
package repos

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"git-ui/internal/gitcmd"
)

var (
	ErrNotRepo     = errors.New("not a git repository")
	ErrUnknownRepo = errors.New("unknown repository")
)

type Repo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Missing bool   `json:"missing"`
}

type Store struct {
	path  string
	mu    sync.Mutex
	repos []Repo
}

// DefaultPath is ~/Library/Application Support/git-ui/repos.json on macOS.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "git-ui", "repos.json"), nil
}

func Open(path string) (*Store, error) {
	s := &Store{path: path, repos: []Repo{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &s.repos); err != nil {
		return nil, fmt.Errorf("repos: parse %s: %w", path, err)
	}
	return s, nil
}

func (s *Store) List() []Repo {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Repo, len(s.repos))
	for i, r := range s.repos {
		r.Missing = !isRepo(r.Path)
		out[i] = r
	}
	return out
}

func (s *Store) Get(id string) (Repo, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.repos {
		if r.ID == id {
			return r, true
		}
	}
	return Repo{}, false
}

// Add registers the work tree containing path. Adding a repo twice returns
// the existing entry.
func (s *Store) Add(ctx context.Context, path string) (Repo, error) {
	top, err := toplevel(ctx, path)
	if err != nil {
		return Repo{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.repos {
		if r.Path == top {
			return r, nil
		}
	}
	r := Repo{ID: idFor(top), Name: filepath.Base(top), Path: top}
	s.repos = append(s.repos, r)
	if err := s.save(); err != nil {
		s.repos = s.repos[:len(s.repos)-1]
		return Repo{}, err
	}
	return r, nil
}

// Relocate points an existing entry at a new path, keeping its ID.
func (s *Store) Relocate(ctx context.Context, id, path string) (Repo, error) {
	top, err := toplevel(ctx, path)
	if err != nil {
		return Repo{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, r := range s.repos {
		if r.ID != id {
			continue
		}
		old := r
		r.Path, r.Name = top, filepath.Base(top)
		s.repos[i] = r
		if err := s.save(); err != nil {
			s.repos[i] = old
			return Repo{}, err
		}
		return r, nil
	}
	return Repo{}, ErrUnknownRepo
}

func (s *Store) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, r := range s.repos {
		if r.ID == id {
			old := s.repos
			s.repos = append(append([]Repo{}, old[:i]...), old[i+1:]...)
			if err := s.save(); err != nil {
				s.repos = old
				return err
			}
			return nil
		}
	}
	return ErrUnknownRepo
}

func (s *Store) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.repos, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func toplevel(ctx context.Context, path string) (string, error) {
	out, err := gitcmd.Run(ctx, path, gitcmd.ReadTimeout, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrNotRepo, path)
	}
	return strings.TrimSpace(out), nil
}

func isRepo(path string) bool {
	_, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil
}

func idFor(path string) string {
	sum := sha1.Sum([]byte(path))
	return hex.EncodeToString(sum[:])[:12]
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/repos/`
Expected: `ok  git-ui/internal/repos`

- [ ] **Step 5: Commit**

```bash
git add internal/repos
git commit -m "feat: add persisted repository list"
```

---
### Task 4: Graph lane layout

**Files:**
- Create: `internal/graph/graph.go`
- Test: `internal/graph/graph_test.go`

**Interfaces:**
- Produces:
  - `graph.MaxStraight = 30`
  - `type graph.Node struct { Hash string; Parents []string }`
  - `type graph.EdgeKind int` with `graph.Line = 0`, `graph.ArrowDown = 1`, `graph.ArrowUp = 2`
  - `type graph.Edge struct { From, To, Color int; Kind EdgeKind; Target string }` (JSON `from`, `to`, `color`, `kind`, `target,omitempty`). Drawn from the previous row's center at column `From` to this row's center at column `To`. `ArrowDown.Target` = hash the cut line leads to; `ArrowUp.Target` = hash of the commit the line came from.
  - `type graph.Row struct { Lane, Color int; Edges []Edge }` (JSON `lane`, `color`, `edges`; `Edges` is never nil)
  - `graph.New() *graph.Layout`; `(*Layout).Add(nodes []Node) []Row` — nodes in topological order (children first); state carries over between calls, so pages can be added one after another.

**Algorithm (for the implementer):** `lanes[j]` is the line leaving the previous row in column j, waiting for commit `want`. For commit C: lanes waiting for C are its targets; C sits in the leftmost target (inheriting its color) or the first free slot (new color). Every active lane emits edges into this row: targets draw from each of their `from` columns to C's column; other lanes draw straight down (or, after `MaxStraight` rows, one `ArrowDown` and then nothing until they arrive, where they draw an `ArrowUp`). Then targets are freed, C's first parent continues in C's column and color, and each extra parent joins a lane already waiting for it (adding C's column to its `from`) or takes a free slot with a new color. Slots freed on this row are not reused by extra parents on the same row. Trailing empty slots are trimmed.

- [ ] **Step 1: Write the failing tests**

`internal/graph/graph_test.go`:
```go
package graph_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"git-ui/internal/graph"
)

// nodes parses "hash:parent1,parent2" specs.
func nodes(specs ...string) []graph.Node {
	out := make([]graph.Node, 0, len(specs))
	for _, s := range specs {
		hash, parents, _ := strings.Cut(s, ":")
		n := graph.Node{Hash: hash, Parents: []string{}}
		if parents != "" {
			n.Parents = strings.Split(parents, ",")
		}
		out = append(out, n)
	}
	return out
}

func line(from, to, color int) graph.Edge {
	return graph.Edge{From: from, To: to, Color: color, Kind: graph.Line}
}

func row(lane, color int, edges ...graph.Edge) graph.Row {
	if edges == nil {
		edges = []graph.Edge{}
	}
	return graph.Row{Lane: lane, Color: color, Edges: edges}
}

func assertRows(t *testing.T, got, want []graph.Row) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d rows, want %d", len(got), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("row %d:\n got  %+v\n want %+v", i, got[i], want[i])
		}
	}
}

func hasEdge(r graph.Row, e graph.Edge) bool {
	for _, x := range r.Edges {
		if x == e {
			return true
		}
	}
	return false
}

func TestLinearHistory(t *testing.T) {
	got := graph.New().Add(nodes("c:b", "b:a", "a"))
	assertRows(t, got, []graph.Row{
		row(0, 0),
		row(0, 0, line(0, 0, 0)),
		row(0, 0, line(0, 0, 0)),
	})
}

func TestBranchAndMerge(t *testing.T) {
	got := graph.New().Add(nodes("M:B1,F1", "F1:B0", "B1:B0", "B0"))
	assertRows(t, got, []graph.Row{
		row(0, 0),
		row(1, 1, line(0, 0, 0), line(0, 1, 1)),
		row(0, 0, line(0, 0, 0), line(1, 1, 1)),
		row(0, 0, line(0, 0, 0), line(1, 0, 1)),
	})
}

func TestOctopusMerge(t *testing.T) {
	got := graph.New().Add(nodes("M:A,B,C", "A:R", "B:R", "C:R", "R"))
	assertRows(t, got, []graph.Row{
		row(0, 0),
		row(0, 0, line(0, 0, 0), line(0, 1, 1), line(0, 2, 2)),
		row(1, 1, line(0, 0, 0), line(1, 1, 1), line(2, 2, 2)),
		row(2, 2, line(0, 0, 0), line(1, 1, 1), line(2, 2, 2)),
		row(0, 0, line(0, 0, 0), line(1, 0, 1), line(2, 0, 2)),
	})
}

func TestDisjointHistoriesReuseLaneWithNewColor(t *testing.T) {
	got := graph.New().Add(nodes("X1:X0", "X0", "Y1:Y0", "Y0"))
	assertRows(t, got, []graph.Row{
		row(0, 0),
		row(0, 0, line(0, 0, 0)),
		row(0, 1),
		row(0, 1, line(0, 0, 1)),
	})
}

func TestMergeJoinsExistingLane(t *testing.T) {
	got := graph.New().Add(nodes("S:B", "M:A,B", "A:B", "B"))
	assertRows(t, got, []graph.Row{
		row(0, 0),
		row(1, 1, line(0, 0, 0)),
		row(1, 1, line(0, 0, 0), line(1, 0, 0), line(1, 1, 1)),
		row(0, 0, line(0, 0, 0), line(1, 0, 1)),
	})
}

func TestLongLaneIsCutWithArrows(t *testing.T) {
	specs := []string{"M:c1,F"}
	for i := 1; i <= 35; i++ {
		parent := fmt.Sprintf("c%d", i+1)
		if i == 35 {
			parent = "R"
		}
		specs = append(specs, fmt.Sprintf("c%d:%s", i, parent))
	}
	specs = append(specs, "F:R", "R")
	rows := graph.New().Add(nodes(specs...))

	if !hasEdge(rows[1], line(0, 1, 1)) {
		t.Fatalf("row 1: missing fork edge: %+v", rows[1].Edges)
	}
	for i := 2; i <= graph.MaxStraight; i++ {
		if !hasEdge(rows[i], line(1, 1, 1)) {
			t.Fatalf("row %d: missing straight line on lane 1: %+v", i, rows[i].Edges)
		}
	}
	arrowRow := graph.MaxStraight + 1
	down := graph.Edge{From: 1, To: 1, Color: 1, Kind: graph.ArrowDown, Target: "F"}
	if !hasEdge(rows[arrowRow], down) {
		t.Fatalf("row %d: missing arrow down: %+v", arrowRow, rows[arrowRow].Edges)
	}
	for i := arrowRow + 1; i <= 35; i++ {
		for _, e := range rows[i].Edges {
			if e.From == 1 || e.To == 1 {
				t.Fatalf("row %d: cut lane still drawn: %+v", i, e)
			}
		}
	}
	fRow := rows[36]
	up := graph.Edge{From: 1, To: 1, Color: 1, Kind: graph.ArrowUp, Target: "M"}
	if fRow.Lane != 1 || !hasEdge(fRow, up) {
		t.Fatalf("F row = %+v", fRow)
	}
	if len(rows[37].Edges) != 2 {
		t.Fatalf("root row edges = %+v", rows[37].Edges)
	}
}

func TestLayoutResumesAcrossPages(t *testing.T) {
	all := nodes("M:B1,F1", "F1:B0", "B1:B0", "B0")
	want := graph.New().Add(all)

	l := graph.New()
	got := append(l.Add(all[:2]), l.Add(all[2:])...)

	assertRows(t, got, want)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/graph/`
Expected: FAIL — `undefined: graph.Node`.

- [ ] **Step 3: Implement**

`internal/graph/graph.go`:
```go
// Package graph assigns commits to lanes and computes the line segments of a
// commit graph. It knows nothing about git: callers pass nodes in topological
// order (children before parents) and draw the returned rows.
package graph

// MaxStraight is how many rows a line may pass through before it is cut and
// drawn as a pair of arrows.
const MaxStraight = 30

type Node struct {
	Hash    string
	Parents []string
}

type EdgeKind int

const (
	Line EdgeKind = iota
	ArrowDown
	ArrowUp
)

// Edge is a segment from the previous row's center (column From) to this
// row's center (column To). Arrow edges name the commit they point toward.
type Edge struct {
	From   int      `json:"from"`
	To     int      `json:"to"`
	Color  int      `json:"color"`
	Kind   EdgeKind `json:"kind"`
	Target string   `json:"target,omitempty"`
}

type Row struct {
	Lane  int    `json:"lane"`
	Color int    `json:"color"`
	Edges []Edge `json:"edges"`
}

type lane struct {
	want   string // commit this line leads to
	source string // commit this line started at
	color  int
	from   []int // columns on the previous row that feed this line
	run    int   // rows passed without arriving
	hidden bool  // cut: slot reserved, nothing drawn
}

type Layout struct {
	lanes     []*lane
	nextColor int
}

func New() *Layout { return &Layout{} }

// Add lays out the next nodes, continuing from previous calls.
func (l *Layout) Add(nodes []Node) []Row {
	rows := make([]Row, 0, len(nodes))
	for _, n := range nodes {
		rows = append(rows, l.add(n))
	}
	return rows
}

func (l *Layout) add(n Node) Row {
	var targets []int
	for j, ln := range l.lanes {
		if ln != nil && ln.want == n.Hash {
			targets = append(targets, j)
		}
	}
	var col, color int
	if len(targets) > 0 {
		col, color = targets[0], l.lanes[targets[0]].color
	} else {
		col, color = l.freeSlot(nil), l.newColor()
	}

	edges := []Edge{}
	for j, ln := range l.lanes {
		if ln == nil {
			continue
		}
		switch {
		case ln.want == n.Hash && ln.hidden:
			edges = append(edges, Edge{From: col, To: col, Color: ln.color, Kind: ArrowUp, Target: ln.source})
		case ln.want == n.Hash:
			for _, f := range ln.from {
				edges = append(edges, Edge{From: f, To: col, Color: ln.color, Kind: Line})
			}
		case ln.hidden:
			// Cut line: the slot stays reserved but nothing is drawn.
		default:
			ln.run++
			if ln.run > MaxStraight {
				ln.hidden = true
				edges = append(edges, Edge{From: j, To: j, Color: ln.color, Kind: ArrowDown, Target: ln.want})
				continue
			}
			for _, f := range ln.from {
				edges = append(edges, Edge{From: f, To: j, Color: ln.color, Kind: Line})
			}
		}
	}

	freed := make(map[int]bool, len(targets))
	for _, j := range targets {
		l.lanes[j] = nil
		freed[j] = true
	}
	for j, ln := range l.lanes {
		if ln != nil && !ln.hidden {
			ln.from = []int{j}
		}
	}

	if len(n.Parents) > 0 {
		l.set(col, &lane{want: n.Parents[0], source: n.Hash, color: color, from: []int{col}})
		for _, p := range n.Parents[1:] {
			if p == n.Parents[0] {
				continue
			}
			if j := l.find(p); j >= 0 {
				ln := l.lanes[j]
				if ln.hidden {
					ln.hidden, ln.from = false, nil
				}
				ln.from = append(ln.from, col)
				ln.run = 0
				continue
			}
			l.set(l.freeSlot(freed), &lane{want: p, source: n.Hash, color: l.newColor(), from: []int{col}})
		}
	}
	l.trim()
	return Row{Lane: col, Color: color, Edges: edges}
}

func (l *Layout) freeSlot(exclude map[int]bool) int {
	for j, ln := range l.lanes {
		if ln == nil && !exclude[j] {
			return j
		}
	}
	return len(l.lanes)
}

func (l *Layout) set(j int, ln *lane) {
	for len(l.lanes) <= j {
		l.lanes = append(l.lanes, nil)
	}
	l.lanes[j] = ln
}

func (l *Layout) find(hash string) int {
	for j, ln := range l.lanes {
		if ln != nil && ln.want == hash {
			return j
		}
	}
	return -1
}

func (l *Layout) trim() {
	for len(l.lanes) > 0 && l.lanes[len(l.lanes)-1] == nil {
		l.lanes = l.lanes[:len(l.lanes)-1]
	}
}

func (l *Layout) newColor() int {
	c := l.nextColor
	l.nextColor++
	return c
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/graph/`
Expected: `ok  git-ui/internal/graph`. If a row assertion fails, re-trace it against the algorithm note above before changing expectations: the expected rows were derived by hand from that algorithm.

- [ ] **Step 5: Commit**

```bash
git add internal/graph
git commit -m "feat: add commit graph lane layout"
```

---

### Task 5: Log reading and parsing

**Files:**
- Create: `internal/gitlog/log.go`
- Test: `internal/gitlog/log_test.go`

**Interfaces:**
- Consumes: `gitcmd.Run`, `gitcmd.Error`, `gitcmd.ReadTimeout`, `testrepo.*`
- Produces:
  - `gitlog.Format` (the `--format` string)
  - `type gitlog.RefKind string` with `RefHead = "head"`, `RefLocal = "local"`, `RefRemote = "remote"`, `RefTag = "tag"`
  - `type gitlog.Ref struct { Name string; Kind RefKind }` (JSON `name`, `kind`)
  - `type gitlog.Commit struct { Hash, Short string; Parents []string; Author, Email string; Date time.Time; Subject string; Refs []Ref }` (JSON `hash`, `short`, `parents`, `author`, `email`, `date`, `subject`, `refs`; slices never nil)
  - `(Commit).IsHead() bool`
  - `type gitlog.Filters struct { Text, Branch, Author, Since, Until string; Paths []string }` (JSON `text`, `branch`, `author`, `since`, `until`, `paths`). `Branch` is any revision (the UI sends full ref names such as `refs/heads/main`); empty means all refs. `Since`/`Until` are `YYYY-MM-DD`.
  - `(Filters).GraphVisible() bool`
  - `gitlog.Args(f Filters, skip, limit int) []string`
  - `gitlog.Parse(out string) ([]Commit, error)`
  - `gitlog.Get(ctx, dir string, f Filters, skip, limit int) ([]Commit, error)`
  - `gitlog.ResolveCommit(ctx, dir, rev string) (string, error)` — full hash, or `""` with nil error when rev is not a commit
  - `gitlog.IsShallow(ctx, dir string) (bool, error)`
  - `gitlog.Authors(ctx, dir string) ([]string, error)` — most commits first

- [ ] **Step 1: Write the failing tests**

`internal/gitlog/log_test.go`:
```go
package gitlog_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"git-ui/internal/gitlog"
	"git-ui/internal/testrepo"
)

var ctx = context.Background()

func TestArgsDefaultsToAllRefs(t *testing.T) {
	got := gitlog.Args(gitlog.Filters{}, 0, 500)
	want := []string{"log", "--topo-order", "--decorate=full", "--format=" + gitlog.Format,
		"--skip=0", "-n500", "--all"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestArgsWithAllFilters(t *testing.T) {
	f := gitlog.Filters{Text: "fix", Branch: "refs/heads/develop", Author: "ana",
		Since: "2026-01-01", Until: "2026-02-01", Paths: []string{"a.go", "b/"}}
	got := gitlog.Args(f, 500, 500)
	want := []string{"log", "--topo-order", "--decorate=full", "--format=" + gitlog.Format,
		"--skip=500", "-n500", "--author=ana", "--grep=fix", "-i", "--fixed-strings",
		"--since=2026-01-01", "--until=2026-02-01",
		"--end-of-options", "refs/heads/develop", "--", "a.go", "b/"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestGraphVisible(t *testing.T) {
	cases := map[string]struct {
		f    gitlog.Filters
		want bool
	}{
		"none":   {gitlog.Filters{}, true},
		"branch": {gitlog.Filters{Branch: "refs/heads/x", Paths: []string{"a"}}, true},
		"text":   {gitlog.Filters{Text: "x"}, false},
		"author": {gitlog.Filters{Author: "x"}, false},
		"since":  {gitlog.Filters{Since: "2026-01-01"}, false},
		"until":  {gitlog.Filters{Until: "2026-01-01"}, false},
	}
	for name, c := range cases {
		if got := c.f.GraphVisible(); got != c.want {
			t.Errorf("%s: got %v", name, got)
		}
	}
}

func TestParse(t *testing.T) {
	out := "aaaa\x00aa\x00bbbb cccc\x00Ana\x00ana@x.io\x002026-03-01T10:00:00+01:00\x00" +
		"HEAD -> refs/heads/main, refs/remotes/origin/main, refs/remotes/origin/HEAD, tag: refs/tags/v1.0\x00" +
		"Merge: things ✓\x1e\n" +
		"bbbb\x00bb\x00\x00Bo\x00bo@x.io\x002026-02-01T10:00:00Z\x00\x00root\x1e\n"

	got, err := gitlog.Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d commits", len(got))
	}
	want0 := gitlog.Commit{
		Hash: "aaaa", Short: "aa", Parents: []string{"bbbb", "cccc"},
		Author: "Ana", Email: "ana@x.io",
		Date:    time.Date(2026, 3, 1, 10, 0, 0, 0, time.FixedZone("", 3600)),
		Subject: "Merge: things ✓",
		Refs: []gitlog.Ref{
			{Name: "HEAD", Kind: gitlog.RefHead},
			{Name: "main", Kind: gitlog.RefLocal},
			{Name: "origin/main", Kind: gitlog.RefRemote},
			{Name: "v1.0", Kind: gitlog.RefTag},
		},
	}
	if !got[0].Date.Equal(want0.Date) {
		t.Fatalf("date = %v", got[0].Date)
	}
	got[0].Date = want0.Date
	if !reflect.DeepEqual(got[0], want0) {
		t.Fatalf("got  %+v\nwant %+v", got[0], want0)
	}
	if !got[0].IsHead() || got[1].IsHead() {
		t.Fatal("IsHead wrong")
	}
	if len(got[1].Parents) != 0 || got[1].Parents == nil || len(got[1].Refs) != 0 || got[1].Refs == nil {
		t.Fatalf("root commit = %+v", got[1])
	}
}

func TestParseDetachedHead(t *testing.T) {
	out := "aaaa\x00aa\x00\x00A\x00a@x\x002026-01-01T00:00:00Z\x00HEAD, refs/heads/feat\x00s\x1e"
	got, err := gitlog.Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	want := []gitlog.Ref{{Name: "HEAD", Kind: gitlog.RefHead}, {Name: "feat", Kind: gitlog.RefLocal}}
	if !reflect.DeepEqual(got[0].Refs, want) {
		t.Fatalf("refs = %+v", got[0].Refs)
	}
}

func TestGetReadsRealRepo(t *testing.T) {
	r := testrepo.New(t)
	base := r.Commit("base")
	r.Git("switch", "-q", "-c", "feature")
	feat := r.Commit("feature work")
	r.Git("switch", "-q", "main")
	r.Commit("main work")
	r.Git("merge", "-q", "--no-ff", "-m", "Merge feature", "feature")
	r.Git("tag", "v1.0")

	all, err := gitlog.Get(ctx, r.Dir, gitlog.Filters{}, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 {
		t.Fatalf("got %d commits", len(all))
	}
	top := all[0]
	if top.Subject != "Merge feature" || len(top.Parents) != 2 || !top.IsHead() {
		t.Fatalf("top = %+v", top)
	}
	wantRefs := []gitlog.Ref{
		{Name: "HEAD", Kind: gitlog.RefHead},
		{Name: "main", Kind: gitlog.RefLocal},
		{Name: "v1.0", Kind: gitlog.RefTag},
	}
	if !reflect.DeepEqual(top.Refs, wantRefs) {
		t.Fatalf("top refs = %+v", top.Refs)
	}
	if all[3].Hash != base {
		t.Fatalf("last = %s, want base %s", all[3].Hash, base)
	}

	page, err := gitlog.Get(ctx, r.Dir, gitlog.Filters{}, 2, 2)
	if err != nil || len(page) != 2 || page[0].Hash != all[2].Hash {
		t.Fatalf("page = %+v, err %v", page, err)
	}

	onlyFeature, err := gitlog.Get(ctx, r.Dir, gitlog.Filters{Branch: "refs/heads/feature"}, 0, 100)
	if err != nil || len(onlyFeature) != 2 || onlyFeature[0].Hash != feat {
		t.Fatalf("feature log = %+v, err %v", onlyFeature, err)
	}
}

func TestGetEmptyRepo(t *testing.T) {
	r := testrepo.New(t)
	got, err := gitlog.Get(ctx, r.Dir, gitlog.Filters{}, 0, 100)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %+v, err %v", got, err)
	}
}

func TestResolveCommit(t *testing.T) {
	r := testrepo.New(t)
	hash := r.Commit("one")

	got, err := gitlog.ResolveCommit(ctx, r.Dir, hash[:7])
	if err != nil || got != hash {
		t.Fatalf("got %q, err %v", got, err)
	}
	got, err = gitlog.ResolveCommit(ctx, r.Dir, "deadbeef")
	if err != nil || got != "" {
		t.Fatalf("unknown: got %q, err %v", got, err)
	}
}

func TestAuthorsAndShallow(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("one")

	authors, err := gitlog.Authors(ctx, r.Dir)
	if err != nil || !reflect.DeepEqual(authors, []string{"Test User"}) {
		t.Fatalf("authors = %q, err %v", authors, err)
	}
	shallow, err := gitlog.IsShallow(ctx, r.Dir)
	if err != nil || shallow {
		t.Fatalf("shallow = %v, err %v", shallow, err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/gitlog/`
Expected: FAIL — `undefined: gitlog.Args`.

- [ ] **Step 3: Implement**

`internal/gitlog/log.go`:
```go
// Package gitlog reads commit history.
package gitlog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"git-ui/internal/gitcmd"
)

// Format is the git log --format string: NUL-separated fields, each record
// terminated by an ASCII record separator.
const Format = "%H%x00%h%x00%P%x00%an%x00%ae%x00%aI%x00%D%x00%s%x1e"

type RefKind string

const (
	RefHead   RefKind = "head"
	RefLocal  RefKind = "local"
	RefRemote RefKind = "remote"
	RefTag    RefKind = "tag"
)

type Ref struct {
	Name string  `json:"name"`
	Kind RefKind `json:"kind"`
}

type Commit struct {
	Hash    string    `json:"hash"`
	Short   string    `json:"short"`
	Parents []string  `json:"parents"`
	Author  string    `json:"author"`
	Email   string    `json:"email"`
	Date    time.Time `json:"date"`
	Subject string    `json:"subject"`
	Refs    []Ref     `json:"refs"`
}

func (c Commit) IsHead() bool {
	for _, r := range c.Refs {
		if r.Kind == RefHead {
			return true
		}
	}
	return false
}

type Filters struct {
	Text   string   `json:"text"`
	Branch string   `json:"branch"`
	Author string   `json:"author"`
	Since  string   `json:"since"`
	Until  string   `json:"until"`
	Paths  []string `json:"paths"`
}

// GraphVisible reports whether a graph can be drawn. Text, author and date
// filters drop commits without rewriting parents, which would leave lines
// that never end.
func (f Filters) GraphVisible() bool {
	return f.Text == "" && f.Author == "" && f.Since == "" && f.Until == ""
}

func Args(f Filters, skip, limit int) []string {
	args := []string{"log", "--topo-order", "--decorate=full", "--format=" + Format,
		fmt.Sprintf("--skip=%d", skip), fmt.Sprintf("-n%d", limit)}
	if f.Author != "" {
		args = append(args, "--author="+f.Author)
	}
	if f.Text != "" {
		args = append(args, "--grep="+f.Text)
	}
	if f.Author != "" || f.Text != "" {
		args = append(args, "-i", "--fixed-strings")
	}
	if f.Since != "" {
		args = append(args, "--since="+f.Since)
	}
	if f.Until != "" {
		args = append(args, "--until="+f.Until)
	}
	if f.Branch == "" {
		args = append(args, "--all")
	} else {
		args = append(args, "--end-of-options", f.Branch)
	}
	if len(f.Paths) > 0 {
		args = append(args, "--")
		args = append(args, f.Paths...)
	}
	return args
}

func Parse(out string) ([]Commit, error) {
	commits := []Commit{}
	for _, rec := range strings.Split(out, "\x1e") {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		f := strings.Split(rec, "\x00")
		if len(f) != 8 {
			return nil, fmt.Errorf("gitlog: record has %d fields, want 8", len(f))
		}
		date, err := time.Parse(time.RFC3339, f[5])
		if err != nil {
			return nil, fmt.Errorf("gitlog: bad date %q: %w", f[5], err)
		}
		parents := strings.Fields(f[2])
		if parents == nil {
			parents = []string{}
		}
		commits = append(commits, Commit{
			Hash: f[0], Short: f[1], Parents: parents, Author: f[3], Email: f[4],
			Date: date, Subject: f[7], Refs: parseRefs(f[6]),
		})
	}
	return commits, nil
}

func parseRefs(decoration string) []Ref {
	refs := []Ref{}
	for _, part := range strings.Split(decoration, ", ") {
		if part == "" {
			continue
		}
		if part == "HEAD" {
			refs = append(refs, Ref{Name: "HEAD", Kind: RefHead})
			continue
		}
		if rest, ok := strings.CutPrefix(part, "HEAD -> "); ok {
			refs = append(refs, Ref{Name: "HEAD", Kind: RefHead})
			part = rest
		}
		part = strings.TrimPrefix(part, "tag: ")
		switch {
		case strings.HasPrefix(part, "refs/heads/"):
			refs = append(refs, Ref{Name: strings.TrimPrefix(part, "refs/heads/"), Kind: RefLocal})
		case strings.HasPrefix(part, "refs/tags/"):
			refs = append(refs, Ref{Name: strings.TrimPrefix(part, "refs/tags/"), Kind: RefTag})
		case strings.HasPrefix(part, "refs/remotes/"):
			name := strings.TrimPrefix(part, "refs/remotes/")
			if !strings.HasSuffix(name, "/HEAD") {
				refs = append(refs, Ref{Name: name, Kind: RefRemote})
			}
		}
	}
	return refs
}

func Get(ctx context.Context, dir string, f Filters, skip, limit int) ([]Commit, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, Args(f, skip, limit)...)
	if err != nil {
		return nil, err
	}
	return Parse(out)
}

func ResolveCommit(ctx context.Context, dir, rev string) (string, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout,
		"rev-parse", "--verify", "--quiet", "--end-of-options", rev+"^{commit}")
	var gerr *gitcmd.Error
	if errors.As(err, &gerr) && gerr.ExitCode == 1 {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func IsShallow(ctx context.Context, dir string) (bool, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--is-shallow-repository")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == "true", nil
}

func Authors(ctx context.Context, dir string) ([]string, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "shortlog", "-sn", "--all")
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, line := range strings.Split(out, "\n") {
		if _, name, ok := strings.Cut(strings.TrimSpace(line), "\t"); ok {
			names = append(names, name)
		}
	}
	return names, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/gitlog/`
Expected: `ok  git-ui/internal/gitlog`

- [ ] **Step 5: Commit**

```bash
git add internal/gitlog
git commit -m "feat: read and parse commit log with filters"
```

---

### Task 6: Commit details and diffs

**Files:**
- Create: `internal/gitlog/details.go`
- Test: `internal/gitlog/details_test.go`

**Interfaces:**
- Consumes: `gitlog.Get`, `gitlog.Commit`, `gitcmd.Run`
- Produces:
  - `type gitlog.FileChange struct { Status, Path, OldPath string }` (JSON `status`, `path`, `oldPath,omitempty`); `Status` is one letter: `A M D R C T`
  - `type gitlog.Details struct { Commit; Body, Committer, CommitterEmail string; CommitDate time.Time; Files []FileChange }` (JSON `body`, `committer`, `committerEmail`, `commitDate`, `files`; embedded commit fields are flattened)
  - `gitlog.GetDetails(ctx, dir, hash string) (Details, error)` — files are compared against the first parent (or the empty tree for a root commit)
  - `gitlog.Diff(ctx, dir, parent, hash string, paths []string) (string, error)` — `parent == ""` for a root commit
  - `gitlog.ParseNameStatus(out string) []FileChange`

- [ ] **Step 1: Write the failing tests**

`internal/gitlog/details_test.go`:
```go
package gitlog_test

import (
	"reflect"
	"strings"
	"testing"

	"git-ui/internal/gitlog"
	"git-ui/internal/testrepo"
)

func TestParseNameStatus(t *testing.T) {
	out := "M\x00a.go\x00R087\x00old.go\x00new.go\x00A\x00b c.txt\x00"
	want := []gitlog.FileChange{
		{Status: "M", Path: "a.go"},
		{Status: "R", Path: "new.go", OldPath: "old.go"},
		{Status: "A", Path: "b c.txt"},
	}
	if got := gitlog.ParseNameStatus(out); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	if got := gitlog.ParseNameStatus(""); got == nil || len(got) != 0 {
		t.Fatalf("empty = %#v", got)
	}
}

func TestGetDetailsAndDiff(t *testing.T) {
	r := testrepo.New(t)
	root := r.Commit("first")
	r.WriteFile("notes.md", "hello\n")
	r.Git("add", "notes.md")
	r.Git("mv", "file-1.txt", "renamed.txt")
	r.Git("commit", "-q", "-m", "subject line", "-m", "body text")
	head := r.Git("rev-parse", "HEAD")

	d, err := gitlog.GetDetails(ctx, r.Dir, head)
	if err != nil {
		t.Fatal(err)
	}
	if d.Subject != "subject line" || d.Body != "body text" || d.Committer != "Test User" {
		t.Fatalf("details = %+v", d)
	}
	wantFiles := []gitlog.FileChange{
		{Status: "A", Path: "notes.md"},
		{Status: "R", Path: "renamed.txt", OldPath: "file-1.txt"},
	}
	if !reflect.DeepEqual(d.Files, wantFiles) {
		t.Fatalf("files = %+v", d.Files)
	}

	rootDetails, err := gitlog.GetDetails(ctx, r.Dir, root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rootDetails.Files, []gitlog.FileChange{{Status: "A", Path: "file-1.txt"}}) {
		t.Fatalf("root files = %+v", rootDetails.Files)
	}

	patch, err := gitlog.Diff(ctx, r.Dir, root, head, []string{"notes.md"})
	if err != nil || !strings.Contains(patch, "+hello") {
		t.Fatalf("patch = %q, err %v", patch, err)
	}
	rootPatch, err := gitlog.Diff(ctx, r.Dir, "", root, []string{"file-1.txt"})
	if err != nil || !strings.Contains(rootPatch, "+first") {
		t.Fatalf("root patch = %q, err %v", rootPatch, err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/gitlog/ -run 'NameStatus|Details'`
Expected: FAIL — `undefined: gitlog.ParseNameStatus`.

- [ ] **Step 3: Implement**

`internal/gitlog/details.go`:
```go
package gitlog

import (
	"context"
	"fmt"
	"strings"
	"time"

	"git-ui/internal/gitcmd"
)

type FileChange struct {
	Status  string `json:"status"`
	Path    string `json:"path"`
	OldPath string `json:"oldPath,omitempty"`
}

type Details struct {
	Commit
	Body           string       `json:"body"`
	Committer      string       `json:"committer"`
	CommitterEmail string       `json:"committerEmail"`
	CommitDate     time.Time    `json:"commitDate"`
	Files          []FileChange `json:"files"`
}

const detailsFormat = "%cn%x00%ce%x00%cI%x00%b"

func GetDetails(ctx context.Context, dir, hash string) (Details, error) {
	commits, err := Get(ctx, dir, Filters{Branch: hash}, 0, 1)
	if err != nil {
		return Details{}, err
	}
	if len(commits) == 0 {
		return Details{}, fmt.Errorf("gitlog: commit %s not found", hash)
	}
	d := Details{Commit: commits[0]}

	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout,
		"show", "-s", "--format="+detailsFormat, "--end-of-options", d.Hash)
	if err != nil {
		return Details{}, err
	}
	f := strings.SplitN(out, "\x00", 4)
	if len(f) != 4 {
		return Details{}, fmt.Errorf("gitlog: unexpected details output for %s", hash)
	}
	d.Committer, d.CommitterEmail = f[0], f[1]
	if d.CommitDate, err = time.Parse(time.RFC3339, f[2]); err != nil {
		return Details{}, fmt.Errorf("gitlog: bad commit date %q: %w", f[2], err)
	}
	d.Body = strings.TrimSpace(f[3])

	args := []string{"diff-tree", "--no-commit-id", "-r", "--name-status", "-z", "-M"}
	if len(d.Parents) == 0 {
		args = append(args, "--root", d.Hash)
	} else {
		args = append(args, d.Parents[0], d.Hash)
	}
	names, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, args...)
	if err != nil {
		return Details{}, err
	}
	d.Files = ParseNameStatus(names)
	return d, nil
}

// ParseNameStatus parses `--name-status -z` output.
func ParseNameStatus(out string) []FileChange {
	parts := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	files := []FileChange{}
	for i := 0; i < len(parts); {
		status := parts[i]
		if status == "" {
			i++
			continue
		}
		kind := status[:1]
		if (kind == "R" || kind == "C") && i+2 < len(parts) {
			files = append(files, FileChange{Status: kind, OldPath: parts[i+1], Path: parts[i+2]})
			i += 3
			continue
		}
		if i+1 >= len(parts) {
			break
		}
		files = append(files, FileChange{Status: kind, Path: parts[i+1]})
		i += 2
	}
	return files
}

// Diff returns the patch for paths in hash compared with parent; parent is
// empty for a root commit.
func Diff(ctx context.Context, dir, parent, hash string, paths []string) (string, error) {
	var args []string
	if parent == "" {
		args = []string{"show", "--format=", "--no-color", "--end-of-options", hash}
	} else {
		args = []string{"diff", "--no-color", "-M", "--end-of-options", parent, hash}
	}
	args = append(args, "--")
	args = append(args, paths...)
	return gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, args...)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/gitlog/`
Expected: `ok  git-ui/internal/gitlog`

- [ ] **Step 5: Commit**

```bash
git add internal/gitlog
git commit -m "feat: load commit details, changed files and diffs"
```

---

### Task 7: List refs

**Files:**
- Create: `internal/refs/list.go`
- Test: `internal/refs/list_test.go`

**Interfaces:**
- Consumes: `gitcmd.Run`, `gitcmd.Error`, `testrepo.*`
- Produces:
  - `type refs.Branch struct { Name, Remote, Hash string; Current bool; Upstream string }` (JSON `name`, `remote`, `hash`, `current`, `upstream`). `Remote` is `""` for local branches; for `origin/feature/x` it is `Remote: "origin", Name: "feature/x"`.
  - `type refs.Remote struct { Name string; Branches []Branch }` (JSON `name`, `branches`)
  - `type refs.Tag struct { Name, Hash string }` (JSON `name`, `hash`; `Hash` is the peeled commit)
  - `type refs.Refs struct { Head, HeadHash string; Detached bool; Local []Branch; Remotes []Remote; Tags []Tag }` (JSON `head`, `headHash`, `detached`, `local`, `remotes`, `tags`; slices never nil)
  - `refs.List(ctx, dir string) (Refs, error)`
  - `refs.CurrentLabel(ctx, dir string) string` — branch name, short hash when detached, `""` for an empty repo or on error
  - `refs.Fingerprint(ctx, dir string) (string, error)` — changes whenever any ref or HEAD changes

- [ ] **Step 1: Write the failing tests**

`internal/refs/list_test.go`:
```go
package refs_test

import (
	"context"
	"reflect"
	"testing"

	"git-ui/internal/refs"
	"git-ui/internal/testrepo"
)

var ctx = context.Background()

func TestListLocalRemotesTags(t *testing.T) {
	src := testrepo.New(t)
	first := src.Commit("first")
	src.Git("branch", "feature/x")
	src.Git("tag", "-a", "v1.0", "-m", "release")
	src.Git("tag", "light")
	r := testrepo.Clone(t, testrepo.NewBareFrom(t, src))

	got, err := refs.List(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Head != "main" || got.Detached || got.HeadHash != first {
		t.Fatalf("head = %q detached=%v hash=%q", got.Head, got.Detached, got.HeadHash)
	}
	wantLocal := []refs.Branch{{Name: "main", Hash: first, Current: true, Upstream: "origin/main"}}
	if !reflect.DeepEqual(got.Local, wantLocal) {
		t.Fatalf("local = %+v", got.Local)
	}
	wantRemotes := []refs.Remote{{Name: "origin", Branches: []refs.Branch{
		{Name: "feature/x", Remote: "origin", Hash: first},
		{Name: "main", Remote: "origin", Hash: first},
	}}}
	if !reflect.DeepEqual(got.Remotes, wantRemotes) {
		t.Fatalf("remotes = %+v", got.Remotes)
	}
	wantTags := []refs.Tag{{Name: "light", Hash: first}, {Name: "v1.0", Hash: first}}
	if !reflect.DeepEqual(got.Tags, wantTags) {
		t.Fatalf("tags = %+v", got.Tags)
	}
}

func TestListDetached(t *testing.T) {
	r := testrepo.New(t)
	a := r.Commit("a")
	r.Commit("b")
	r.Git("switch", "-q", "--detach", a)

	got, err := refs.List(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Detached || got.Head != "" || got.HeadHash != a {
		t.Fatalf("got %+v", got)
	}
	if label := refs.CurrentLabel(ctx, r.Dir); label != a[:7] {
		t.Fatalf("label = %q", label)
	}
}

func TestListEmptyRepo(t *testing.T) {
	r := testrepo.New(t)
	got, err := refs.List(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Head != "main" || got.HeadHash != "" || got.Detached || len(got.Local) != 0 || got.Local == nil {
		t.Fatalf("got %+v", got)
	}
}

func TestFingerprintAndLabel(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("a")
	if label := refs.CurrentLabel(ctx, r.Dir); label != "main" {
		t.Fatalf("label = %q", label)
	}

	before, err := refs.Fingerprint(ctx, r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	same, _ := refs.Fingerprint(ctx, r.Dir)
	if same != before {
		t.Fatal("fingerprint not stable")
	}
	r.Git("branch", "other")
	after, _ := refs.Fingerprint(ctx, r.Dir)
	if after == before {
		t.Fatal("fingerprint did not change after creating a branch")
	}
	r.Git("switch", "-q", "other")
	switched, _ := refs.Fingerprint(ctx, r.Dir)
	if switched == after {
		t.Fatal("fingerprint did not change after switching branch")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/refs/`
Expected: FAIL — `undefined: refs.List`.

- [ ] **Step 3: Implement**

`internal/refs/list.go`:
```go
// Package refs lists and edits branches and tags.
package refs

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"strings"

	"git-ui/internal/gitcmd"
)

type Branch struct {
	Name     string `json:"name"`
	Remote   string `json:"remote"`
	Hash     string `json:"hash"`
	Current  bool   `json:"current"`
	Upstream string `json:"upstream"`
}

type Remote struct {
	Name     string   `json:"name"`
	Branches []Branch `json:"branches"`
}

type Tag struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
}

type Refs struct {
	Head     string   `json:"head"`
	HeadHash string   `json:"headHash"`
	Detached bool     `json:"detached"`
	Local    []Branch `json:"local"`
	Remotes  []Remote `json:"remotes"`
	Tags     []Tag    `json:"tags"`
}

const refFormat = "%(refname)%00%(objectname)%00%(*objectname)%00%(HEAD)%00%(upstream:short)"

func List(ctx context.Context, dir string) (Refs, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout,
		"for-each-ref", "--format="+refFormat, "refs/heads", "refs/remotes", "refs/tags")
	if err != nil {
		return Refs{}, err
	}
	remoteOut, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "remote")
	if err != nil {
		return Refs{}, err
	}

	r := Refs{Local: []Branch{}, Remotes: []Remote{}, Tags: []Tag{}}
	remoteNames := strings.Fields(remoteOut)
	for _, name := range remoteNames {
		r.Remotes = append(r.Remotes, Remote{Name: name, Branches: []Branch{}})
	}

	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Split(line, "\x00")
		if len(f) != 5 {
			continue
		}
		ref, hash, peeled, head, upstream := f[0], f[1], f[2], f[3], f[4]
		switch {
		case strings.HasPrefix(ref, "refs/heads/"):
			r.Local = append(r.Local, Branch{Name: strings.TrimPrefix(ref, "refs/heads/"),
				Hash: hash, Current: head == "*", Upstream: upstream})
		case strings.HasPrefix(ref, "refs/tags/"):
			if peeled != "" {
				hash = peeled
			}
			r.Tags = append(r.Tags, Tag{Name: strings.TrimPrefix(ref, "refs/tags/"), Hash: hash})
		case strings.HasPrefix(ref, "refs/remotes/"):
			i, name := splitRemote(strings.TrimPrefix(ref, "refs/remotes/"), remoteNames)
			if i < 0 || name == "HEAD" {
				continue
			}
			r.Remotes[i].Branches = append(r.Remotes[i].Branches,
				Branch{Name: name, Remote: remoteNames[i], Hash: hash})
		}
	}

	head, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "symbolic-ref", "-q", "--short", "HEAD")
	var gerr *gitcmd.Error
	switch {
	case err == nil:
		r.Head = strings.TrimSpace(head)
	case errors.As(err, &gerr) && gerr.ExitCode == 1:
		r.Detached = true
	default:
		return Refs{}, err
	}
	if hash, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "--quiet", "HEAD"); err == nil {
		r.HeadHash = strings.TrimSpace(hash)
	}
	return r, nil
}

// splitRemote finds the remote a remote-tracking ref belongs to, preferring
// the longest matching remote name.
func splitRemote(rest string, remotes []string) (int, string) {
	best, name := -1, ""
	for i, remote := range remotes {
		if strings.HasPrefix(rest, remote+"/") && (best < 0 || len(remote) > len(remotes[best])) {
			best, name = i, strings.TrimPrefix(rest, remote+"/")
		}
	}
	return best, name
}

func CurrentLabel(ctx context.Context, dir string) string {
	if out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "symbolic-ref", "-q", "--short", "HEAD"); err == nil {
		return strings.TrimSpace(out)
	}
	if out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--short", "HEAD"); err == nil {
		return strings.TrimSpace(out)
	}
	return ""
}

func Fingerprint(ctx context.Context, dir string) (string, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "for-each-ref", "--format=%(refname) %(objectname)")
	if err != nil {
		return "", err
	}
	head, _ := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "symbolic-ref", "-q", "HEAD")
	hash, _ := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "--quiet", "HEAD")
	sum := sha1.Sum([]byte(out + "\x00" + head + "\x00" + hash))
	return hex.EncodeToString(sum[:]), nil
}
```

Note: `rev-parse --short HEAD` returns 7 characters in a small repo, which is what `TestListDetached` expects.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/refs/`
Expected: `ok  git-ui/internal/refs`

- [ ] **Step 5: Commit**

```bash
git add internal/refs
git commit -m "feat: list branches, remotes and tags"
```

---

### Task 8: Create and delete branches and tags

**Files:**
- Create: `internal/refs/mutate.go`
- Test: `internal/refs/mutate_test.go`

**Interfaces:**
- Consumes: `gitcmd.Run`, `gitcmd.Error`, `refs.List`, `testrepo.*`
- Produces:
  - `refs.ErrNotMerged`, `refs.ErrCurrentBranch`, `refs.ErrInvalidName`
  - `refs.CreateBranch(ctx, dir, name, target string, checkout bool) error`
  - `refs.DeleteBranch(ctx, dir, name string, force bool) error` — returns an error wrapping `ErrNotMerged` (message contains "not fully merged") when git refuses `-d`
  - `refs.DeleteRemoteBranch(ctx, dir, remote, name string) error`
  - `refs.CreateTag(ctx, dir, name, target, message string) error` — annotated when `message != ""`
  - `refs.DeleteTag(ctx, dir, name string) error`

- [ ] **Step 1: Write the failing tests**

`internal/refs/mutate_test.go`:
```go
package refs_test

import (
	"errors"
	"testing"

	"git-ui/internal/gitcmd"
	"git-ui/internal/refs"
	"git-ui/internal/testrepo"
)

func TestCreateBranchAndCheckout(t *testing.T) {
	r := testrepo.New(t)
	first := r.Commit("first")
	r.Commit("second")

	if err := refs.CreateBranch(ctx, r.Dir, "feature/a", first, true); err != nil {
		t.Fatal(err)
	}
	if head := r.Git("rev-parse", "HEAD"); head != first {
		t.Fatalf("HEAD = %s", head)
	}
	if name := r.Git("symbolic-ref", "--short", "HEAD"); name != "feature/a" {
		t.Fatalf("branch = %s", name)
	}
}

func TestCreateBranchRejectsInvalidName(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("first")
	for _, name := range []string{"bad..name", "-x", ""} {
		if err := refs.CreateBranch(ctx, r.Dir, name, "HEAD", false); !errors.Is(err, refs.ErrInvalidName) {
			t.Errorf("%q: want ErrInvalidName, got %v", name, err)
		}
	}
}

func TestDeleteBranchNotMergedThenForce(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("switch", "-q", "-c", "topic")
	r.Commit("topic work")
	r.Git("switch", "-q", "main")

	err := refs.DeleteBranch(ctx, r.Dir, "topic", false)
	if !errors.Is(err, refs.ErrNotMerged) {
		t.Fatalf("want ErrNotMerged, got %v", err)
	}
	if err := refs.DeleteBranch(ctx, r.Dir, "topic", true); err != nil {
		t.Fatal(err)
	}
	if _, err := gitcmd.Run(ctx, r.Dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "--quiet", "refs/heads/topic"); err == nil {
		t.Fatal("topic still exists")
	}
}

func TestDeleteCurrentBranchRefused(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	if err := refs.DeleteBranch(ctx, r.Dir, "main", true); !errors.Is(err, refs.ErrCurrentBranch) {
		t.Fatalf("want ErrCurrentBranch, got %v", err)
	}
}

func TestCreateAndDeleteTags(t *testing.T) {
	r := testrepo.New(t)
	h := r.Commit("base")

	if err := refs.CreateTag(ctx, r.Dir, "v1", h, ""); err != nil {
		t.Fatal(err)
	}
	if err := refs.CreateTag(ctx, r.Dir, "v2", h, "release notes"); err != nil {
		t.Fatal(err)
	}
	if kind := r.Git("cat-file", "-t", "v2"); kind != "tag" {
		t.Fatalf("v2 is %s, want annotated tag", kind)
	}
	if err := refs.CreateTag(ctx, r.Dir, "-bad", h, ""); !errors.Is(err, refs.ErrInvalidName) {
		t.Fatalf("want ErrInvalidName, got %v", err)
	}
	if err := refs.DeleteTag(ctx, r.Dir, "v1"); err != nil {
		t.Fatal(err)
	}
	list, _ := refs.List(ctx, r.Dir)
	if len(list.Tags) != 1 || list.Tags[0].Name != "v2" {
		t.Fatalf("tags = %+v", list.Tags)
	}
}

func TestDeleteRemoteBranch(t *testing.T) {
	src := testrepo.New(t)
	src.Commit("base")
	src.Git("branch", "old")
	bare := testrepo.NewBareFrom(t, src)
	r := testrepo.Clone(t, bare)

	if err := refs.DeleteRemoteBranch(ctx, r.Dir, "origin", "old"); err != nil {
		t.Fatal(err)
	}
	out, err := gitcmd.Run(ctx, bare, gitcmd.ReadTimeout, "branch", "--list", "old")
	if err != nil || out != "" {
		t.Fatalf("remote still has old: %q, err %v", out, err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/refs/ -run 'Create|Delete'`
Expected: FAIL — `undefined: refs.CreateBranch`.

- [ ] **Step 3: Implement**

`internal/refs/mutate.go`:
```go
package refs

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"git-ui/internal/gitcmd"
)

var (
	ErrNotMerged     = errors.New("branch is not fully merged")
	ErrCurrentBranch = errors.New("cannot delete the checked-out branch")
	ErrInvalidName   = errors.New("invalid ref name")
)

func CreateBranch(ctx context.Context, dir, name, target string, checkout bool) error {
	if err := validateBranch(ctx, dir, name); err != nil {
		return err
	}
	if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "branch", name, target); err != nil {
		return err
	}
	if checkout {
		_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "switch", name)
		return err
	}
	return nil
}

func DeleteBranch(ctx context.Context, dir, name string, force bool) error {
	if name == "" || strings.HasPrefix(name, "-") {
		return fmt.Errorf("%w: %q", ErrInvalidName, name)
	}
	if cur, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "symbolic-ref", "-q", "--short", "HEAD"); err == nil && strings.TrimSpace(cur) == name {
		return ErrCurrentBranch
	}
	flag := "-d"
	if force {
		flag = "-D"
	}
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "branch", flag, name)
	var gerr *gitcmd.Error
	if errors.As(err, &gerr) && strings.Contains(gerr.Stderr, "not fully merged") {
		return fmt.Errorf("%w: %s", ErrNotMerged, name)
	}
	return err
}

func DeleteRemoteBranch(ctx context.Context, dir, remote, name string) error {
	if name == "" || strings.HasPrefix(name, "-") || strings.HasPrefix(remote, "-") {
		return fmt.Errorf("%w: %s/%s", ErrInvalidName, remote, name)
	}
	_, err := gitcmd.Run(ctx, dir, gitcmd.NetworkTimeout, "push", remote, "--delete", name)
	return err
}

func CreateTag(ctx context.Context, dir, name, target, message string) error {
	if name == "" || strings.HasPrefix(name, "-") {
		return fmt.Errorf("%w: %q", ErrInvalidName, name)
	}
	if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "check-ref-format", "refs/tags/"+name); err != nil {
		return fmt.Errorf("%w: %q", ErrInvalidName, name)
	}
	args := []string{"tag", name, target}
	if message != "" {
		args = []string{"tag", "-a", name, "-m", message, target}
	}
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, args...)
	return err
}

func DeleteTag(ctx context.Context, dir, name string) error {
	if name == "" || strings.HasPrefix(name, "-") {
		return fmt.Errorf("%w: %q", ErrInvalidName, name)
	}
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "tag", "-d", name)
	return err
}

func validateBranch(ctx context.Context, dir, name string) error {
	if name == "" || strings.HasPrefix(name, "-") {
		return fmt.Errorf("%w: %q", ErrInvalidName, name)
	}
	if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "check-ref-format", "--branch", name); err != nil {
		return fmt.Errorf("%w: %q", ErrInvalidName, name)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/refs/`
Expected: `ok  git-ui/internal/refs`

- [ ] **Step 5: Commit**

```bash
git add internal/refs
git commit -m "feat: create and delete branches and tags"
```

---

### Task 9: Checkout, fetch and pull

**Files:**
- Create: `internal/ops/ops.go`
- Test: `internal/ops/ops_test.go`

**Interfaces:**
- Consumes: `gitcmd.Run`, `gitcmd.Error`, `testrepo.*`
- Produces:
  - `ops.ErrNotFastForward`, `ops.ErrInvalidRef`
  - `ops.Checkout(ctx, dir, branch string) error` — local branch
  - `ops.CheckoutRemote(ctx, dir, remote, name string) error` — switches to the local branch `name` if it exists, else creates it tracking `remote/name`
  - `ops.CheckoutDetached(ctx, dir, hash string) error`
  - `ops.Fetch(ctx, dir string) error` — `git fetch --all --prune`
  - `ops.Pull(ctx, dir string) error` — `git pull --ff-only`; wraps `ErrNotFastForward` when histories diverged
  - Git's own refusals (for example local changes that checkout would overwrite) come back as `*gitcmd.Error` with git's stderr.

- [ ] **Step 1: Write the failing tests**

`internal/ops/ops_test.go`:
```go
package ops_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"git-ui/internal/gitcmd"
	"git-ui/internal/ops"
	"git-ui/internal/testrepo"
)

var ctx = context.Background()

// clones returns two clones of a remote that has branches main and feature.
func clones(t *testing.T) (*testrepo.Repo, *testrepo.Repo) {
	t.Helper()
	src := testrepo.New(t)
	src.Commit("base")
	src.Git("branch", "feature")
	bare := testrepo.NewBareFrom(t, src)
	return testrepo.Clone(t, bare), testrepo.Clone(t, bare)
}

func TestFetchAndPullFastForward(t *testing.T) {
	a, b := clones(t)
	h := b.Commit("from b")
	b.Git("push", "-q", "origin", "main")

	if err := ops.Fetch(ctx, a.Dir); err != nil {
		t.Fatal(err)
	}
	if got := a.Git("rev-parse", "origin/main"); got != h {
		t.Fatalf("origin/main = %s, want %s", got, h)
	}
	if err := ops.Pull(ctx, a.Dir); err != nil {
		t.Fatal(err)
	}
	if got := a.Git("rev-parse", "HEAD"); got != h {
		t.Fatalf("HEAD = %s, want %s", got, h)
	}
}

func TestPullRefusesDivergedHistory(t *testing.T) {
	a, b := clones(t)
	b.Commit("from b")
	b.Git("push", "-q", "origin", "main")
	a.Commit("local only")

	err := ops.Pull(ctx, a.Dir)
	if !errors.Is(err, ops.ErrNotFastForward) {
		t.Fatalf("want ErrNotFastForward, got %v", err)
	}
}

func TestCheckoutRemoteCreatesTrackingBranch(t *testing.T) {
	a, _ := clones(t)

	if err := ops.CheckoutRemote(ctx, a.Dir, "origin", "feature"); err != nil {
		t.Fatal(err)
	}
	if name := a.Git("symbolic-ref", "--short", "HEAD"); name != "feature" {
		t.Fatalf("branch = %s", name)
	}
	if up := a.Git("rev-parse", "--abbrev-ref", "feature@{upstream}"); up != "origin/feature" {
		t.Fatalf("upstream = %s", up)
	}

	a.Git("switch", "-q", "main")
	if err := ops.CheckoutRemote(ctx, a.Dir, "origin", "feature"); err != nil {
		t.Fatalf("second checkout of existing local branch: %v", err)
	}
}

func TestCheckoutAndDetached(t *testing.T) {
	r := testrepo.New(t)
	first := r.Commit("first")
	r.Git("branch", "other")
	r.Commit("second")

	if err := ops.Checkout(ctx, r.Dir, "other"); err != nil {
		t.Fatal(err)
	}
	if head := r.Git("rev-parse", "HEAD"); head != first {
		t.Fatalf("HEAD = %s", head)
	}
	if err := ops.CheckoutDetached(ctx, r.Dir, first); err != nil {
		t.Fatal(err)
	}
	if _, err := gitcmd.Run(ctx, r.Dir, gitcmd.ReadTimeout, "symbolic-ref", "-q", "HEAD"); err == nil {
		t.Fatal("HEAD is not detached")
	}
	if err := ops.Checkout(ctx, r.Dir, "-f"); !errors.Is(err, ops.ErrInvalidRef) {
		t.Fatalf("want ErrInvalidRef, got %v", err)
	}
}

func TestCheckoutConflictReturnsGitError(t *testing.T) {
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("switch", "-q", "-c", "other")
	r.WriteFile("file-1.txt", "other\n")
	r.Git("commit", "-q", "-am", "change on other")
	r.Git("switch", "-q", "main")
	r.WriteFile("file-1.txt", "dirty\n")

	err := ops.Checkout(ctx, r.Dir, "other")
	var gerr *gitcmd.Error
	if !errors.As(err, &gerr) || !strings.Contains(gerr.Stderr, "would be overwritten") {
		t.Fatalf("want git overwrite error, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ops/`
Expected: FAIL — `undefined: ops.Fetch`.

- [ ] **Step 3: Implement**

`internal/ops/ops.go`:
```go
// Package ops runs checkout, fetch and pull.
package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"git-ui/internal/gitcmd"
)

var (
	ErrNotFastForward = errors.New("cannot fast-forward: local and remote history have diverged")
	ErrInvalidRef     = errors.New("invalid ref")
)

func Checkout(ctx context.Context, dir, branch string) error {
	if err := checkRef(branch); err != nil {
		return err
	}
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "switch", branch)
	return err
}

func CheckoutRemote(ctx context.Context, dir, remote, name string) error {
	if err := checkRef(remote); err != nil {
		return err
	}
	if err := checkRef(name); err != nil {
		return err
	}
	if _, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "rev-parse", "--verify", "--quiet", "refs/heads/"+name); err == nil {
		return Checkout(ctx, dir, name)
	}
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "switch", "-c", name, "--track", remote+"/"+name)
	return err
}

func CheckoutDetached(ctx context.Context, dir, hash string) error {
	if err := checkRef(hash); err != nil {
		return err
	}
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "switch", "--detach", hash)
	return err
}

func Fetch(ctx context.Context, dir string) error {
	_, err := gitcmd.Run(ctx, dir, gitcmd.NetworkTimeout, "fetch", "--all", "--prune")
	return err
}

func Pull(ctx context.Context, dir string) error {
	_, err := gitcmd.Run(ctx, dir, gitcmd.NetworkTimeout, "pull", "--ff-only")
	var gerr *gitcmd.Error
	if errors.As(err, &gerr) && strings.Contains(strings.ToLower(gerr.Stderr), "not possible to fast-forward") {
		return fmt.Errorf("%w\n%s", ErrNotFastForward, strings.TrimSpace(gerr.Stderr))
	}
	return err
}

func checkRef(ref string) error {
	if ref == "" || strings.HasPrefix(ref, "-") {
		return fmt.Errorf("%w: %q", ErrInvalidRef, ref)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ops/`
Expected: `ok  git-ui/internal/ops`

- [ ] **Step 5: Commit**

```bash
git add internal/ops
git commit -m "feat: checkout, fetch and fast-forward pull"
```

---

### Task 10: Wails-bound app API

**Files:**
- Create: `internal/app/app.go`
- Test: `internal/app/app_test.go`
- Modify: `main.go` (replace entirely)
- Delete: `app.go` (template)
- Generated: `frontend/wailsjs/**` (via `wails generate module`)

**Interfaces:**
- Consumes: everything from Tasks 2–9.
- Produces (all exported methods on `*app.App` become frontend bindings in `frontend/wailsjs/go/app/App`):
  - `app.New(store *repos.Store) *app.App`, `(*App).Startup(ctx context.Context)`
  - `app.ErrBusy`, `app.ErrStalePage` (message contains "stale")
  - `type app.RepoItem struct { repos.Repo; Branch string }` (JSON flattens repo + `branch`)
  - `type app.LogRow struct { gitlog.Commit; Lane, Color int; Edges []graph.Edge; IsMerge, IsHead bool }` (JSON flattens commit + `lane`, `color`, `edges`, `isMerge`, `isHead`)
  - `type app.LogPage struct { Rows []LogRow; HasMore, GraphVisible bool }` (JSON `rows`, `hasMore`, `graphVisible`)
  - Repos: `ListRepos() []RepoItem`, `AddRepo() (repos.Repo, error)` (zero Repo when the dialog is cancelled), `RelocateRepo(id string) (repos.Repo, error)`, `RemoveRepo(id string) error`
  - Reads: `GetRefs(id string) (refs.Refs, error)`, `GetLog(id string, filters gitlog.Filters, offset, limit int) (LogPage, error)` (offset 0 restarts the layout; any other offset must equal the rows already returned for the same filters, else `ErrStalePage`), `GetDetails(id, hash string) (gitlog.Details, error)`, `GetDiff(id, parent, hash string, paths []string) (string, error)`, `GetAuthors(id string) ([]string, error)`, `ResolveCommit(id, text string) (string, error)`, `IsShallow(id string) (bool, error)`, `Fingerprint(id string) (string, error)`
  - Writes (each holds the repo's write lock; `ErrBusy` if held): `Checkout(id, branch string)`, `CheckoutRemote(id, remote, name string)`, `CheckoutDetached(id, hash string)`, `Fetch(id string)`, `Pull(id string)`, `CreateBranch(id, name, target string, checkout bool)`, `DeleteBranch(id, name string, force bool)`, `DeleteRemoteBranch(id, remote, name string)`, `CreateTag(id, name, target, message string)`, `DeleteTag(id, name string)` — all return `error`

- [ ] **Step 1: Write the failing tests**

`internal/app/app_test.go`:
```go
package app

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"git-ui/internal/gitlog"
	"git-ui/internal/repos"
	"git-ui/internal/testrepo"
)

func newTestApp(t *testing.T) (*App, string) {
	t.Helper()
	store, err := repos.Open(filepath.Join(t.TempDir(), "repos.json"))
	if err != nil {
		t.Fatal(err)
	}
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("switch", "-q", "-c", "feature")
	r.Commit("feature work")
	r.Git("switch", "-q", "main")
	r.Commit("main work")
	r.Git("merge", "-q", "--no-ff", "-m", "Merge feature", "feature")
	repo, err := store.Add(context.Background(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	return New(store), repo.ID
}

func TestGetLogPagesMatchSinglePage(t *testing.T) {
	a, id := newTestApp(t)

	whole, err := a.GetLog(id, gitlog.Filters{}, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	first, err := a.GetLog(id, gitlog.Filters{}, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.GetLog(id, gitlog.Filters{}, 2, 100)
	if err != nil {
		t.Fatal(err)
	}

	if len(whole.Rows) != 4 || !whole.GraphVisible || whole.HasMore {
		t.Fatalf("whole = %+v", whole)
	}
	if !first.HasMore || second.HasMore {
		t.Fatalf("hasMore: first %v second %v", first.HasMore, second.HasMore)
	}
	got := append(first.Rows, second.Rows...)
	if !reflect.DeepEqual(got, whole.Rows) {
		t.Fatalf("paged rows differ:\n got  %+v\n want %+v", got, whole.Rows)
	}
	top := whole.Rows[0]
	if !top.IsMerge || !top.IsHead || top.Subject != "Merge feature" {
		t.Fatalf("top = %+v", top)
	}
}

func TestGetLogRejectsStalePage(t *testing.T) {
	a, id := newTestApp(t)
	if _, err := a.GetLog(id, gitlog.Filters{}, 0, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := a.GetLog(id, gitlog.Filters{Author: "Test"}, 2, 2); !errors.Is(err, ErrStalePage) {
		t.Fatalf("different filters: want ErrStalePage, got %v", err)
	}
	if _, err := a.GetLog(id, gitlog.Filters{}, 3, 2); !errors.Is(err, ErrStalePage) {
		t.Fatalf("wrong offset: want ErrStalePage, got %v", err)
	}
}

func TestGetLogHidesGraphForAuthorFilter(t *testing.T) {
	a, id := newTestApp(t)
	page, err := a.GetLog(id, gitlog.Filters{Author: "Test User"}, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if page.GraphVisible || len(page.Rows) != 4 {
		t.Fatalf("page = %+v", page)
	}
	for _, r := range page.Rows {
		if r.Lane != 0 || len(r.Edges) != 0 || r.Edges == nil {
			t.Fatalf("row has graph data: %+v", r)
		}
	}
}

func TestWriteRejectsConcurrentOperation(t *testing.T) {
	a, id := newTestApp(t)
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan error)
	go func() {
		done <- a.write(id, func(ctx context.Context, dir string) error {
			close(started)
			<-release
			return nil
		})
	}()
	<-started

	if err := a.Fetch(id); !errors.Is(err, ErrBusy) {
		t.Fatalf("want ErrBusy, got %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestCheckoutAndRefsThroughApp(t *testing.T) {
	a, id := newTestApp(t)
	if err := a.Checkout(id, "feature"); err != nil {
		t.Fatal(err)
	}
	got, err := a.GetRefs(id)
	if err != nil || got.Head != "feature" {
		t.Fatalf("refs = %+v, err %v", got, err)
	}
	items := a.ListRepos()
	if len(items) != 1 || items[0].Branch != "feature" {
		t.Fatalf("items = %+v", items)
	}
	if _, err := a.GetRefs("unknown"); !errors.Is(err, repos.ErrUnknownRepo) {
		t.Fatalf("unknown repo: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/app/`
Expected: FAIL — `undefined: New`.

- [ ] **Step 3: Implement the app**

`internal/app/app.go`:
```go
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

func (a *App) Fetch(id string) error {
	return a.write(id, func(ctx context.Context, dir string) error { return ops.Fetch(ctx, dir) })
}

func (a *App) Pull(id string) error {
	return a.write(id, func(ctx context.Context, dir string) error { return ops.Pull(ctx, dir) })
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/app/`
Expected: `ok  git-ui/internal/app`

- [ ] **Step 5: Wire `main.go` and remove the template `app.go`**

Delete the template file: `git rm app.go`

Replace `main.go`:
```go
package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"

	"git-ui/internal/app"
	"git-ui/internal/repos"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	path, err := repos.DefaultPath()
	if err != nil {
		log.Fatal(err)
	}
	store, err := repos.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	api := app.New(store)

	err = wails.Run(&options.App{
		Title:            "git-ui",
		Width:            1440,
		Height:           900,
		MinWidth:         960,
		MinHeight:        600,
		AssetServer:      &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: 249, G: 248, B: 246, A: 255},
		OnStartup:        api.Startup,
		Bind:             []interface{}{api},
		Mac: &mac.Options{
			TitleBar: mac.TitleBarHiddenInset(),
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
```

- [ ] **Step 6: Generate bindings and verify everything compiles**

```bash
source ~/.nvm/nvm.sh && nvm use 22
cd /Users/josfh/playground/git-ui
go vet ./... && go test ./...
~/go/bin/wails generate module
ls frontend/wailsjs/go/app/
```
Expected: all packages `ok`; `App.js` and `App.d.ts` listed. (The template's `frontend/wailsjs/go/main/` folder is stale now; delete it: `rm -rf frontend/wailsjs/go/main`.) The template `App.svelte` still imports `Greet` and will fail to build; that is fixed in Task 12, so don't run `wails build` here.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "feat: expose repos, log, refs and operations to the frontend"
```

---

### Task 11: Frontend types, graph geometry and date formatting

**Files:**
- Create: `frontend/src/lib/types.ts`
- Create: `frontend/src/lib/geometry.ts`
- Create: `frontend/src/lib/format.ts`
- Test: `frontend/src/lib/geometry.test.ts`
- Test: `frontend/src/lib/format.test.ts`

**Interfaces:**
- Consumes: JSON shapes from Task 10.
- Produces:
  - `types.ts`: `RefKind`, `Ref`, `Commit`, `EDGE_LINE = 0`, `EDGE_ARROW_DOWN = 1`, `EDGE_ARROW_UP = 2`, `Edge`, `LogRow`, `LogPage`, `Filters`, `emptyFilters(): Filters`, `Repo`, `Branch`, `Remote`, `Tag`, `Refs`, `FileChange`, `Details`
  - `geometry.ts`: `ROW_HEIGHT = 28`, `LANE_WIDTH = 16`, `GRAPH_PADDING = 10`, `DOT_RADIUS = 4`, `OVERSCAN = 10`, `laneX(lane): number`, `rowCenterY(index): number`, `type Segment = { x1, y1, x2, y2, color: number; arrow: 'none' | 'down' | 'up'; target?: string }`, `edgeSegment(edge: Edge, rowIndex: number): Segment`, `visibleRange(scrollTop, viewportHeight, total): { start: number; end: number }` (end exclusive), `graphWidth(rows: LogRow[]): number`, `arrowAt(rows: LogRow[], x: number, y: number): Segment | null`, `LANE_COLORS: string[]`, `laneColor(color: number): string`
  - `format.ts`: `relativeDate(iso: string, now?: Date): string`

- [ ] **Step 1: Write the types**

`frontend/src/lib/types.ts`:
```ts
export type RefKind = 'head' | 'local' | 'remote' | 'tag'

export interface Ref {
  name: string
  kind: RefKind
}

export interface Commit {
  hash: string
  short: string
  parents: string[]
  author: string
  email: string
  date: string
  subject: string
  refs: Ref[]
}

export const EDGE_LINE = 0
export const EDGE_ARROW_DOWN = 1
export const EDGE_ARROW_UP = 2

export interface Edge {
  from: number
  to: number
  color: number
  kind: number
  target?: string
}

export interface LogRow extends Commit {
  lane: number
  color: number
  edges: Edge[]
  isMerge: boolean
  isHead: boolean
}

export interface LogPage {
  rows: LogRow[]
  hasMore: boolean
  graphVisible: boolean
}

export interface Filters {
  text: string
  branch: string
  author: string
  since: string
  until: string
  paths: string[]
}

export const emptyFilters = (): Filters => ({ text: '', branch: '', author: '', since: '', until: '', paths: [] })

export interface Repo {
  id: string
  name: string
  path: string
  missing: boolean
  branch: string
}

export interface Branch {
  name: string
  remote: string
  hash: string
  current: boolean
  upstream: string
}

export interface Remote {
  name: string
  branches: Branch[]
}

export interface Tag {
  name: string
  hash: string
}

export interface Refs {
  head: string
  headHash: string
  detached: boolean
  local: Branch[]
  remotes: Remote[]
  tags: Tag[]
}

export interface FileChange {
  status: string
  path: string
  oldPath?: string
}

export interface Details extends Commit {
  body: string
  committer: string
  committerEmail: string
  commitDate: string
  files: FileChange[]
}
```

- [ ] **Step 2: Write the failing tests**

`frontend/src/lib/geometry.test.ts`:
```ts
import { describe, expect, it } from 'vitest'
import { arrowAt, edgeSegment, graphWidth, laneColor, laneX, LANE_COLORS, rowCenterY, visibleRange } from './geometry'
import { EDGE_ARROW_DOWN, EDGE_ARROW_UP, EDGE_LINE, type Edge, type LogRow } from './types'

const row = (lane: number, edges: Edge[] = []): LogRow =>
  ({ lane, color: 0, edges, isMerge: false, isHead: false, hash: '', short: '', parents: [], author: '', email: '', date: '', subject: '', refs: [] })

describe('positions', () => {
  it('centers lanes and rows', () => {
    expect(laneX(0)).toBe(18)
    expect(laneX(2)).toBe(50)
    expect(rowCenterY(0)).toBe(14)
    expect(rowCenterY(3)).toBe(98)
  })
})

describe('edgeSegment', () => {
  it('draws lines from the previous row center to this row center', () => {
    expect(edgeSegment({ from: 0, to: 1, color: 3, kind: EDGE_LINE }, 2)).toEqual({
      x1: 18, y1: 42, x2: 34, y2: 70, color: 3, arrow: 'none',
    })
  })

  it('ends a down arrow at the top of its row', () => {
    expect(edgeSegment({ from: 1, to: 1, color: 1, kind: EDGE_ARROW_DOWN, target: 'abc' }, 5)).toEqual({
      x1: 34, y1: 126, x2: 34, y2: 140, color: 1, arrow: 'down', target: 'abc',
    })
  })

  it('starts an up arrow at the top of its row', () => {
    expect(edgeSegment({ from: 1, to: 1, color: 1, kind: EDGE_ARROW_UP, target: 'abc' }, 5)).toEqual({
      x1: 34, y1: 140, x2: 34, y2: 154, color: 1, arrow: 'up', target: 'abc',
    })
  })
})

describe('visibleRange', () => {
  it('adds overscan and clamps to the row count', () => {
    expect(visibleRange(280, 280, 1000)).toEqual({ start: 0, end: 30 })
    expect(visibleRange(2800, 280, 1000)).toEqual({ start: 90, end: 120 })
    expect(visibleRange(0, 100, 5)).toEqual({ start: 0, end: 5 })
  })
})

describe('graphWidth', () => {
  it('fits the widest lane used by any dot or edge', () => {
    expect(graphWidth([row(0, [{ from: 2, to: 0, color: 0, kind: EDGE_LINE }]), row(1)])).toBe(68)
    expect(graphWidth([])).toBe(36)
  })
})

describe('arrowAt', () => {
  const rows = [row(0), row(0), row(0), row(0), row(0), row(1, [{ from: 1, to: 1, color: 1, kind: EDGE_ARROW_UP, target: 'abc' }])]

  it('finds an arrow under the pointer', () => {
    expect(arrowAt(rows, 34, 145)?.target).toBe('abc')
  })

  it('ignores points away from the arrow', () => {
    expect(arrowAt(rows, 18, 145)).toBeNull()
    expect(arrowAt(rows, 34, 20)).toBeNull()
  })
})

describe('laneColor', () => {
  it('cycles through the palette', () => {
    expect(laneColor(0)).toBe(LANE_COLORS[0])
    expect(laneColor(LANE_COLORS.length + 1)).toBe(LANE_COLORS[1])
  })
})
```

`frontend/src/lib/format.test.ts`:
```ts
import { describe, expect, it } from 'vitest'
import { relativeDate } from './format'

describe('relativeDate', () => {
  const now = new Date('2026-09-16T12:00:00Z')

  it('uses short relative units for recent dates', () => {
    expect(relativeDate('2026-09-16T11:59:30Z', now)).toBe('just now')
    expect(relativeDate('2026-09-16T11:55:00Z', now)).toBe('5m ago')
    expect(relativeDate('2026-09-16T09:00:00Z', now)).toBe('3h ago')
    expect(relativeDate('2026-09-14T12:00:00Z', now)).toBe('2d ago')
  })

  it('uses a calendar date after a week', () => {
    expect(relativeDate('2026-03-04T12:00:00Z', now)).toBe('Mar 4')
    expect(relativeDate('2025-03-04T12:00:00Z', now)).toBe('Mar 4, 2025')
  })
})
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd frontend && npm test`
Expected: FAIL — cannot resolve `./geometry` and `./format`.

- [ ] **Step 4: Implement**

`frontend/src/lib/geometry.ts`:
```ts
import { EDGE_ARROW_DOWN, EDGE_ARROW_UP, EDGE_LINE, type Edge, type LogRow } from './types'

export const ROW_HEIGHT = 28
export const LANE_WIDTH = 16
export const GRAPH_PADDING = 10
export const DOT_RADIUS = 4
export const OVERSCAN = 10

export const LANE_COLORS = ['#4f9d4f', '#a4478f', '#b58a3a', '#3f7fbf', '#c0504d', '#2a9d8f', '#7b61c9', '#d9822b']

export const laneX = (lane: number) => GRAPH_PADDING + lane * LANE_WIDTH + LANE_WIDTH / 2
export const rowCenterY = (index: number) => index * ROW_HEIGHT + ROW_HEIGHT / 2
export const laneColor = (color: number) => LANE_COLORS[((color % LANE_COLORS.length) + LANE_COLORS.length) % LANE_COLORS.length]

export interface Segment {
  x1: number
  y1: number
  x2: number
  y2: number
  color: number
  arrow: 'none' | 'down' | 'up'
  target?: string
}

export function edgeSegment(edge: Edge, rowIndex: number): Segment {
  const x1 = laneX(edge.from)
  const x2 = laneX(edge.to)
  const center = rowCenterY(rowIndex)
  const top = center - ROW_HEIGHT / 2
  switch (edge.kind) {
    case EDGE_ARROW_DOWN:
      return { x1, y1: rowCenterY(rowIndex - 1), x2, y2: top, color: edge.color, arrow: 'down', target: edge.target }
    case EDGE_ARROW_UP:
      return { x1, y1: top, x2, y2: center, color: edge.color, arrow: 'up', target: edge.target }
    default:
      return { x1, y1: rowCenterY(rowIndex - 1), x2, y2: center, color: edge.color, arrow: 'none' }
  }
}

export function visibleRange(scrollTop: number, viewportHeight: number, total: number) {
  const start = Math.max(0, Math.floor(scrollTop / ROW_HEIGHT) - OVERSCAN)
  const end = Math.min(total, Math.ceil((scrollTop + viewportHeight) / ROW_HEIGHT) + OVERSCAN)
  return { start, end }
}

export function graphWidth(rows: LogRow[]): number {
  let max = 0
  for (const r of rows) {
    max = Math.max(max, r.lane)
    for (const e of r.edges) max = Math.max(max, e.from, e.to)
  }
  return GRAPH_PADDING * 2 + (max + 1) * LANE_WIDTH
}

export function arrowAt(rows: LogRow[], x: number, y: number): Segment | null {
  const index = Math.floor(y / ROW_HEIGHT)
  for (const i of [index, index + 1]) {
    const row = rows[i]
    if (!row) continue
    for (const e of row.edges) {
      if (e.kind === EDGE_LINE) continue
      const s = edgeSegment(e, i)
      const top = Math.min(s.y1, s.y2)
      const bottom = Math.max(s.y1, s.y2)
      if (Math.abs(x - s.x1) <= LANE_WIDTH / 2 && y >= top && y <= bottom) return s
    }
  }
  return null
}
```

`frontend/src/lib/format.ts`:
```ts
export function relativeDate(iso: string, now = new Date()): string {
  const date = new Date(iso)
  const seconds = Math.round((now.getTime() - date.getTime()) / 1000)
  if (seconds < 60) return 'just now'
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`
  if (seconds < 7 * 86400) return `${Math.floor(seconds / 86400)}d ago`
  const options: Intl.DateTimeFormatOptions = { month: 'short', day: 'numeric' }
  if (date.getFullYear() !== now.getFullYear()) options.year = 'numeric'
  return date.toLocaleDateString('en-US', options)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd frontend && npm test`
Expected: 2 test files passed.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/lib
git commit -m "feat(ui): add frontend types, graph geometry and date formatting"
```

---

### Task 12: App shell, theme, state and shared UI

**Files:**
- Create: `frontend/src/theme.css`
- Modify: `frontend/src/main.ts` (replace)
- Modify: `frontend/src/App.svelte` (replace)
- Delete: `frontend/src/style.css`, `frontend/src/assets/`
- Create: `frontend/src/lib/api.ts`, `frontend/src/lib/stores.ts`, `frontend/src/lib/ui.ts`, `frontend/src/lib/actions.ts`
- Create: `frontend/src/components/Icon.svelte`, `Splitter.svelte`, `ContextMenu.svelte`, `DialogHost.svelte`, `Toasts.svelte`, `ChatPanel.svelte`
- Create (placeholders replaced in Tasks 13–14): `frontend/src/components/Sidebar.svelte`, `frontend/src/components/LogView.svelte`

**Interfaces:**
- Consumes: bindings in `frontend/wailsjs/go/app/App` and `frontend/wailsjs/runtime/runtime` (Task 10); types (Task 11).
- Produces:
  - `api` object in `lib/api.ts` with camelCase methods mirroring every Task 10 binding, typed with Task 11 types (for example `api.getLog(id, filters, offset, limit): Promise<LogPage>`).
  - `lib/stores.ts`: `repos`, `selectedRepoId`, `selectedRepo` (derived `Repo | null`), `refs` (`Refs | null`), `filters`, `selectedHash`, `jumpTo` (hash to scroll to, `''` when idle), `logVersion` (bump to reload the log), `busy` (label of the running action, `''` when idle), persisted `sidebarWidth`, `chatWidth`, `detailsHeight`, `chatOpen`; functions `loadRepos()`, `loadRefs()`, `refreshRepo()`, `selectRepo(id)`.
  - `lib/ui.ts`: `toasts`, `toast(message, kind?)`, `dismissToast(id)`, `errorMessage(e)`, `dialog`, `confirmDialog(opts): Promise<boolean>`, `promptDialog(opts): Promise<PromptResult | null>`, `menu`, `openMenu(event, items)`, `copyText(text)`; types `MenuItem`, `PromptResult { value, second, checked }`.
  - `lib/actions.ts`: `addRepo()`, `removeRepo(repo)`, `relocateRepo(id)`, `fetchRepo(id)`, `pullRepo(id)`, `checkoutBranch(id, branch)`, `checkoutCommit(id, hash)`, `newBranch(id, target, targetLabel)`, `deleteBranch(id, branch)`, `newTag(id, target, targetLabel)`, `deleteTag(id, name)`, `startFocusRefresh(): () => void`.
  - `Icon.svelte` props `name`, `size = 16`. `Splitter.svelte` prop `direction: 'vertical' | 'horizontal'`, event `drag` with the pixel delta.

- [ ] **Step 1: Remove template leftovers**

```bash
cd /Users/josfh/playground/git-ui/frontend
rm -rf src/assets src/style.css
```

- [ ] **Step 2: Theme and entry point**

`frontend/src/theme.css`:
```css
:root {
  --bg: #f9f8f6;
  --surface: #ffffff;
  --sidebar: #f4f3f0;
  --hover: #ecebe7;
  --active: #e5e3de;
  --border: #e7e5e0;
  --text: #1f1e1c;
  --muted: #7a7873;
  --faint: #a8a6a1;
  --accent: #c96442;
  --danger: #c0392b;
  --selection: #e8eefb;
  --merge-text: #8a8883;
  --add-bg: #e6f4ea;
  --del-bg: #fce8e6;
  --shadow: 0 8px 28px rgba(0, 0, 0, 0.12);
  --radius: 8px;
  --font: -apple-system, BlinkMacSystemFont, 'SF Pro Text', 'Helvetica Neue', sans-serif;
  --mono: ui-monospace, 'SF Mono', Menlo, monospace;
  color-scheme: light;
}

@media (prefers-color-scheme: dark) {
  :root {
    --bg: #201f1d;
    --surface: #262624;
    --sidebar: #1b1a19;
    --hover: #2f2e2b;
    --active: #3a3935;
    --border: #34332f;
    --text: #ecebe8;
    --muted: #9c9a95;
    --faint: #6f6d69;
    --selection: #2d3444;
    --merge-text: #8c8a85;
    --add-bg: #1f3a28;
    --del-bg: #45221f;
    --shadow: 0 8px 28px rgba(0, 0, 0, 0.5);
    color-scheme: dark;
  }
}

* { box-sizing: border-box; }
html, body, #app { height: 100%; margin: 0; }
body {
  background: var(--bg);
  color: var(--text);
  font: 13px/1.4 var(--font);
  -webkit-font-smoothing: antialiased;
  overflow: hidden;
  user-select: none;
  cursor: default;
}
button { font: inherit; color: inherit; background: none; border: 0; padding: 0; cursor: default; }
button:disabled { color: var(--faint); }
input, textarea, select {
  font: inherit;
  color: var(--text);
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 7px;
  padding: 4px 8px;
  outline: none;
  user-select: text;
}
input:focus, textarea:focus, select:focus { border-color: var(--faint); }
pre { margin: 0; font: inherit; white-space: pre-wrap; }

.section-title { font-size: 12px; font-weight: 500; color: var(--muted); }
.row-item {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  height: 28px;
  padding: 0 10px;
  border-radius: var(--radius);
  white-space: nowrap;
  overflow: hidden;
  text-align: left;
}
.row-item:hover { background: var(--hover); }
.row-item.active { background: var(--active); }
.icon-btn {
  width: 24px;
  height: 24px;
  display: inline-grid;
  place-items: center;
  flex: none;
  border-radius: 6px;
  color: var(--muted);
}
.icon-btn:hover:not(:disabled) { background: var(--hover); color: var(--text); }
.btn { height: 28px; padding: 0 12px; border-radius: 7px; border: 1px solid var(--border); background: var(--surface); }
.btn:hover { background: var(--hover); }
.btn.primary { background: var(--text); color: var(--bg); border-color: var(--text); }
.btn.danger { background: var(--danger); color: #fff; border-color: var(--danger); }
.ellipsis { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; }
.mono { font-family: var(--mono); font-size: 12px; }
.drag { --wails-draggable: drag; }
.drag button, .drag input { --wails-draggable: no-drag; }
```

`frontend/src/main.ts`:
```ts
import './theme.css'
import App from './App.svelte'

const app = new App({ target: document.getElementById('app')! })

export default app
```

- [ ] **Step 3: API wrapper**

`frontend/src/lib/api.ts`:
```ts
import * as Go from '../../wailsjs/go/app/App'
import type { Details, Filters, LogPage, Refs, Repo } from './types'

// The generated bindings use Wails model classes; the JSON is identical to our
// interfaces, so cast at this single boundary.
const call = <T>(p: Promise<unknown>) => p as Promise<T>

export const api = {
  listRepos: () => call<Repo[]>(Go.ListRepos()),
  addRepo: () => call<Repo>(Go.AddRepo()),
  relocateRepo: (id: string) => call<Repo>(Go.RelocateRepo(id)),
  removeRepo: (id: string) => call<void>(Go.RemoveRepo(id)),

  getRefs: (id: string) => call<Refs>(Go.GetRefs(id)),
  getLog: (id: string, filters: Filters, offset: number, limit: number) =>
    call<LogPage>(Go.GetLog(id, filters as any, offset, limit)),
  getDetails: (id: string, hash: string) => call<Details>(Go.GetDetails(id, hash)),
  getDiff: (id: string, parent: string, hash: string, paths: string[]) => call<string>(Go.GetDiff(id, parent, hash, paths)),
  getAuthors: (id: string) => call<string[]>(Go.GetAuthors(id)),
  resolveCommit: (id: string, text: string) => call<string>(Go.ResolveCommit(id, text)),
  isShallow: (id: string) => call<boolean>(Go.IsShallow(id)),
  fingerprint: (id: string) => call<string>(Go.Fingerprint(id)),

  checkout: (id: string, branch: string) => call<void>(Go.Checkout(id, branch)),
  checkoutRemote: (id: string, remote: string, name: string) => call<void>(Go.CheckoutRemote(id, remote, name)),
  checkoutDetached: (id: string, hash: string) => call<void>(Go.CheckoutDetached(id, hash)),
  fetch: (id: string) => call<void>(Go.Fetch(id)),
  pull: (id: string) => call<void>(Go.Pull(id)),
  createBranch: (id: string, name: string, target: string, checkout: boolean) =>
    call<void>(Go.CreateBranch(id, name, target, checkout)),
  deleteBranch: (id: string, name: string, force: boolean) => call<void>(Go.DeleteBranch(id, name, force)),
  deleteRemoteBranch: (id: string, remote: string, name: string) => call<void>(Go.DeleteRemoteBranch(id, remote, name)),
  createTag: (id: string, name: string, target: string, message: string) => call<void>(Go.CreateTag(id, name, target, message)),
  deleteTag: (id: string, name: string) => call<void>(Go.DeleteTag(id, name)),
}
```

- [ ] **Step 4: Stores**

`frontend/src/lib/stores.ts`:
```ts
import { derived, get, writable, type Writable } from 'svelte/store'
import { api } from './api'
import { emptyFilters, type Filters, type Refs, type Repo } from './types'

function persisted<T>(key: string, initial: T): Writable<T> {
  let start = initial
  try {
    const raw = localStorage.getItem(key)
    if (raw !== null) start = JSON.parse(raw)
  } catch {
    // Storage unavailable: fall back to the default.
  }
  const store = writable<T>(start)
  store.subscribe((value) => {
    try {
      localStorage.setItem(key, JSON.stringify(value))
    } catch {
      // Ignore: persistence is a convenience.
    }
  })
  return store
}

export const sidebarWidth = persisted('sidebarWidth', 280)
export const chatWidth = persisted('chatWidth', 340)
export const detailsHeight = persisted('detailsHeight', 280)
export const chatOpen = persisted('chatOpen', true)
export const selectedRepoId = persisted('selectedRepoId', '')

export const repos = writable<Repo[]>([])
export const refs = writable<Refs | null>(null)
export const filters = writable<Filters>(emptyFilters())
export const selectedHash = writable('')
export const jumpTo = writable('')
export const logVersion = writable(0)
export const busy = writable('')

export const selectedRepo = derived([repos, selectedRepoId], ([$repos, $id]) => $repos.find((r) => r.id === $id) ?? null)

export async function loadRepos() {
  repos.set(await api.listRepos())
}

export async function loadRefs() {
  const repo = get(selectedRepo)
  if (!repo || repo.missing) {
    refs.set(null)
    return
  }
  try {
    refs.set(await api.getRefs(repo.id))
  } catch {
    refs.set(null)
  }
}

export async function refreshRepo() {
  await loadRepos()
  await loadRefs()
  logVersion.update((v) => v + 1)
}

export function selectRepo(id: string) {
  if (get(selectedRepoId) !== id) {
    filters.set(emptyFilters())
    selectedHash.set('')
  }
  selectedRepoId.set(id)
  loadRefs()
}
```

- [ ] **Step 5: Shared UI state**

`frontend/src/lib/ui.ts`:
```ts
import { writable } from 'svelte/store'
import { ClipboardSetText } from '../../wailsjs/runtime/runtime'

export interface Toast {
  id: number
  message: string
  kind: 'error' | 'info'
}

export const toasts = writable<Toast[]>([])
let nextToast = 1

export function toast(message: string, kind: Toast['kind'] = 'info') {
  const id = nextToast++
  toasts.update((list) => [...list, { id, message, kind }])
  if (kind === 'info') setTimeout(() => dismissToast(id), 3000)
}

export function dismissToast(id: number) {
  toasts.update((list) => list.filter((t) => t.id !== id))
}

export function errorMessage(e: unknown): string {
  if (typeof e === 'string') return e
  if (e instanceof Error) return e.message
  return String(e)
}

export interface ConfirmOptions {
  title: string
  message: string
  confirmLabel: string
  danger?: boolean
}

export interface PromptOptions {
  title: string
  label: string
  value?: string
  secondLabel?: string
  checkboxLabel?: string
  checked?: boolean
  submitLabel?: string
}

export interface PromptResult {
  value: string
  second: string
  checked: boolean
}

export type Dialog =
  | (ConfirmOptions & { kind: 'confirm'; resolve: (ok: boolean) => void })
  | (PromptOptions & { kind: 'prompt'; resolve: (result: PromptResult | null) => void })

export const dialog = writable<Dialog | null>(null)

export const confirmDialog = (options: ConfirmOptions) =>
  new Promise<boolean>((resolve) => dialog.set({ ...options, kind: 'confirm', resolve }))

export const promptDialog = (options: PromptOptions) =>
  new Promise<PromptResult | null>((resolve) => dialog.set({ ...options, kind: 'prompt', resolve }))

export interface MenuItem {
  label: string
  action: () => void
  danger?: boolean
  disabled?: boolean
}

export const menu = writable<{ x: number; y: number; items: MenuItem[] } | null>(null)

export function openMenu(event: MouseEvent, items: MenuItem[]) {
  event.preventDefault()
  event.stopPropagation()
  menu.set({ x: event.clientX, y: event.clientY, items })
}

export async function copyText(text: string) {
  await ClipboardSetText(text)
  toast('Copied')
}
```

- [ ] **Step 6: Actions**

`frontend/src/lib/actions.ts`:
```ts
import { get } from 'svelte/store'
import { api } from './api'
import { busy, loadRefs, loadRepos, logVersion, refreshRepo, selectRepo, selectedRepoId } from './stores'
import type { Branch, Repo } from './types'
import { confirmDialog, errorMessage, promptDialog, toast } from './ui'

async function run(label: string, fn: () => Promise<unknown>): Promise<boolean> {
  busy.set(label)
  try {
    await fn()
    return true
  } catch (e) {
    toast(errorMessage(e), 'error')
    return false
  } finally {
    busy.set('')
    await refreshRepo()
  }
}

export async function addRepo() {
  try {
    const repo = await api.addRepo()
    if (!repo.id) return
    await loadRepos()
    selectRepo(repo.id)
  } catch (e) {
    toast(errorMessage(e), 'error')
  }
}

export async function removeRepo(repo: Repo) {
  const ok = await confirmDialog({
    title: 'Remove repository',
    message: `Remove ${repo.name} from the list? Files on disk are not touched.`,
    confirmLabel: 'Remove',
  })
  if (!ok) return
  try {
    await api.removeRepo(repo.id)
    if (get(selectedRepoId) === repo.id) selectedRepoId.set('')
    await loadRepos()
    await loadRefs()
  } catch (e) {
    toast(errorMessage(e), 'error')
  }
}

export async function relocateRepo(id: string) {
  try {
    const repo = await api.relocateRepo(id)
    if (!repo.id) return
    await refreshRepo()
  } catch (e) {
    toast(errorMessage(e), 'error')
  }
}

export const fetchRepo = (id: string) => run('Fetching…', () => api.fetch(id))
export const pullRepo = (id: string) => run('Pulling…', () => api.pull(id))

export const checkoutBranch = (id: string, branch: Branch) =>
  run('Checking out…', () =>
    branch.remote ? api.checkoutRemote(id, branch.remote, branch.name) : api.checkout(id, branch.name))

export async function checkoutCommit(id: string, hash: string) {
  const ok = await confirmDialog({
    title: 'Check out commit',
    message: `HEAD will be detached at ${hash.slice(0, 8)}. New commits made there won't belong to any branch.`,
    confirmLabel: 'Check out',
  })
  if (ok) await run('Checking out…', () => api.checkoutDetached(id, hash))
}

export async function newBranch(id: string, target: string, targetLabel: string) {
  const result = await promptDialog({
    title: 'New branch',
    label: `Branch name (from ${targetLabel})`,
    checkboxLabel: 'Check out after creating',
    checked: true,
    submitLabel: 'Create',
  })
  const name = result?.value.trim()
  if (!result || !name) return
  await run('Creating branch…', () => api.createBranch(id, name, target, result.checked))
}

export async function deleteBranch(id: string, branch: Branch) {
  if (branch.remote) {
    const ok = await confirmDialog({
      title: 'Delete remote branch',
      message: `Delete ${branch.name} on remote "${branch.remote}"? This affects everyone who uses that remote.`,
      confirmLabel: 'Delete on remote',
      danger: true,
    })
    if (ok) await run('Deleting remote branch…', () => api.deleteRemoteBranch(id, branch.remote, branch.name))
    return
  }

  const ok = await confirmDialog({
    title: 'Delete branch',
    message: `Delete local branch ${branch.name}?`,
    confirmLabel: 'Delete',
    danger: true,
  })
  if (!ok) return
  busy.set('Deleting branch…')
  try {
    await api.deleteBranch(id, branch.name, false)
  } catch (e) {
    const message = errorMessage(e)
    busy.set('')
    if (!message.includes('not fully merged')) {
      toast(message, 'error')
      return
    }
    const force = await confirmDialog({
      title: 'Branch not merged',
      message: `${branch.name} has commits that are not merged into any other branch. Force deleting loses them.`,
      confirmLabel: 'Force delete',
      danger: true,
    })
    if (force) await run('Deleting branch…', () => api.deleteBranch(id, branch.name, true))
    return
  } finally {
    busy.set('')
  }
  await refreshRepo()
}

export async function newTag(id: string, target: string, targetLabel: string) {
  const result = await promptDialog({
    title: 'New tag',
    label: `Tag name (at ${targetLabel})`,
    secondLabel: 'Message (optional, creates an annotated tag)',
    submitLabel: 'Create',
  })
  const name = result?.value.trim()
  if (!result || !name) return
  await run('Creating tag…', () => api.createTag(id, name, target, result.second.trim()))
}

export async function deleteTag(id: string, name: string) {
  const ok = await confirmDialog({
    title: 'Delete tag',
    message: `Delete local tag ${name}? Tags already pushed stay on the remote.`,
    confirmLabel: 'Delete',
    danger: true,
  })
  if (ok) await run('Deleting tag…', () => api.deleteTag(id, name))
}

// Reloads refs and log when the repo changed outside the app while the window
// was in the background.
export function startFocusRefresh(): () => void {
  let known = ''
  let knownId = ''

  const remember = async () => {
    const id = get(selectedRepoId)
    if (!id) return
    try {
      known = await api.fingerprint(id)
      knownId = id
    } catch {
      known = ''
    }
  }

  const onFocus = async () => {
    const id = get(selectedRepoId)
    if (!id) return
    try {
      const current = await api.fingerprint(id)
      if (id === knownId && known && current !== known) await refreshRepo()
      known = current
      knownId = id
    } catch {
      // Missing repo: the sidebar already shows it.
    }
  }

  const stopVersion = logVersion.subscribe(() => remember())
  const stopRepo = selectedRepoId.subscribe(() => remember())
  window.addEventListener('focus', onFocus)
  return () => {
    stopVersion()
    stopRepo()
    window.removeEventListener('focus', onFocus)
  }
}
```

- [ ] **Step 7: Icon and Splitter**

`frontend/src/components/Icon.svelte`:
```svelte
<script lang="ts">
  export let name: string
  export let size = 16

  const paths: Record<string, string> = {
    plus: 'M8 3v10M3 8h10',
    refresh: 'M13 8a5 5 0 1 1-1.5-3.5M13 2.5V5h-2.5',
    download: 'M8 2.5v8m0 0L5 7.5m3 3 3-3M3 13.5h10',
    'chevron-right': 'M6 4l4 4-4 4',
    'chevron-down': 'M4 6l4 4 4-4',
    settings: 'M8 10a2 2 0 1 0 0-4 2 2 0 0 0 0 4ZM8 1.5v2M8 12.5v2M1.5 8h2M12.5 8h2M3.4 3.4l1.4 1.4M11.2 11.2l1.4 1.4M3.4 12.6l1.4-1.4M11.2 4.8l1.4-1.4',
    tag: 'M2.5 2.5h5l6 6-5 5-6-6v-5ZM5.5 5.5h.01',
    cloud: 'M4.5 12.5h7a3 3 0 0 0 .4-6 4 4 0 0 0-7.7 1 2.5 2.5 0 0 0 .3 5Z',
    check: 'M3.5 8.5l3 3 6-7',
    'panel-right': 'M2.5 3.5h11v9h-11zM10 3.5v9',
    search: 'M7 12a5 5 0 1 0 0-10 5 5 0 0 0 0 10ZM10.5 10.5 14 14',
    x: 'M4 4l8 8M12 4l-8 8',
    send: 'M8 13V3M4 7l4-4 4 4',
    sparkle: 'M8 2l1.5 4.5L14 8l-4.5 1.5L8 14l-1.5-4.5L2 8l4.5-1.5z',
    copy: 'M5.5 5.5h7v8h-7zM3.5 10.5v-8h7',
  }
</script>

<svg width={size} height={size} viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
  <path d={paths[name] ?? ''} />
</svg>
```

`frontend/src/components/Splitter.svelte`:
```svelte
<script lang="ts">
  import { createEventDispatcher } from 'svelte'

  export let direction: 'vertical' | 'horizontal' = 'vertical'

  const dispatch = createEventDispatcher<{ drag: number }>()

  function down(event: PointerEvent) {
    const handle = event.currentTarget as HTMLElement
    const pick = (e: PointerEvent) => (direction === 'vertical' ? e.clientX : e.clientY)
    let last = pick(event)
    handle.setPointerCapture(event.pointerId)
    const move = (e: PointerEvent) => {
      const pos = pick(e)
      dispatch('drag', pos - last)
      last = pos
    }
    const up = () => {
      handle.removeEventListener('pointermove', move)
      handle.removeEventListener('pointerup', up)
    }
    handle.addEventListener('pointermove', move)
    handle.addEventListener('pointerup', up)
  }
</script>

<div class="splitter {direction}" on:pointerdown={down}></div>

<style>
  .splitter { flex: none; position: relative; z-index: 3; background: var(--border); }
  .vertical { width: 1px; cursor: col-resize; }
  .vertical::after { content: ''; position: absolute; top: 0; bottom: 0; left: -4px; right: -4px; }
  .horizontal { height: 1px; cursor: row-resize; }
  .horizontal::after { content: ''; position: absolute; left: 0; right: 0; top: -4px; bottom: -4px; }
</style>
```

- [ ] **Step 8: Context menu, dialogs and toasts**

`frontend/src/components/ContextMenu.svelte`:
```svelte
<script lang="ts">
  import { menu, type MenuItem } from '../lib/ui'

  const close = () => menu.set(null)

  function choose(item: MenuItem) {
    close()
    item.action()
  }
</script>

<svelte:window on:click={close} on:blur={close} on:resize={close} on:keydown={(e) => e.key === 'Escape' && close()} />

{#if $menu}
  <div
    class="menu"
    style="left: {Math.min($menu.x, window.innerWidth - 230)}px; top: {Math.min($menu.y, window.innerHeight - $menu.items.length * 30 - 16)}px"
    on:contextmenu|preventDefault
  >
    {#each $menu.items as item}
      <button class="item" class:danger={item.danger} disabled={item.disabled} on:click|stopPropagation={() => choose(item)}>
        {item.label}
      </button>
    {/each}
  </div>
{/if}

<style>
  .menu {
    position: fixed;
    z-index: 50;
    min-width: 210px;
    padding: 4px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 10px;
    box-shadow: var(--shadow);
  }
  .item { display: block; width: 100%; height: 28px; padding: 0 10px; border-radius: 6px; text-align: left; }
  .item:hover:not(:disabled) { background: var(--hover); }
  .danger { color: var(--danger); }
</style>
```

`frontend/src/components/DialogHost.svelte`:
```svelte
<script lang="ts">
  import { dialog } from '../lib/ui'

  let value = ''
  let second = ''
  let checked = false

  $: if ($dialog?.kind === 'prompt') {
    value = $dialog.value ?? ''
    second = ''
    checked = $dialog.checked ?? false
  }

  function finish(ok: boolean) {
    const current = $dialog
    if (!current) return
    dialog.set(null)
    if (current.kind === 'confirm') current.resolve(ok)
    else current.resolve(ok ? { value, second, checked } : null)
  }

  function focus(node: HTMLElement, enabled = true) {
    if (enabled) setTimeout(() => node.focus())
  }
</script>

<svelte:window on:keydown={(e) => $dialog && e.key === 'Escape' && finish(false)} />

{#if $dialog}
  <div class="backdrop" on:click|self={() => finish(false)} role="presentation">
    <form class="dialog" on:submit|preventDefault={() => finish(true)}>
      <h3>{$dialog.title}</h3>
      {#if $dialog.kind === 'confirm'}
        <p>{$dialog.message}</p>
      {:else}
        <label>
          <span>{$dialog.label}</span>
          <input bind:value use:focus />
        </label>
        {#if $dialog.secondLabel}
          <label>
            <span>{$dialog.secondLabel}</span>
            <input bind:value={second} />
          </label>
        {/if}
        {#if $dialog.checkboxLabel}
          <label class="check"><input type="checkbox" bind:checked /> {$dialog.checkboxLabel}</label>
        {/if}
      {/if}
      <div class="buttons">
        <button type="button" class="btn" on:click={() => finish(false)}>Cancel</button>
        <button
          type="submit"
          class="btn {$dialog.kind === 'confirm' && $dialog.danger ? 'danger' : 'primary'}"
          use:focus={$dialog.kind === 'confirm'}
        >
          {$dialog.kind === 'confirm' ? $dialog.confirmLabel : $dialog.submitLabel ?? 'OK'}
        </button>
      </div>
    </form>
  </div>
{/if}

<style>
  .backdrop { position: fixed; inset: 0; z-index: 40; display: grid; place-items: center; background: rgba(0, 0, 0, 0.25); }
  .dialog {
    width: 420px;
    padding: 18px 20px 16px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 12px;
    box-shadow: var(--shadow);
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  h3 { margin: 0; font-size: 14px; font-weight: 600; }
  p { margin: 0; color: var(--muted); line-height: 1.5; }
  label { display: flex; flex-direction: column; gap: 5px; color: var(--muted); font-size: 12px; }
  label input:not([type='checkbox']) { font-size: 13px; }
  .check { flex-direction: row; align-items: center; gap: 6px; color: var(--text); font-size: 13px; }
  .buttons { display: flex; justify-content: flex-end; gap: 8px; margin-top: 4px; }
</style>
```

`frontend/src/components/Toasts.svelte`:
```svelte
<script lang="ts">
  import Icon from './Icon.svelte'
  import { copyText, dismissToast, toasts } from '../lib/ui'
</script>

<div class="toasts">
  {#each $toasts as t (t.id)}
    <div class="toast" class:error={t.kind === 'error'}>
      <pre class="message">{t.message}</pre>
      {#if t.kind === 'error'}
        <button class="icon-btn" title="Copy" on:click={() => copyText(t.message)}><Icon name="copy" size={14} /></button>
      {/if}
      <button class="icon-btn" title="Dismiss" on:click={() => dismissToast(t.id)}><Icon name="x" size={14} /></button>
    </div>
  {/each}
</div>

<style>
  .toasts { position: fixed; right: 16px; bottom: 16px; z-index: 60; display: flex; flex-direction: column; gap: 8px; }
  .toast {
    display: flex;
    align-items: flex-start;
    gap: 6px;
    max-width: 440px;
    padding: 10px 8px 10px 14px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 10px;
    box-shadow: var(--shadow);
  }
  .error { border-left: 3px solid var(--danger); }
  .message { flex: 1; max-height: 200px; overflow: auto; user-select: text; }
</style>
```

- [ ] **Step 9: Chat panel placeholder and temporary panels**

`frontend/src/components/ChatPanel.svelte`:
```svelte
<script lang="ts">
  import Icon from './Icon.svelte'
  import { chatOpen, selectedRepo } from '../lib/stores'
</script>

<div class="chat">
  <header class="drag">
    <span class="title">Chat</span>
    <button class="icon-btn" title="Hide chat" on:click={() => chatOpen.set(false)}><Icon name="panel-right" /></button>
  </header>
  <div class="empty">
    <Icon name="sparkle" size={22} />
    <p>Set up an AI provider to chat with {$selectedRepo ? $selectedRepo.name : 'your repositories'}.</p>
    <p class="hint">Anthropic, OpenAI and Ollama support arrives in a later version.</p>
  </div>
  <div class="composer">
    <textarea rows="2" placeholder="Ask about this repo…" disabled></textarea>
    <button class="icon-btn" disabled title="Send"><Icon name="send" /></button>
  </div>
</div>

<style>
  .chat { display: flex; flex-direction: column; height: 100%; }
  header { display: flex; align-items: center; justify-content: space-between; height: 44px; padding: 0 10px 0 16px; flex: none; }
  .title { font-weight: 500; }
  .empty { flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 4px; padding: 24px; text-align: center; color: var(--muted); }
  .empty p { margin: 0; }
  .hint { font-size: 12px; color: var(--faint); }
  .composer { display: flex; align-items: flex-end; gap: 6px; margin: 12px; padding: 8px; background: var(--surface); border: 1px solid var(--border); border-radius: 12px; }
  textarea { flex: 1; resize: none; border: 0; padding: 2px 4px; background: transparent; }
</style>
```

`frontend/src/components/Sidebar.svelte` (placeholder, replaced in Task 13):
```svelte
<div style="padding: 48px 16px; color: var(--muted)">Sidebar</div>
```

`frontend/src/components/LogView.svelte` (placeholder, replaced in Task 14):
```svelte
<div style="padding: 48px 16px; color: var(--muted)">Log</div>
```

- [ ] **Step 10: App shell**

`frontend/src/App.svelte`:
```svelte
<script lang="ts">
  import { onMount } from 'svelte'
  import ChatPanel from './components/ChatPanel.svelte'
  import ContextMenu from './components/ContextMenu.svelte'
  import DialogHost from './components/DialogHost.svelte'
  import LogView from './components/LogView.svelte'
  import Sidebar from './components/Sidebar.svelte'
  import Splitter from './components/Splitter.svelte'
  import Toasts from './components/Toasts.svelte'
  import { startFocusRefresh } from './lib/actions'
  import { chatOpen, chatWidth, loadRefs, loadRepos, sidebarWidth } from './lib/stores'

  const clamp = (v: number, min: number, max: number) => Math.min(max, Math.max(min, v))

  onMount(() => {
    loadRepos().then(loadRefs)
    return startFocusRefresh()
  })
</script>

<div class="app">
  <aside style="width: {$sidebarWidth}px"><Sidebar /></aside>
  <Splitter on:drag={(e) => sidebarWidth.set(clamp($sidebarWidth + e.detail, 200, 480))} />
  <main><LogView /></main>
  {#if $chatOpen}
    <Splitter on:drag={(e) => chatWidth.set(clamp($chatWidth - e.detail, 260, 560))} />
    <section class="chat" style="width: {$chatWidth}px"><ChatPanel /></section>
  {/if}
</div>

<ContextMenu />
<DialogHost />
<Toasts />

<style>
  .app { display: flex; height: 100%; }
  aside { flex: none; min-width: 0; background: var(--sidebar); }
  main { flex: 1; min-width: 0; background: var(--surface); }
  .chat { flex: none; min-width: 0; background: var(--bg); }
</style>
```

- [ ] **Step 11: Verify**

```bash
source ~/.nvm/nvm.sh && nvm use 22
cd /Users/josfh/playground/git-ui/frontend && npm run check && npm test
cd .. && ~/go/bin/wails build
```
Expected: svelte-check reports 0 errors (warnings are acceptable), Vitest passes, build succeeds.

Manual: `open build/bin/git-ui.app`. Expect: three columns with "Sidebar", "Log" and the chat panel; the chat's hide button removes the panel; dragging the dividers resizes columns; after relaunch the chat stays hidden and widths persist. In dark mode (System Settings → Appearance → Dark) the colors switch.

- [ ] **Step 12: Commit**

```bash
git add -A
git commit -m "feat(ui): add app shell, theme, state, dialogs and chat placeholder"
```

---

### Task 13: Sidebar with repos, branches, remotes and tags

**Files:**
- Modify: `frontend/src/components/Sidebar.svelte` (replace placeholder)
- Create: `frontend/src/components/RepoRefs.svelte`

**Interfaces:**
- Consumes: `stores` (`repos`, `selectedRepoId`, `selectRepo`, `refs`, `filters`, `busy`), `actions` (`addRepo`, `removeRepo`, `relocateRepo`, `fetchRepo`, `pullRepo`, `checkoutBranch`, `newBranch`, `deleteBranch`, `newTag`, `deleteTag`), `ui.openMenu`, `Icon`.
- Produces: `RepoRefs.svelte` with prop `repoId: string`. Clicking a ref sets `filters.branch` to its full ref name (`refs/heads/<name>`, `refs/remotes/<remote>/<name>`, `refs/tags/<name>`); clicking the active ref clears it.

- [ ] **Step 1: RepoRefs**

`frontend/src/components/RepoRefs.svelte`:
```svelte
<script lang="ts">
  import Icon from './Icon.svelte'
  import { checkoutBranch, deleteBranch, deleteTag, newBranch, newTag } from '../lib/actions'
  import { busy, filters, refs } from '../lib/stores'
  import type { Branch, Tag } from '../lib/types'
  import { openMenu } from '../lib/ui'

  export let repoId: string

  let showRemotes = true
  let showTags = true
  let openRemotes: Record<string, boolean> = {}

  const branchRef = (b: Branch) => (b.remote ? `refs/remotes/${b.remote}/${b.name}` : `refs/heads/${b.name}`)
  const branchLabel = (b: Branch) => (b.remote ? `${b.remote}/${b.name}` : b.name)

  function toggleFilter(ref: string) {
    filters.update((f) => ({ ...f, branch: f.branch === ref ? '' : ref }))
  }

  function checkout(b: Branch) {
    if (!b.current && !$busy) checkoutBranch(repoId, b)
  }

  function branchMenu(event: MouseEvent, b: Branch) {
    openMenu(event, [
      { label: 'Check out', action: () => checkoutBranch(repoId, b), disabled: b.current || !!$busy },
      { label: 'New branch from here…', action: () => newBranch(repoId, branchLabel(b), branchLabel(b)) },
      { label: 'New tag here…', action: () => newTag(repoId, branchLabel(b), branchLabel(b)) },
      { label: b.remote ? 'Delete on remote…' : 'Delete…', action: () => deleteBranch(repoId, b), danger: true, disabled: b.current },
    ])
  }

  function tagMenu(event: MouseEvent, t: Tag) {
    openMenu(event, [
      { label: 'New branch from here…', action: () => newBranch(repoId, `refs/tags/${t.name}`, t.name) },
      { label: 'Delete…', action: () => deleteTag(repoId, t.name), danger: true },
    ])
  }
</script>

{#if $refs}
  <div class="refs">
    <div class="section">
      <span class="section-title">Branches</span>
      <button class="icon-btn" title="New branch from HEAD" on:click={() => newBranch(repoId, 'HEAD', 'HEAD')}>
        <Icon name="plus" size={14} />
      </button>
    </div>
    {#if $refs.detached}
      <div class="row-item ref detached">
        <span class="mark"><Icon name="check" size={12} /></span>
        <span class="mono">HEAD ({$refs.headHash.slice(0, 8)})</span>
      </div>
    {/if}
    {#each $refs.local as b (b.name)}
      <button
        class="row-item ref"
        class:active={$filters.branch === branchRef(b)}
        title={b.upstream ? `${b.name} → ${b.upstream}` : b.name}
        on:click={() => toggleFilter(branchRef(b))}
        on:dblclick={() => checkout(b)}
        on:contextmenu={(e) => branchMenu(e, b)}
      >
        <span class="mark">{#if b.current}<Icon name="check" size={12} />{/if}</span>
        <span class="ellipsis" class:current={b.current}>{b.name}</span>
      </button>
    {/each}

    {#if $refs.remotes.length}
      <div class="section">
        <button class="section-title" on:click={() => (showRemotes = !showRemotes)}>Remotes</button>
      </div>
      {#if showRemotes}
        {#each $refs.remotes as remote (remote.name)}
          <button class="row-item ref" on:click={() => (openRemotes = { ...openRemotes, [remote.name]: !openRemotes[remote.name] })}>
            <span class="mark"><Icon name={openRemotes[remote.name] ? 'chevron-down' : 'chevron-right'} size={12} /></span>
            <Icon name="cloud" size={14} />
            <span class="ellipsis">{remote.name}</span>
          </button>
          {#if openRemotes[remote.name]}
            {#each remote.branches as b (b.name)}
              <button
                class="row-item ref nested"
                class:active={$filters.branch === branchRef(b)}
                title={branchLabel(b)}
                on:click={() => toggleFilter(branchRef(b))}
                on:dblclick={() => checkout(b)}
                on:contextmenu={(e) => branchMenu(e, b)}
              >
                <span class="ellipsis">{b.name}</span>
              </button>
            {/each}
          {/if}
        {/each}
      {/if}
    {/if}

    <div class="section">
      <button class="section-title" on:click={() => (showTags = !showTags)}>Tags</button>
      <button class="icon-btn" title="New tag at HEAD" on:click={() => newTag(repoId, 'HEAD', 'HEAD')}>
        <Icon name="plus" size={14} />
      </button>
    </div>
    {#if showTags}
      {#each $refs.tags as t (t.name)}
        <button
          class="row-item ref"
          class:active={$filters.branch === `refs/tags/${t.name}`}
          on:click={() => toggleFilter(`refs/tags/${t.name}`)}
          on:contextmenu={(e) => tagMenu(e, t)}
        >
          <span class="mark"><Icon name="tag" size={12} /></span>
          <span class="ellipsis">{t.name}</span>
        </button>
      {:else}
        <div class="none">No tags</div>
      {/each}
    {/if}
  </div>
{/if}

<style>
  .refs { padding: 0 0 12px 12px; }
  .section { display: flex; align-items: center; justify-content: space-between; height: 30px; padding: 6px 4px 0 10px; }
  .ref { height: 26px; }
  .mark { width: 12px; flex: none; display: inline-grid; place-items: center; color: var(--muted); }
  .current { font-weight: 500; }
  .detached { color: var(--muted); }
  .nested { padding-left: 42px; }
  .none { padding: 2px 10px 0 30px; color: var(--faint); font-size: 12px; }
</style>
```

- [ ] **Step 2: Sidebar**

`frontend/src/components/Sidebar.svelte`:
```svelte
<script lang="ts">
  import Icon from './Icon.svelte'
  import RepoRefs from './RepoRefs.svelte'
  import { addRepo, fetchRepo, pullRepo, relocateRepo, removeRepo } from '../lib/actions'
  import { busy, repos, selectRepo, selectedRepoId } from '../lib/stores'
  import type { Repo } from '../lib/types'
  import { openMenu } from '../lib/ui'

  let settingsOpen = false

  function repoMenu(event: MouseEvent, repo: Repo) {
    openMenu(event, [
      ...(repo.missing ? [{ label: 'Locate…', action: () => relocateRepo(repo.id) }] : []),
      { label: 'Fetch', action: () => fetchRepo(repo.id), disabled: repo.missing || !!$busy },
      { label: 'Pull', action: () => pullRepo(repo.id), disabled: repo.missing || !!$busy },
      { label: 'Remove from list…', action: () => removeRepo(repo), danger: true },
    ])
  }
</script>

<div class="sidebar">
  <div class="titlebar drag"></div>

  <button class="row-item add" on:click={addRepo}><Icon name="plus" /> Add repo</button>

  <div class="section-title heading">Repos</div>
  <div class="list">
    {#each $repos as repo (repo.id)}
      {@const active = repo.id === $selectedRepoId}
      <div class="repo row-item" class:active class:missing={repo.missing} on:contextmenu={(e) => repoMenu(e, repo)}>
        <button class="select" on:click={() => selectRepo(repo.id)}>
          <Icon name={active ? 'chevron-down' : 'chevron-right'} size={12} />
          <span class="name ellipsis">{repo.name}</span>
          {#if repo.missing}
            <span class="badge">missing</span>
          {:else}
            <span class="branch ellipsis">{repo.branch}</span>
          {/if}
        </button>
        {#if !repo.missing}
          <span class="hover-actions">
            <button class="icon-btn" title="Fetch" disabled={!!$busy} on:click={() => fetchRepo(repo.id)}><Icon name="refresh" size={14} /></button>
            <button class="icon-btn" title="Pull" disabled={!!$busy} on:click={() => pullRepo(repo.id)}><Icon name="download" size={14} /></button>
          </span>
        {/if}
      </div>
      {#if active && !repo.missing}
        <RepoRefs repoId={repo.id} />
      {/if}
    {:else}
      <p class="empty">Add a git repository to get started.</p>
    {/each}
  </div>

  <div class="footer">
    {#if $busy}<div class="note">{$busy}</div>{/if}
    <button class="row-item" on:click={() => (settingsOpen = !settingsOpen)}><Icon name="settings" /> Settings</button>
    {#if settingsOpen}
      <div class="note">git-ui 0.1 · AI providers arrive in a later version.</div>
    {/if}
  </div>
</div>

<style>
  .sidebar { display: flex; flex-direction: column; height: 100%; padding: 0 8px 8px; }
  .titlebar { height: 40px; flex: none; }
  .add { font-weight: 500; margin-bottom: 16px; }
  .heading { padding: 0 10px 6px; }
  .list { flex: 1; overflow-y: auto; min-height: 0; }
  .repo { padding: 0 4px 0 0; }
  .select { flex: 1; min-width: 0; height: 100%; display: flex; align-items: center; gap: 8px; padding-left: 10px; color: var(--muted); }
  .name { color: var(--text); flex: none; max-width: 60%; }
  .branch { margin-left: auto; font-size: 12px; color: var(--muted); }
  .missing .name { color: var(--faint); }
  .badge { margin-left: auto; font-size: 11px; padding: 0 6px; border-radius: 4px; background: var(--hover); color: var(--muted); }
  .hover-actions { display: none; }
  .repo:hover .hover-actions { display: flex; }
  .repo:hover .branch { display: none; }
  .empty { margin: 0; padding: 6px 10px; color: var(--muted); }
  .footer { flex: none; border-top: 1px solid var(--border); padding-top: 6px; }
  .note { padding: 4px 10px; font-size: 12px; color: var(--muted); }
</style>
```

- [ ] **Step 3: Verify**

```bash
source ~/.nvm/nvm.sh && nvm use 22
cd /Users/josfh/playground/git-ui/frontend && npm run check
cd .. && ~/go/bin/wails dev
```
Expected: svelte-check 0 errors. In the dev window:
1. "Add repo" opens a folder picker; choose this project folder → it appears with branch `main` and is expanded.
2. Branches shows `main` with a check mark; Tags shows "No tags".
3. Tags `+` → name `v0.0.1` → a toast-free refresh shows the tag. Right-click it → Delete… → confirm → it disappears.
4. Branches `+` → `try/sidebar`, keep "Check out" ticked → the check mark moves to `try/sidebar` and the repo row shows it. Double-click `main` → check mark moves back. Right-click `try/sidebar` → Delete… → it's gone.
5. Hovering the repo row shows Fetch and Pull; Pull on a repo without upstream shows an error toast with git's message and a Copy button.
6. Right-click the repo → Remove from list… → confirm → it disappears; add it again.
Stop `wails dev` with Ctrl+C.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components
git commit -m "feat(ui): sidebar with repos, branches, remotes and tags"
```

---

### Task 14: Graph log with filters

**Files:**
- Modify: `frontend/src/components/LogView.svelte` (replace placeholder)
- Create: `frontend/src/components/FilterBar.svelte`
- Create: `frontend/src/components/LogList.svelte`
- Create (placeholder, replaced in Task 15): `frontend/src/components/CommitDetails.svelte`

**Interfaces:**
- Consumes: `api`, stores (`selectedRepo`, `filters`, `selectedHash`, `jumpTo`, `logVersion`, `chatOpen`, `detailsHeight`), actions (`checkoutCommit`, `newBranch`, `newTag`), `ui` (`openMenu`, `toast`, `errorMessage`, `copyText`), geometry, `relativeDate`.
- Produces: `FilterBar.svelte` and `LogList.svelte`, each with prop `repoId: string`. `LogList` reloads when `repoId`, `$filters` or `$logVersion` change; setting `jumpTo` to a hash loads pages until the commit is found, selects it and scrolls to it. `CommitDetails.svelte` takes props `repoId`, `hash`.

- [ ] **Step 1: Details placeholder**

`frontend/src/components/CommitDetails.svelte`:
```svelte
<script lang="ts">
  export let repoId: string
  export let hash: string
</script>

<div style="padding: 12px; color: var(--muted)">{repoId}: {hash}</div>
```

- [ ] **Step 2: Filter bar**

`frontend/src/components/FilterBar.svelte`:
```svelte
<script lang="ts">
  import { onDestroy } from 'svelte'
  import Icon from './Icon.svelte'
  import { api } from '../lib/api'
  import { filters, jumpTo } from '../lib/stores'

  export let repoId: string

  let text = $filters.text
  let paths = $filters.paths.join(', ')
  let authors: string[] = []
  let timer: ReturnType<typeof setTimeout> | undefined

  $: loadAuthors(repoId)

  async function loadAuthors(id: string) {
    try {
      authors = await api.getAuthors(id)
    } catch {
      authors = []
    }
  }

  function onText() {
    clearTimeout(timer)
    timer = setTimeout(async () => {
      const value = text.trim()
      if (/^[0-9a-f]{4,40}$/i.test(value)) {
        const hash = await api.resolveCommit(repoId, value).catch(() => '')
        if (hash) {
          filters.update((f) => ({ ...f, text: '' }))
          jumpTo.set(hash)
          return
        }
      }
      filters.update((f) => ({ ...f, text: value }))
    }, 300)
  }

  onDestroy(() => clearTimeout(timer))

  const update = (key: 'author' | 'since' | 'until') => (event: Event) =>
    filters.update((f) => ({ ...f, [key]: (event.target as HTMLInputElement | HTMLSelectElement).value }))

  function applyPaths() {
    filters.update((f) => ({ ...f, paths: paths.split(',').map((p) => p.trim()).filter(Boolean) }))
  }

  const shortRef = (ref: string) => ref.replace(/^refs\/(heads|remotes|tags)\//, '')
</script>

<div class="bar">
  <label class="search">
    <Icon name="search" size={14} />
    <input placeholder="Text or hash" bind:value={text} on:input={onText} />
  </label>
  {#if $filters.branch}
    <button class="chip" title="Show all branches" on:click={() => filters.update((f) => ({ ...f, branch: '' }))}>
      <span class="ellipsis">{shortRef($filters.branch)}</span>
      <Icon name="x" size={12} />
    </button>
  {:else}
    <span class="chip idle">All branches</span>
  {/if}
  <select value={$filters.author} on:change={update('author')} title="User">
    <option value="">All users</option>
    {#each authors as author}
      <option value={author}>{author}</option>
    {/each}
  </select>
  <input type="date" value={$filters.since} on:change={update('since')} title="Since" />
  <input type="date" value={$filters.until} on:change={update('until')} title="Until" />
  <input class="paths" placeholder="Paths, comma separated" bind:value={paths} on:change={applyPaths} />
</div>

<style>
  .bar { display: flex; align-items: center; gap: 8px; padding: 0 12px 10px; flex-wrap: wrap; }
  .search { display: flex; align-items: center; gap: 6px; padding-left: 8px; border: 1px solid var(--border); border-radius: 7px; color: var(--muted); background: var(--surface); }
  .search input { border: 0; padding-left: 0; width: 180px; }
  .chip { display: inline-flex; align-items: center; gap: 4px; max-width: 220px; height: 26px; padding: 0 8px; border-radius: 13px; background: var(--active); }
  .chip.idle { background: none; color: var(--muted); }
  select { height: 28px; max-width: 160px; }
  input[type='date'] { height: 28px; }
  .paths { flex: 1; min-width: 140px; }
</style>
```

- [ ] **Step 3: Log list with canvas graph**

`frontend/src/components/LogList.svelte`:
```svelte
<script lang="ts">
  import { onDestroy, tick } from 'svelte'
  import { api } from '../lib/api'
  import { checkoutCommit, newBranch, newTag } from '../lib/actions'
  import { relativeDate } from '../lib/format'
  import {
    arrowAt, DOT_RADIUS, edgeSegment, graphWidth, laneColor, laneX, ROW_HEIGHT, rowCenterY, visibleRange,
  } from '../lib/geometry'
  import { filters, jumpTo, logVersion, selectedHash } from '../lib/stores'
  import type { LogRow } from '../lib/types'
  import { copyText, errorMessage, openMenu, toast } from '../lib/ui'

  export let repoId: string

  const PAGE = 500
  const MAX_JUMP_PAGES = 20

  let rows: LogRow[] = []
  let byHash = new Map<string, number>()
  let hasMore = false
  let graphVisible = true
  let shallow = false
  let error = ''
  let generation = 0
  let loadingGen = -1

  let scroller: HTMLDivElement
  let canvas: HTMLCanvasElement
  let scrollTop = 0
  let viewport = 0
  let hover: { x: number; y: number; text: string } | null = null

  $: reload(repoId, $filters, $logVersion)

  async function reload(..._deps: unknown[]) {
    const gen = ++generation
    rows = []
    byHash = new Map()
    hasMore = false
    error = ''
    if (scroller) scroller.scrollTop = 0
    scrollTop = 0
    api.isShallow(repoId).then((s) => gen === generation && (shallow = s)).catch(() => {})
    await loadPage(gen)
  }

  async function loadPage(gen: number) {
    if (loadingGen === gen) return
    loadingGen = gen
    try {
      const page = await api.getLog(repoId, $filters, rows.length, PAGE)
      if (gen !== generation) return
      page.rows.forEach((r, i) => byHash.set(r.hash, rows.length + i))
      rows = rows.concat(page.rows)
      hasMore = page.hasMore
      graphVisible = page.graphVisible
    } catch (e) {
      if (gen !== generation) return
      const message = errorMessage(e)
      if (message.includes('stale')) reload()
      else error = message
    } finally {
      if (loadingGen === gen) loadingGen = -1
    }
  }

  function onScroll() {
    scrollTop = scroller.scrollTop
    hover = null
    const nearEnd = scrollTop + viewport > (rows.length - 100) * ROW_HEIGHT
    if (hasMore && loadingGen === -1 && nearEnd) loadPage(generation)
  }

  $: range = visibleRange(scrollTop, viewport, rows.length)
  $: width = graphVisible ? graphWidth(rows) : 12
  $: draw(canvas, rows, range, width, viewport, scrollTop, graphVisible)

  function draw(..._deps: unknown[]) {
    if (!canvas || !graphVisible) return
    const dpr = window.devicePixelRatio || 1
    canvas.width = width * dpr
    canvas.height = viewport * dpr
    canvas.style.width = `${width}px`
    canvas.style.height = `${viewport}px`
    const ctx = canvas.getContext('2d')
    if (!ctx) return
    ctx.setTransform(dpr, 0, 0, dpr, 0, -scrollTop * dpr)
    ctx.clearRect(0, scrollTop, width, viewport)
    ctx.lineCap = 'round'
    ctx.lineJoin = 'round'

    const last = Math.min(rows.length, range.end + 1)
    for (let i = range.start; i < last; i++) {
      for (const edge of rows[i].edges) {
        const s = edgeSegment(edge, i)
        ctx.strokeStyle = laneColor(s.color)
        ctx.lineWidth = 1.6
        ctx.beginPath()
        ctx.moveTo(s.x1, s.y1)
        ctx.lineTo(s.x2, s.y2)
        ctx.stroke()
        if (s.arrow !== 'none') {
          const tip = s.arrow === 'down' ? s.y2 : s.y1
          const back = s.arrow === 'down' ? -5 : 5
          ctx.beginPath()
          ctx.moveTo(s.x1 - 4, tip + back)
          ctx.lineTo(s.x1, tip)
          ctx.lineTo(s.x1 + 4, tip + back)
          ctx.stroke()
        }
      }
    }

    const surface = getComputedStyle(canvas).getPropertyValue('--surface').trim() || '#fff'
    for (let i = range.start; i < range.end; i++) {
      const r = rows[i]
      ctx.beginPath()
      ctx.arc(laneX(r.lane), rowCenterY(i), DOT_RADIUS, 0, Math.PI * 2)
      if (r.isHead) {
        ctx.fillStyle = surface
        ctx.fill()
        ctx.strokeStyle = laneColor(r.color)
        ctx.lineWidth = 2
        ctx.stroke()
      } else {
        ctx.fillStyle = laneColor(r.color)
        ctx.fill()
      }
    }
  }

  function graphPoint(event: MouseEvent) {
    const rect = canvas.getBoundingClientRect()
    return { x: event.clientX - rect.left, y: event.clientY - rect.top + scrollTop }
  }

  function onGraphMove(event: MouseEvent) {
    const p = graphPoint(event)
    const s = arrowAt(rows, p.x, p.y)
    if (!s?.target) {
      hover = null
      return
    }
    const index = byHash.get(s.target)
    hover = {
      x: event.clientX + 12,
      y: event.clientY + 12,
      text: index === undefined ? `Go to ${s.target.slice(0, 8)}` : rows[index].subject,
    }
  }

  function onGraphClick(event: MouseEvent) {
    const p = graphPoint(event)
    const s = arrowAt(rows, p.x, p.y)
    if (s?.target) {
      jumpTo.set(s.target)
      return
    }
    const row = rows[Math.floor(p.y / ROW_HEIGHT)]
    if (row) selectedHash.set(row.hash)
  }

  function onGraphContext(event: MouseEvent) {
    const row = rows[Math.floor(graphPoint(event).y / ROW_HEIGHT)]
    if (row) commitMenu(event, row)
  }

  function commitMenu(event: MouseEvent, row: LogRow) {
    selectedHash.set(row.hash)
    openMenu(event, [
      { label: 'Check out (detached)…', action: () => checkoutCommit(repoId, row.hash) },
      { label: 'New branch here…', action: () => newBranch(repoId, row.hash, row.short) },
      { label: 'New tag here…', action: () => newTag(repoId, row.hash, row.short) },
      { label: 'Copy hash', action: () => copyText(row.hash) },
    ])
  }

  async function jump(hash: string) {
    const gen = generation
    while (loadingGen === gen) await new Promise((resolve) => setTimeout(resolve, 50))
    for (let n = 0; !byHash.has(hash) && hasMore && n < MAX_JUMP_PAGES && gen === generation; n++) {
      await loadPage(gen)
    }
    const index = byHash.get(hash)
    if (index === undefined) {
      toast('That commit is not in the current view. Clear the filters and try again.')
      return
    }
    selectedHash.set(hash)
    await tick()
    scroller.scrollTop = Math.max(0, index * ROW_HEIGHT - viewport / 2)
  }

  const stopJump = jumpTo.subscribe((hash) => {
    if (!hash) return
    jumpTo.set('')
    jump(hash)
  })
  onDestroy(stopJump)
</script>

<div class="log">
  <div class="scroller" bind:this={scroller} bind:clientHeight={viewport} on:scroll={onScroll}>
    <div class="spacer" style="height: {(rows.length + (shallow && !hasMore && rows.length ? 1 : 0)) * ROW_HEIGHT}px">
      {#if graphVisible}
        <canvas
          bind:this={canvas}
          style="top: {scrollTop}px"
          on:mousemove={onGraphMove}
          on:mouseleave={() => (hover = null)}
          on:click={onGraphClick}
          on:contextmenu={onGraphContext}
        ></canvas>
      {/if}
      {#each rows.slice(range.start, range.end) as row, i (row.hash)}
        <button
          class="row"
          class:selected={row.hash === $selectedHash}
          class:merge={row.isMerge}
          style="top: {(range.start + i) * ROW_HEIGHT}px; padding-left: {width}px"
          on:click={() => selectedHash.set(row.hash)}
          on:contextmenu={(e) => commitMenu(e, row)}
        >
          <span class="subject ellipsis">
            {#each row.refs.filter((r) => r.kind !== 'head') as ref}
              <span class="badge {ref.kind}">{ref.name}</span>
            {/each}
            {row.subject}
          </span>
          <span class="author ellipsis">{row.author}</span>
          <span class="date">{relativeDate(row.date)}</span>
          <span class="hash mono">{row.short}</span>
        </button>
      {/each}
      {#if shallow && !hasMore && rows.length}
        <div class="note" style="top: {rows.length * ROW_HEIGHT}px">Shallow clone: older history is not available locally.</div>
      {/if}
    </div>
  </div>

  {#if error}
    <div class="overlay error">{error}</div>
  {:else if rows.length === 0 && loadingGen === -1}
    <div class="overlay">No commits yet</div>
  {/if}

  {#if hover}
    <div class="tooltip" style="left: {hover.x}px; top: {hover.y}px">{hover.text}</div>
  {/if}
</div>

<style>
  .log { position: relative; height: 100%; }
  .scroller { height: 100%; overflow-y: auto; overflow-x: hidden; }
  .spacer { position: relative; }
  canvas { position: absolute; left: 0; z-index: 2; }
  .row {
    position: absolute;
    left: 0;
    right: 0;
    height: 28px;
    display: grid;
    grid-template-columns: minmax(0, 1fr) 150px 90px 72px;
    align-items: center;
    gap: 12px;
    padding-right: 12px;
    text-align: left;
  }
  .row:hover { background: var(--hover); }
  .row.selected { background: var(--selection); }
  .merge .subject { color: var(--merge-text); }
  .author, .date, .hash { color: var(--muted); font-size: 12px; }
  .badge { display: inline-block; margin-right: 6px; padding: 0 6px; border-radius: 4px; font-size: 11px; line-height: 17px; background: var(--hover); color: var(--muted); }
  .badge.local { color: var(--text); background: var(--active); }
  .badge.tag { color: var(--accent); }
  .note { position: absolute; left: 0; right: 0; height: 28px; line-height: 28px; text-align: center; font-size: 12px; color: var(--faint); }
  .overlay { position: absolute; inset: 0; display: grid; place-items: center; color: var(--muted); pointer-events: none; }
  .overlay.error { color: var(--danger); padding: 24px; white-space: pre-wrap; user-select: text; }
  .tooltip { position: fixed; z-index: 30; max-width: 360px; padding: 4px 8px; border-radius: 6px; background: var(--text); color: var(--bg); font-size: 12px; pointer-events: none; }
</style>
```

- [ ] **Step 4: Log view**

`frontend/src/components/LogView.svelte`:
```svelte
<script lang="ts">
  import CommitDetails from './CommitDetails.svelte'
  import FilterBar from './FilterBar.svelte'
  import Icon from './Icon.svelte'
  import LogList from './LogList.svelte'
  import Splitter from './Splitter.svelte'
  import { chatOpen, detailsHeight, selectedHash, selectedRepo } from '../lib/stores'
</script>

<div class="log-view">
  <header class="drag">
    <div class="title">
      {#if $selectedRepo}
        <span>{$selectedRepo.name}</span>
        <span class="path ellipsis">{$selectedRepo.path}</span>
      {/if}
    </div>
    {#if !$chatOpen}
      <button class="icon-btn" title="Show chat" on:click={() => chatOpen.set(true)}><Icon name="panel-right" /></button>
    {/if}
  </header>

  {#if $selectedRepo && !$selectedRepo.missing}
    {#key $selectedRepo.id}
      <FilterBar repoId={$selectedRepo.id} />
      <div class="list"><LogList repoId={$selectedRepo.id} /></div>
      {#if $selectedHash}
        <Splitter direction="horizontal" on:drag={(e) => detailsHeight.set(Math.min(720, Math.max(120, $detailsHeight - e.detail)))} />
        <div class="details" style="height: {$detailsHeight}px">
          <CommitDetails repoId={$selectedRepo.id} hash={$selectedHash} />
        </div>
      {/if}
    {/key}
  {:else if $selectedRepo}
    <div class="empty">This repository was moved or deleted. Right-click it in the sidebar and choose Locate…</div>
  {:else}
    <div class="empty">Select or add a repository.</div>
  {/if}
</div>

<style>
  .log-view { display: flex; flex-direction: column; height: 100%; }
  header { display: flex; align-items: center; gap: 8px; height: 44px; padding: 0 10px 0 14px; flex: none; }
  .title { flex: 1; min-width: 0; display: flex; align-items: baseline; gap: 8px; font-weight: 500; }
  .path { font-weight: 400; font-size: 12px; color: var(--faint); }
  .list { flex: 1; min-height: 0; border-top: 1px solid var(--border); }
  .details { flex: none; min-height: 0; overflow: hidden; }
  .empty { flex: 1; display: grid; place-items: center; padding: 24px; color: var(--muted); text-align: center; }
</style>
```

- [ ] **Step 5: Verify**

```bash
source ~/.nvm/nvm.sh && nvm use 22
cd /Users/josfh/playground/git-ui/frontend && npm run check && npm test
cd .. && ~/go/bin/wails dev
```
Expected: svelte-check 0 errors, tests pass. In the dev window, add a real repo with a lot of merges (for example an escala backend repo) and check:
1. The graph shows colored lanes; merge rows are gray; the HEAD commit is a ring; ref badges appear on commits.
2. Scrolling is smooth and more commits load past row ~400; lines connect across the page boundary.
3. Long-lived branch lines end with a down arrow; hovering it shows the target commit's subject; clicking it scrolls to and selects that commit.
4. Clicking a branch in the sidebar filters the log and shows a chip; clicking the chip's × clears it.
5. Typing a word filters by message and the graph disappears; clearing restores it. Typing a 7+ character hash jumps to that commit.
6. User, Since/Until and Paths filters narrow the list.
7. Right-click a commit → Copy hash → toast "Copied".
8. Hide the chat, then use the header button to show it again.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components
git commit -m "feat(ui): graph log with virtual scrolling, arrows and filters"
```

---

### Task 15: Commit details pane

**Files:**
- Modify: `frontend/src/components/CommitDetails.svelte` (replace placeholder)

**Interfaces:**
- Consumes: `api.getDetails`, `api.getDiff`, `jumpTo`, `copyText`, `errorMessage`, types `Details`, `FileChange`.
- Produces: `CommitDetails.svelte` with props `repoId: string`, `hash: string`. Shows subject, body, author, date, hash (click to copy), parents (click to jump), changed files (first one opened automatically) and the selected file's diff, capped at 5000 lines.

- [ ] **Step 1: Implement**

`frontend/src/components/CommitDetails.svelte`:
```svelte
<script lang="ts">
  import { api } from '../lib/api'
  import { jumpTo } from '../lib/stores'
  import type { Details, FileChange } from '../lib/types'
  import { copyText, errorMessage } from '../lib/ui'

  export let repoId: string
  export let hash: string

  const MAX_LINES = 5000

  let details: Details | null = null
  let error = ''
  let file: FileChange | null = null
  let diff = ''
  let request = 0

  $: load(repoId, hash)
  $: lines = diff.split('\n')

  async function load(id: string, h: string) {
    const current = ++request
    details = null
    error = ''
    file = null
    diff = ''
    try {
      const d = await api.getDetails(id, h)
      if (current !== request) return
      details = d
      if (d.files.length) openFile(d.files[0])
    } catch (e) {
      if (current === request) error = errorMessage(e)
    }
  }

  async function openFile(f: FileChange) {
    if (!details) return
    const current = request
    file = f
    diff = ''
    const paths = f.oldPath ? [f.oldPath, f.path] : [f.path]
    try {
      const text = await api.getDiff(repoId, details.parents[0] ?? '', details.hash, paths)
      if (current === request && file === f) diff = text
    } catch (e) {
      if (current === request && file === f) diff = errorMessage(e)
    }
  }

  function lineClass(line: string): string {
    if (line.startsWith('+++') || line.startsWith('---') || line.startsWith('diff ') || line.startsWith('index ')) return 'meta'
    if (line.startsWith('@@')) return 'hunk'
    if (line.startsWith('+')) return 'add'
    if (line.startsWith('-')) return 'del'
    return ''
  }
</script>

<div class="details">
  {#if error}
    <div class="error">{error}</div>
  {:else if details}
    <div class="info">
      <div class="subject">{details.subject}</div>
      {#if details.body}<pre class="body">{details.body}</pre>{/if}
      <div class="meta">
        <span>{details.author} &lt;{details.email}&gt;</span>
        <span>{new Date(details.date).toLocaleString()}</span>
        <span class="links">
          <button class="mono link" title="Copy full hash" on:click={() => copyText(details?.hash ?? '')}>{details.short}</button>
          {#each details.parents as parent}
            <button class="mono link" title="Go to parent" on:click={() => jumpTo.set(parent)}>↑ {parent.slice(0, 7)}</button>
          {/each}
        </span>
      </div>
      <div class="files">
        {#each details.files as f (f.path)}
          <button
            class="row-item file"
            class:active={file === f}
            title={f.oldPath ? `${f.oldPath} → ${f.path}` : f.path}
            on:click={() => openFile(f)}
          >
            <span class="status s-{f.status}">{f.status}</span>
            <span class="ellipsis">{f.path}</span>
          </button>
        {:else}
          <div class="none">No file changes</div>
        {/each}
      </div>
    </div>
    <div class="diff mono">
      {#each lines.slice(0, MAX_LINES) as line}
        <div class="line {lineClass(line)}">{line || ' '}</div>
      {/each}
      {#if lines.length > MAX_LINES}
        <div class="line meta">… diff truncated after {MAX_LINES} lines</div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .details { display: grid; grid-template-columns: minmax(260px, 36%) 1fr; height: 100%; }
  .info { overflow-y: auto; padding: 12px 8px 12px 14px; border-right: 1px solid var(--border); user-select: text; }
  .subject { font-weight: 600; margin-bottom: 6px; }
  .body { color: var(--muted); margin-bottom: 8px; }
  .meta { display: flex; flex-direction: column; gap: 2px; font-size: 12px; color: var(--muted); margin-bottom: 10px; }
  .links { display: flex; flex-wrap: wrap; gap: 8px; }
  .link { color: var(--accent); }
  .link:hover { text-decoration: underline; }
  .file { height: 24px; font-size: 12px; }
  .status { width: 14px; flex: none; font-family: var(--mono); font-weight: 600; color: var(--muted); }
  .s-A { color: #4f9d4f; }
  .s-D { color: var(--danger); }
  .s-R, .s-C { color: #3f7fbf; }
  .none { padding: 4px 10px; color: var(--faint); font-size: 12px; }
  .diff { overflow: auto; padding: 8px 0; user-select: text; }
  .line { padding: 0 12px; white-space: pre; line-height: 18px; }
  .add { background: var(--add-bg); }
  .del { background: var(--del-bg); }
  .hunk { color: #3f7fbf; }
  .meta { color: var(--faint); }
  .error { padding: 12px; color: var(--danger); white-space: pre-wrap; }
</style>
```

Note: `.meta` is used both for the metadata block and diff header lines; the diff rule only changes color, which is intended for both.

- [ ] **Step 2: Verify**

```bash
source ~/.nvm/nvm.sh && nvm use 22
cd /Users/josfh/playground/git-ui/frontend && npm run check
cd .. && ~/go/bin/wails dev
```
Expected: 0 errors. In the dev window:
1. Selecting a commit opens the details pane below the log with subject, body, author, date and files; the first file's diff is shown with green/red lines.
2. Clicking another file loads its diff; a renamed file shows `old → new` on hover.
3. Clicking the short hash copies it; clicking a parent link selects and scrolls to the parent.
4. Selecting a merge commit lists files changed against its first parent.
5. Dragging the divider above the pane resizes it, and the height persists after relaunch.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/CommitDetails.svelte
git commit -m "feat(ui): commit details with changed files and diff"
```

---

### Task 16: End-to-end verification and release build

**Files:**
- Modify: `docs/superpowers/specs/2026-09-16-git-ui-design.md` (record the planning deviations)
- Create: `README.md` (replace the template's)

**Interfaces:**
- Consumes: the whole app.
- Produces: `build/bin/git-ui.app` verified against the spec checklist.

- [ ] **Step 1: Record planning deviations in the spec**

In `docs/superpowers/specs/2026-09-16-git-ui-design.md`:
- In "Data flow — log" and the `Row` code block, replace the `EdgeKind` comment with `// Line, ArrowDown, ArrowUp` and state that `graph` takes `[]graph.Node{Hash, Parents}` and returns `[]graph.Row{Lane, Color, Edges}`, joined with commits into `app.LogRow` by `internal/app`.
- Add under "Data flow — log": "The graph is hidden when the text, author, since or until filter is set; branch and path filters keep it."
- Add under "Layout → Sidebar": "Clicking a ref sets the log's branch filter, shown as a removable chip in the filter bar."

- [ ] **Step 2: README**

`README.md`:
````markdown
# git-ui

AI-first desktop Git client (Wails + Go + Svelte). Sub-project 1: core viewer.

## Requirements

- macOS, Go 1.26, git ≥ 2.28
- Node 22 (`nvm use 22`)
- Wails CLI: `go install github.com/wailsapp/wails/v2/cmd/wails@latest`

## Develop

```bash
~/go/bin/wails dev
```

## Test

```bash
go test ./...
cd frontend && npm test && npm run check
```

## Build

```bash
~/go/bin/wails build   # → build/bin/git-ui.app
```
````

- [ ] **Step 3: Full automated check**

```bash
source ~/.nvm/nvm.sh && nvm use 22
cd /Users/josfh/playground/git-ui
go vet ./... && go test ./...
(cd frontend && npm test && npm run check)
~/go/bin/wails build
```
Expected: every Go package `ok`, Vitest passes, svelte-check 0 errors, build succeeds.

- [ ] **Step 4: Manual checklist on the release build**

Run `open build/bin/git-ui.app` (launched from Finder, so it uses the GUI PATH and SSH agent) and confirm each item; fix and re-run anything that fails before moving on:
1. Repos added in earlier runs are still listed; a repo folder renamed on disk shows "missing", and Locate… fixes it.
2. Fetch on a Bitbucket repo succeeds over SSH without prompting; with the network off it fails with a readable toast instead of hanging.
3. Pull fast-forwards when behind; when diverged it shows the "cannot fast-forward" message.
4. Starting Fetch and immediately Pull on the same repo: the second is disabled or reports it's busy.
5. Checkout with conflicting local changes shows git's "would be overwritten" message.
6. Deleting an unmerged local branch asks a second time before force deleting; deleting a remote branch names the remote in the confirmation.
7. Committing in a terminal, then switching back to the app, refreshes refs and log automatically.
8. An empty repo (`git init` in a temp folder) shows "No commits yet"; a `git clone --depth 20` repo shows the shallow note at the end of the log.
9. Light and dark mode both look right; the window drags from the top of each column.
10. The chat panel shows the provider placeholder and collapses/restores.

- [ ] **Step 5: Commit**

```bash
git add README.md docs/superpowers/specs/2026-09-16-git-ui-design.md
git commit -m "docs: README and spec updates for core viewer"
```
