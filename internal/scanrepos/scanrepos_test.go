package scanrepos_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"git-ui/internal/clone"
	"git-ui/internal/scanrepos"
)

// mkdirs creates each relative path (folders) under root.
func mkdirs(t *testing.T, root string, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if err := os.MkdirAll(filepath.Join(root, p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func names(r scanrepos.Result) []string {
	out := []string{}
	for _, c := range r.Repos {
		out = append(out, c.Name)
	}
	return out
}

func TestScanListsSortsAndSkips(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root,
		"beta/.git", "Alpha/.git", "My Repo/.git", "ñandú/.git", // repos
		"plain",             // folder without .git: skipped
		".hidden/.git",      // hidden: neither listed nor counted
		"nested/inner/.git", // only direct children count: skipped
	)
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A child whose .git is a file (linked worktree / submodule checkout): skipped.
	mkdirs(t, root, "linked")
	if err := os.WriteFile(filepath.Join(root, "linked", ".git"), []byte("gitdir: /elsewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A symlink to a repository is listed; a broken symlink is skipped.
	other := t.TempDir()
	mkdirs(t, other, "target/.git")
	if err := os.Symlink(filepath.Join(other, "target"), filepath.Join(root, "linkrepo")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(other, "gone"), filepath.Join(root, "broken")); err != nil {
		t.Fatal(err)
	}

	got, err := scanrepos.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Alpha", "beta", "linkrepo", "My Repo", "ñandú"}
	if !reflect.DeepEqual(names(got), want) {
		t.Fatalf("repos = %v, want %v", names(got), want)
	}
	// plain, nested, notes.txt, linked, broken
	if got.Skipped != 5 {
		t.Fatalf("skipped = %d, want 5", got.Skipped)
	}
	if got.Parent != filepath.Clean(root) {
		t.Fatalf("parent = %q", got.Parent)
	}
	if got.Repos[0].Path != filepath.Join(root, "Alpha") {
		t.Fatalf("path = %q", got.Repos[0].Path)
	}
}

func TestScanTrimsAndExpands(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	mkdirs(t, home, "projects/app/.git")

	for _, typed := range []string{"~/projects", " ~/projects/ ", home + "/projects/"} {
		got, err := scanrepos.Scan(typed)
		if err != nil {
			t.Fatalf("%q: %v", typed, err)
		}
		if got.Parent != filepath.Join(home, "projects") || !reflect.DeepEqual(names(got), []string{"app"}) {
			t.Fatalf("%q: %+v", typed, got)
		}
	}
}

func TestScanParentIsRepo(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, ".git", "src", "vendor/lib")
	if err := os.WriteFile(filepath.Join(root, "vendor", "lib", ".git"), []byte("gitdir: ../../.git/modules/lib\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := scanrepos.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Repos) != 0 || got.Skipped != 2 {
		t.Fatalf("got %+v, want no repos and 2 skipped", got)
	}
}

func TestScanEmptyFolderGivesNonNilList(t *testing.T) {
	got, err := scanrepos.Scan(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got.Repos == nil || len(got.Repos) != 0 || got.Skipped != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestScanRefusals(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "f")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		parent string
		want   error
	}{
		{"relative/path", clone.ErrParentNotAbsolute},
		{filepath.Join(root, "missing"), scanrepos.ErrParentMissing},
		{file, scanrepos.ErrParentNotDir},
	}
	for _, c := range cases {
		if _, err := scanrepos.Scan(c.parent); !errors.Is(err, c.want) {
			t.Errorf("Scan(%q) = %v, want %v", c.parent, err, c.want)
		}
	}
	if scanrepos.ErrParentMissing.Error() != "That folder doesn't exist." ||
		scanrepos.ErrParentNotDir.Error() != "That path isn't a folder." {
		t.Fatal("messages changed")
	}
}

func TestScanUnreadable(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permissions not enforced")
	}
	root := filepath.Join(t.TempDir(), "locked")
	mkdirs(t, root, "a/.git")
	if err := os.Chmod(root, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(root, 0o755) })
	_, err := scanrepos.Scan(root)
	if !errors.Is(err, scanrepos.ErrParentUnreadable) || !strings.HasPrefix(err.Error(), "That folder can't be read: ") {
		t.Fatalf("err = %v", err)
	}
}

// Scanning never runs git: with no git on PATH it still works.
func TestScanRunsNoGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed anyway")
	}
	root := t.TempDir()
	mkdirs(t, root, "a/.git")
	t.Setenv("PATH", t.TempDir())
	got, err := scanrepos.Scan(root)
	if err != nil || len(got.Repos) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
}
