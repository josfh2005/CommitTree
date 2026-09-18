package ops_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git-ui/internal/gitcmd"
	"git-ui/internal/ops"
	"git-ui/internal/testrepo"
)

// threeCommits is a repository at c3 whose last commit changed a.txt; it
// returns the hashes of c1, c2 and c3.
func threeCommits(t *testing.T) (*testrepo.Repo, [3]string) {
	t.Helper()
	r := testrepo.New(t)
	var h [3]string
	for i, content := range []string{"one\n", "two\n", "three\n"} {
		r.WriteFile("a.txt", content)
		r.Git("add", "a.txt")
		r.Git("commit", "-q", "-m", "c"+string(rune('1'+i)))
		h[i] = r.Git("rev-parse", "HEAD")
	}
	return r, h
}

func read(t *testing.T, r *testrepo.Repo, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(r.Dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestResetSoftKeepsTheChangesStaged(t *testing.T) {
	r, h := threeCommits(t)
	if err := ops.Reset(ctx, r.Dir, h[0], ops.ResetSoft); err != nil {
		t.Fatal(err)
	}
	if got := r.Git("rev-parse", "HEAD"); got != h[0] {
		t.Errorf("HEAD = %s, want c1", got)
	}
	if got := r.Git("diff", "--cached", "--name-only"); got != "a.txt" {
		t.Errorf("staged = %q, want a.txt", got)
	}
	if got := read(t, r, "a.txt"); got != "three\n" {
		t.Errorf("a.txt = %q, want the worktree untouched", got)
	}
}

func TestResetMixedKeepsTheChangesUnstaged(t *testing.T) {
	r, h := threeCommits(t)
	if err := ops.Reset(ctx, r.Dir, h[0], ops.ResetMixed); err != nil {
		t.Fatal(err)
	}
	if got := r.Git("diff", "--cached", "--name-only"); got != "" {
		t.Errorf("staged = %q, want nothing", got)
	}
	if got := r.Git("diff", "--name-only"); got != "a.txt" {
		t.Errorf("unstaged = %q, want a.txt", got)
	}
}

func TestResetHardDiscardsEverything(t *testing.T) {
	r, h := threeCommits(t)
	r.WriteFile("a.txt", "uncommitted\n")
	if err := ops.Reset(ctx, r.Dir, h[0], ops.ResetHard); err != nil {
		t.Fatal(err)
	}
	if got := read(t, r, "a.txt"); got != "one\n" {
		t.Errorf("a.txt = %q, want c1's content", got)
	}
	if got := r.Git("status", "--porcelain"); got != "" {
		t.Errorf("status = %q, want clean", got)
	}
}

func TestResetRefusesAnUnknownMode(t *testing.T) {
	r, h := threeCommits(t)
	if err := ops.Reset(ctx, r.Dir, h[0], ops.ResetMode("keep")); err == nil {
		t.Error("want an error for an unknown mode")
	}
	if got := r.Git("rev-parse", "HEAD"); got != h[2] {
		t.Error("HEAD moved")
	}
}

// Only a full commit hash is accepted: never a ref, an option or a range.
func TestResetRefusesAnythingButACommitHash(t *testing.T) {
	r, h := threeCommits(t)
	blob := r.Git("rev-parse", "HEAD:a.txt")
	for _, target := range []string{"", "--hard", "HEAD~1", "main", h[0][:7], blob, strings.Repeat("0", 40)} {
		if err := ops.Reset(ctx, r.Dir, target, ops.ResetHard); !errors.Is(err, ops.ErrInvalidRef) {
			t.Errorf("Reset(%q) = %v, want ErrInvalidRef", target, err)
		}
	}
	if got := r.Git("rev-parse", "HEAD"); got != h[2] {
		t.Error("HEAD moved")
	}
}

func TestResetRefusesDetachedHead(t *testing.T) {
	r, h := threeCommits(t)
	r.Git("switch", "-q", "--detach", h[1])
	if err := ops.Reset(ctx, r.Dir, h[0], ops.ResetSoft); !errors.Is(err, ops.ErrDetached) {
		t.Errorf("err = %v, want ErrDetached", err)
	}
}

func TestResetRefusesDuringAMerge(t *testing.T) {
	r, h := threeCommits(t)
	r.Git("switch", "-q", "-c", "feature", h[1])
	r.WriteFile("a.txt", "feature\n")
	r.Git("commit", "-q", "-am", "feature")
	r.Git("switch", "-q", "main")
	// The merge conflicts and stops, which is the point: MERGE_HEAD exists.
	_, _ = gitcmd.Run(ctx, r.Dir, gitcmd.ReadTimeout, "merge", "feature")
	if err := ops.Reset(ctx, r.Dir, h[0], ops.ResetHard); !errors.Is(err, ops.ErrMerging) {
		t.Errorf("err = %v, want ErrMerging", err)
	}
}

func TestResetPreviewCountsUndoneCommits(t *testing.T) {
	r, h := threeCommits(t)
	p, err := ops.ResetPreview(ctx, r.Dir, h[0])
	if err != nil {
		t.Fatal(err)
	}
	if p.Undone != 2 || p.Pushed != 0 || p.Upstream != "" {
		t.Errorf("preview = %+v, want 2 undone, no upstream", p)
	}
}

func TestResetPreviewCountsPushedCommits(t *testing.T) {
	a, _ := clones(t)
	base := a.Git("rev-parse", "HEAD")
	a.Commit("pushed")
	a.Git("push", "-q", "origin", "main")
	a.Commit("local only")
	p, err := ops.ResetPreview(ctx, a.Dir, base)
	if err != nil {
		t.Fatal(err)
	}
	if p.Undone != 2 || p.Pushed != 1 || p.Upstream != "origin/main" {
		t.Errorf("preview = %+v, want 2 undone, 1 pushed to origin/main", p)
	}
}

// A mixed or hard reset mid-cherry-pick would throw away its state; git
// itself only stops the soft one.
func TestResetRefusesDuringACherryPick(t *testing.T) {
	r, h := threeCommits(t)
	r.Git("switch", "-q", "-c", "other", h[0])
	r.WriteFile("a.txt", "other\n")
	r.Git("commit", "-q", "-am", "other")
	pick := r.Git("rev-parse", "HEAD")
	r.Git("switch", "-q", "main")
	_, _ = gitcmd.Run(ctx, r.Dir, gitcmd.ReadTimeout, "cherry-pick", pick) // conflicts
	for _, mode := range []ops.ResetMode{ops.ResetSoft, ops.ResetMixed, ops.ResetHard} {
		if err := ops.Reset(ctx, r.Dir, h[0], mode); !errors.Is(err, ops.ErrInProgress) {
			t.Errorf("%s: err = %v, want ErrInProgress", mode, err)
		}
	}
}

// Resetting to a commit on another line of history also brings its commits
// in; the preview must say so, not only what is undone.
func TestResetPreviewCountsCommitsGained(t *testing.T) {
	r, h := threeCommits(t)
	r.Git("switch", "-q", "-c", "other", h[0])
	r.WriteFile("b.txt", "b\n")
	r.Git("add", "b.txt")
	r.Git("commit", "-q", "-m", "other")
	target := r.Git("rev-parse", "HEAD")
	r.Git("switch", "-q", "main")
	p, err := ops.ResetPreview(ctx, r.Dir, target)
	if err != nil {
		t.Fatal(err)
	}
	if p.Undone != 2 || p.Gained != 1 {
		t.Errorf("preview = %+v, want 2 undone and 1 gained", p)
	}
}
