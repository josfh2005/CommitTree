package clone

import (
	"errors"
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
