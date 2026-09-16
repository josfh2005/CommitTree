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
