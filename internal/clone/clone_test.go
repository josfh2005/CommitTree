package clone

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestParseProgress(t *testing.T) {
	cases := []struct {
		line string
		want Progress
		ok   bool
	}{
		{"Cloning into 'cl'...", Progress{Phase: "Cloning into 'cl'", Percent: -1}, true},
		{"Cloning into '/Users/me/src/app/vendor/lib'...", Progress{Phase: "Cloning into 'lib'", Percent: -1}, true},
		{"remote: Enumerating objects: 5466, done.", Progress{Phase: "Enumerating objects", Percent: -1, Detail: "5466"}, true},
		{"remote: Counting objects:   1% (55/5466)", Progress{Phase: "Counting objects", Percent: 1, Detail: "55/5466"}, true},
		{"remote: Compressing objects: 100% (2142/2142), done.", Progress{Phase: "Compressing objects", Percent: 100, Detail: "2142/2142"}, true},
		{"Receiving objects:  45% (2460/5466), 3.10 MiB | 6.01 MiB/s", Progress{Phase: "Receiving objects", Percent: 45, Detail: "2460/5466, 3.10 MiB | 6.01 MiB/s"}, true},
		{"Receiving objects: 100% (5466/5466), 8.92 MiB | 6.92 MiB/s, done.", Progress{Phase: "Receiving objects", Percent: 100, Detail: "5466/5466, 8.92 MiB | 6.92 MiB/s"}, true},
		{"Resolving deltas: 100% (3620/3620), done.", Progress{Phase: "Resolving deltas", Percent: 100, Detail: "3620/3620"}, true},
		{"Updating files:  12% (120/1000)", Progress{Phase: "Updating files", Percent: 12, Detail: "120/1000"}, true},
		{"remote: Total 5466 (delta 3620), reused 5000 (delta 3000), pack-reused 0", Progress{}, false},
		{"Submodule 'lib' (https://h/lib.git) registered for path 'lib'", Progress{}, false},
		{"fatal: repository 'https://h/x.git/' not found", Progress{}, false},
		{"", Progress{}, false},
	}
	for _, c := range cases {
		got, ok := ParseProgress(c.line)
		if ok != c.ok || got != c.want {
			t.Errorf("ParseProgress(%q) = %+v, %v; want %+v, %v", c.line, got, ok, c.want, c.ok)
		}
	}
}

func TestValidate(t *testing.T) {
	parent := t.TempDir()
	if err := os.Mkdir(filepath.Join(parent, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(parent, "full", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "file"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(filepath.Join(parent, "gone"), filepath.Join(parent, "dangling")); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"new", "empty", "my repo ñ"} {
		dest, err := Validate(parent, name)
		if err != nil || dest != filepath.Join(parent, name) {
			t.Errorf("Validate(%q) = %q, %v", name, dest, err)
		}
	}
	for name, want := range map[string]error{
		"":     ErrBadName,
		".":    ErrBadName,
		"..":   ErrBadName,
		"a/b":  ErrBadName,
		`a\b`:  ErrBadName,
		"full": ErrDestNotEmpty,
		"file": ErrDestNotEmpty,
		// A dangling symlink exists: it is not a free name.
		"dangling": ErrDestNotEmpty,
	} {
		if _, err := Validate(parent, name); !errors.Is(err, want) {
			t.Errorf("Validate(%q) err = %v, want %v", name, err, want)
		}
	}
	if _, err := Validate(filepath.Join(parent, "nope"), "x"); !errors.Is(err, ErrParentMissing) {
		t.Errorf("missing parent err = %v", err)
	}
	if _, err := Validate(filepath.Join(parent, "file"), "x"); !errors.Is(err, ErrParentMissing) {
		t.Errorf("file as parent err = %v", err)
	}
}

func TestValidateTypedParent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.Mkdir(filepath.Join(home, "code"), 0o755); err != nil {
		t.Fatal(err)
	}
	abs := t.TempDir()

	for _, tc := range []struct{ parent, want string }{
		{"~/code", filepath.Join(home, "code", "app")},
		{"~", filepath.Join(home, "app")},
		{"~/code/", filepath.Join(home, "code", "app")},
		{"  " + abs + "  ", filepath.Join(abs, "app")},
		{abs + "/", filepath.Join(abs, "app")},
	} {
		dest, err := Validate(tc.parent, "app")
		if err != nil || dest != tc.want {
			t.Errorf("Validate(%q) = %q, %v; want %q", tc.parent, dest, err, tc.want)
		}
	}
	for _, parent := range []string{"foo/bar", "~other/x", "~other", `~\x`, "", "   ", "./x", ".."} {
		if _, err := Validate(parent, "app"); !errors.Is(err, ErrParentNotAbsolute) {
			t.Errorf("Validate(%q) err = %v, want ErrParentNotAbsolute", parent, err)
		}
	}
	// An absolute path is still checked for existing.
	if _, err := Validate("~/nope", "app"); !errors.Is(err, ErrParentMissing) {
		t.Errorf("missing ~ subfolder err = %v", err)
	}
	t.Setenv("HOME", "")
	if _, err := Validate("~/code", "app"); !errors.Is(err, ErrParentMissing) || err.Error() != "The home folder is unknown; type a full path." {
		t.Errorf("unknown home err = %v", err)
	}
	if got := ErrParentNotAbsolute.Error(); got != "Parent folder must be a full path (or start with ~)." {
		t.Errorf("message = %q", got)
	}
}

// listTree makes dirs (and files, names ending in "!") under root.
func listTree(t *testing.T, root string, names ...string) {
	t.Helper()
	for _, n := range names {
		p := filepath.Join(root, n)
		if len(n) > 0 && n[len(n)-1] == '!' {
			if err := os.WriteFile(filepath.Join(root, n[:len(n)-1]), nil, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestListDirsPrefixSortedFilesExcluded(t *testing.T) {
	root := t.TempDir()
	listTree(t, root, "src", "docs", "Dev", "doc-files!", "downloads")
	got := ListDirs(root + "/d")
	want := []string{root + "/docs", root + "/downloads"}
	if !eq(got, want) {
		t.Errorf("ListDirs prefix = %v; want %v", got, want)
	}
	// Case-sensitive: "D" only matches "Dev".
	if got := ListDirs(root + "/D"); !eq(got, []string{root + "/Dev"}) {
		t.Errorf("ListDirs(D) = %v", got)
	}
}

func TestListDirsTrailingSlashListsAllChildren(t *testing.T) {
	root := t.TempDir()
	listTree(t, root, "b", "a", "file!")
	got := ListDirs(root + "/")
	if want := []string{root + "/a", root + "/b"}; !eq(got, want) {
		t.Errorf("ListDirs(root/) = %v; want %v", got, want)
	}
}

func TestListDirsHiddenOnlyWhenTyped(t *testing.T) {
	root := t.TempDir()
	listTree(t, root, ".config", ".cache", "code")
	if got := ListDirs(root + "/"); !eq(got, []string{root + "/code"}) {
		t.Errorf("hidden shown without a dot: %v", got)
	}
	if got := ListDirs(root + "/c"); !eq(got, []string{root + "/code"}) {
		t.Errorf("ListDirs(c) = %v", got)
	}
	if got := ListDirs(root + "/."); !eq(got, []string{root + "/.cache", root + "/.config"}) {
		t.Errorf("ListDirs(.) = %v", got)
	}
}

func TestListDirsFollowsSymlinkToDirectory(t *testing.T) {
	root := t.TempDir()
	listTree(t, root, "real", "plain!")
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "link")); err != nil {
		t.Skip("symlinks unavailable")
	}
	if err := os.Symlink(filepath.Join(root, "plain"), filepath.Join(root, "filelink")); err != nil {
		t.Skip("symlinks unavailable")
	}
	if err := os.Symlink(filepath.Join(root, "gone"), filepath.Join(root, "dangling")); err != nil {
		t.Skip("symlinks unavailable")
	}
	want := []string{root + "/link", root + "/real"}
	if got := ListDirs(root + "/"); !eq(got, want) {
		t.Errorf("ListDirs = %v; want %v", got, want)
	}
}

func TestListDirsLimit(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < MaxSuggestions+20; i++ {
		listTree(t, root, fmt.Sprintf("d%03d", i))
	}
	got := ListDirs(root + "/")
	if len(got) != MaxSuggestions {
		t.Fatalf("len = %d; want %d", len(got), MaxSuggestions)
	}
	if got[0] != root+"/d000" || got[MaxSuggestions-1] != fmt.Sprintf("%s/d%03d", root, MaxSuggestions-1) {
		t.Errorf("not the first %d sorted: %s .. %s", MaxSuggestions, got[0], got[MaxSuggestions-1])
	}
}

func TestListDirsRelativeOrMissingIsEmpty(t *testing.T) {
	root := t.TempDir()
	listTree(t, root, "a")
	t.Chdir(root)
	for _, in := range []string{"", "a", "a/", "./", "../", "~user/", "~user", root + "/nope/", root + "/nope/x", root + "/a/x"} {
		if got := ListDirs(in); got == nil || len(got) != 0 {
			t.Errorf("ListDirs(%q) = %#v; want empty non-nil", in, got)
		}
	}
	// A file is not a folder to list.
	listTree(t, root, "f!")
	if got := ListDirs(root + "/f/"); len(got) != 0 {
		t.Errorf("file as folder: %v", got)
	}
}

func TestListDirsTildeKeepsTheTypedForm(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	listTree(t, home, "projects", "pics", "notes!", "projects/alpha", "projects/beta")
	if got := ListDirs("~/p"); !eq(got, []string{"~/pics", "~/projects"}) {
		t.Errorf("ListDirs(~/p) = %v", got)
	}
	if got := ListDirs("~/projects/"); !eq(got, []string{"~/projects/alpha", "~/projects/beta"}) {
		t.Errorf("ListDirs(~/projects/) = %v", got)
	}
	if got := ListDirs("~/"); !eq(got, []string{"~/pics", "~/projects"}) {
		t.Errorf("ListDirs(~/) = %v", got)
	}
	// A bare ~ offers the home folder itself, so Tab can start descending.
	if got := ListDirs("~"); !eq(got, []string{"~/"}) {
		t.Errorf("ListDirs(~) = %v", got)
	}
}

func TestListDirsTrimsWhitespace(t *testing.T) {
	root := t.TempDir()
	listTree(t, root, "docs")
	if got := ListDirs("  " + root + "/d \n"); !eq(got, []string{root + "/docs"}) {
		t.Errorf("ListDirs = %v", got)
	}
}

func TestListDirsHomeUnknownIsEmpty(t *testing.T) {
	t.Setenv("HOME", "")
	if got := ListDirs("~/"); got == nil || len(got) != 0 {
		t.Errorf("ListDirs(~/) without a home = %#v", got)
	}
	if got := ListDirs("~"); got == nil || len(got) != 0 {
		t.Errorf("ListDirs(~) without a home = %#v", got)
	}
}

func TestListDirsSortsByBytes(t *testing.T) {
	root := t.TempDir()
	listTree(t, root, "docs", "Dev", "Zed", "alpha")
	want := []string{root + "/Dev", root + "/Zed", root + "/alpha", root + "/docs"}
	if got := ListDirs(root + "/"); !eq(got, want) {
		t.Errorf("ListDirs = %v; want uppercase names first (byte order) %v", got, want)
	}
}

func TestListDirsUnreadableIsEmpty(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every folder")
	}
	root := t.TempDir()
	listTree(t, root, "locked/inner")
	locked := filepath.Join(root, "locked")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Skip("chmod unavailable")
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	if got := ListDirs(locked + "/"); got == nil || len(got) != 0 {
		t.Errorf("ListDirs(unreadable) = %#v; want empty non-nil", got)
	}
}
