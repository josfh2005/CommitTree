package reposettings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, text string) {
	t.Helper()
	p := filepath.Join(dir, Dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func file(ri RepoInstructions, name string) RepoFile {
	for _, f := range ri.Files {
		if f.Name == name {
			return f
		}
	}
	return RepoFile{Name: "<missing>"}
}

func TestReadRepoNoDirectory(t *testing.T) {
	ri := ReadRepo(t.TempDir())
	if len(ri.Files) != 0 || ri.Hash != "" || ri.Error != "" {
		t.Fatalf("got %+v", ri)
	}
}

func TestReadRepoUsedAndIgnoredFiles(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "instructions.md", "We use Go.")
	write(t, dir, "commit-message.md", "Conventional Commits.")
	write(t, dir, "deploy.md", "x")
	write(t, dir, "notes.txt", "x")
	write(t, dir, "big.md", strings.Repeat("a", MaxFile+1))
	write(t, dir, "bad.md", "\xff\xfe")
	ri := ReadRepo(dir)
	if f := file(ri, "instructions.md"); f.Ignored != "" || f.Text != "We use Go." {
		t.Fatalf("instructions.md: %+v", f)
	}
	if f := file(ri, "deploy.md"); f.Ignored != "not used" {
		t.Fatalf("deploy.md: %+v", f)
	}
	if f := file(ri, "big.md"); !strings.Contains(f.Ignored, "16 KB") {
		t.Fatalf("big.md: %+v", f)
	}
	if f := file(ri, "bad.md"); f.Ignored != "not UTF-8 text" {
		t.Fatalf("bad.md: %+v", f)
	}
	if f := file(ri, "notes.txt"); f.Name != "<missing>" {
		t.Fatalf("non-.md files are not listed: %+v", f)
	}
	if !strings.HasPrefix(ri.Hash, "sha256:") {
		t.Fatalf("hash %q", ri.Hash)
	}
	if got := ri.Text("commit-message"); len(got) != 2 || got[0] != "We use Go." || got[1] != "Conventional Commits." {
		t.Fatalf("Text: %q", got)
	}
	if got := ri.Text("chat"); len(got) != 1 {
		t.Fatalf("Text(chat): %q", got)
	}
}

func TestReadRepoTotalLimitInNameOrder(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "chat.md", strings.Repeat("a", MaxFile))
	write(t, dir, "commit-message.md", strings.Repeat("b", MaxFile))
	write(t, dir, "explain-commit.md", "c")
	ri := ReadRepo(dir)
	if f := file(ri, "explain-commit.md"); !strings.Contains(f.Ignored, "32 KB") {
		t.Fatalf("explain-commit.md should be past the total: %+v", f)
	}
}

func TestReadRepoSkipsSymlinks(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.md")
	_ = os.WriteFile(outside, []byte("secret"), 0o644)
	write(t, dir, "instructions.md", "ok")
	if err := os.Symlink(outside, filepath.Join(dir, Dir, "chat.md")); err != nil {
		t.Fatal(err)
	}
	ri := ReadRepo(dir)
	if f := file(ri, "chat.md"); f.Ignored != "a symbolic link" || f.Text != "" {
		t.Fatalf("chat.md: %+v", f)
	}
}

func TestReadRepoDirectoryIsSymlinkOrFile(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, Dir), []byte("x"), 0o644)
	if ri := ReadRepo(dir); len(ri.Files) != 0 || ri.Hash != "" || ri.Error == "" {
		t.Fatalf("a file named .committree: %+v", ri)
	}
	dir2 := t.TempDir()
	target := t.TempDir()
	_ = os.WriteFile(filepath.Join(target, "instructions.md"), []byte("outside"), 0o644)
	_ = os.Symlink(target, filepath.Join(dir2, Dir))
	if ri := ReadRepo(dir2); len(ri.Files) != 0 || ri.Hash != "" || ri.Error == "" {
		t.Fatalf("a symlinked .committree: %+v", ri)
	}
}

func TestHashChangesWithContentAndName(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "instructions.md", "a")
	h1 := ReadRepo(dir).Hash
	if h1 != ReadRepo(dir).Hash {
		t.Fatal("hash not stable")
	}
	write(t, dir, "instructions.md", "b")
	h2 := ReadRepo(dir).Hash
	write(t, dir, "chat.md", "")
	h3 := ReadRepo(dir).Hash
	if h1 == h2 || h2 == h3 {
		t.Fatalf("hashes %s %s %s", h1, h2, h3)
	}
}

func TestStateOf(t *testing.T) {
	cases := []struct{ approval, hash, want string }{
		{"", "", StateNone},
		{"sha256:a", "", StateNone},
		{"", "sha256:a", StatePending},
		{"sha256:a", "sha256:a", StateApproved},
		{"ignored:sha256:a", "sha256:a", StateIgnored},
		{"sha256:a", "sha256:b", StateChanged},
		{"ignored:sha256:a", "sha256:b", StateChanged},
	}
	for _, c := range cases {
		if got := StateOf(c.approval, c.hash); got != c.want {
			t.Errorf("StateOf(%q,%q) = %s, want %s", c.approval, c.hash, got, c.want)
		}
	}
}
