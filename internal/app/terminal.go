package app

import (
	"context"
	"errors"
	"os"
)

// The embedded terminal. It is deliberately not an app operation: it does
// not go through a.write, so a command typed there can race an app
// operation on the same repository — the same exposure an external terminal
// has. Git's own index and ref locks turn such a race into a git error, not
// a corrupted repository. See docs/spec/08-terminal.md.

var ErrRepoMissing = errors.New("repository folder is missing")

type TerminalData struct {
	Tab  string `json:"tab"`
	Data string `json:"data"`
}

type TerminalSettled struct {
	Tab  string `json:"tab"`
	Repo string `json:"repo"`
}

type TerminalExit struct {
	Tab  string `json:"tab"`
	Code int    `json:"code"`
}

// TerminalOpen starts a shell at the repository's root and returns its tab.
func (a *App) TerminalOpen(repoID string, cols, rows int) (string, error) {
	dir, err := a.dir(repoID)
	if err != nil {
		return "", err
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return "", ErrRepoMissing
	}
	return a.term.Open(repoID, dir, cols, rows)
}

func (a *App) TerminalWrite(tab, data string) error { return a.term.Write(tab, data) }

func (a *App) TerminalResize(tab string, cols, rows int) error {
	return a.term.Resize(tab, cols, rows)
}

func (a *App) TerminalClose(tab string) error { return a.term.Close(tab) }

// TerminalShell is the basename of the shell new tabs run, for tab labels.
func (a *App) TerminalShell() string { return a.term.Shell() }

// Shutdown is Wails' OnShutdown hook: no shell outlives the app.
func (a *App) Shutdown(ctx context.Context) { a.term.CloseAll() }
