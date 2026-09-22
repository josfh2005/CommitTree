package app

import (
	"errors"
	"os"
	"testing"

	"git-ui/internal/repos"
	"git-ui/internal/terminal"
)

func TestTerminalOpenUnknownRepo(t *testing.T) {
	a, _, _ := newPlainApp(t)
	if _, err := a.TerminalOpen("nope", 80, 24); !errors.Is(err, repos.ErrUnknownRepo) {
		t.Fatalf("got %v", err)
	}
}

func TestTerminalOpenMissingRepo(t *testing.T) {
	a, r, id := newPlainApp(t)
	if err := os.RemoveAll(r.Dir); err != nil {
		t.Fatal(err)
	}
	if _, err := a.TerminalOpen(id, 80, 24); !errors.Is(err, ErrRepoMissing) {
		t.Fatalf("got %v", err)
	}
}

func TestRemoveRepoClosesTerminals(t *testing.T) {
	a, _, id := newPlainApp(t)
	t.Cleanup(func() { a.Shutdown(nil) })
	tab, err := a.TerminalOpen(id, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.RemoveRepo(id); err != nil {
		t.Fatal(err)
	}
	if err := a.TerminalWrite(tab, "x"); !errors.Is(err, terminal.ErrUnknownTab) {
		t.Fatalf("tab still open: %v", err)
	}
}

func TestTerminalShellIsBasename(t *testing.T) {
	a, _, _ := newPlainApp(t)
	if s := a.TerminalShell(); s == "" || s[0] == '/' {
		t.Fatalf("got %q", s)
	}
}
