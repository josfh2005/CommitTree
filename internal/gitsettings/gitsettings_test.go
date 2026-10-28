package gitsettings_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git-ui/internal/gitsettings"
)

func TestDefaultsAreAutoStrategy(t *testing.T) {
	if got := gitsettings.Defaults().PullStrategy; got != gitsettings.PullAuto {
		t.Errorf("default = %q, want auto", got)
	}
}

func TestLoadOfAMissingFileReturnsDefaults(t *testing.T) {
	got, err := gitsettings.Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got != gitsettings.Defaults() {
		t.Errorf("got = %+v, want defaults", got)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "git.json")
	want := gitsettings.Settings{PullStrategy: gitsettings.PullRebase}
	if err := gitsettings.Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := gitsettings.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("got = %+v, want %+v", got, want)
	}
}

func TestSaveRejectsAnUnknownStrategy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "git.json")
	err := gitsettings.Save(path, gitsettings.Settings{PullStrategy: "sometimes"})
	if !errors.Is(err, gitsettings.ErrInvalid) {
		t.Errorf("err = %v, want ErrInvalid", err)
	}
}

// A file written before PullStrategy existed, or with it blanked out, must
// still load with the default filled in.
func TestLoadFillsInAMissingStrategy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "git.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := gitsettings.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.PullStrategy != gitsettings.PullAuto {
		t.Errorf("strategy = %q, want auto filled in", got.PullStrategy)
	}
}

// The push-scope setting is gone (the Push dialog replaced it): a file that
// still has it loads fine, and the next save drops it.
func TestLoadIgnoresAnOldPushScope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "git.json")
	if err := os.WriteFile(path, []byte(`{"pullStrategy":"merge","pushScope":"all"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := gitsettings.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.PullStrategy != gitsettings.PullMerge {
		t.Errorf("strategy = %q, want merge", got.PullStrategy)
	}
	if err := gitsettings.Save(path, got); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "pushScope") {
		t.Errorf("saved file still has pushScope: %s", data)
	}
}
