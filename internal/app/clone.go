package app

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"git-ui/internal/clone"
	"git-ui/internal/cmdlog"
	"git-ui/internal/repos"
)

// ErrCloneRunning refuses a second clone while one runs.
var ErrCloneRunning = errors.New("A clone is already running.")

// CloneState is what the Clone dialog shows when it opens: the running
// clone, or the last one's error.
type CloneState struct {
	Running   bool            `json:"running"`
	URL       string          `json:"url"`
	Dest      string          `json:"dest"`
	Progress  *clone.Progress `json:"progress"`
	LastError string          `json:"lastError"`
}

// CloneDone is the clone:done event: the added repository, or why not. Both
// Repo and Error are set when the repository was cloned but not completely
// (a submodule or the checkout failed).
type CloneDone struct {
	Repo      *repos.Repo `json:"repo"`
	Error     string      `json:"error"`
	Cancelled bool        `json:"cancelled"`
}

// progressEvery throttles clone:progress within one phase.
const progressEvery = 100 * time.Millisecond

// CloneRepo checks the destination and URL, then clones url into
// parent/name in the background, reporting through clone:progress and
// clone:done. One clone runs at a time.
func (a *App) CloneRepo(url, parent, name string) error {
	url = strings.TrimSpace(url)
	a.cloneMu.Lock()
	defer a.cloneMu.Unlock()
	if a.clone.Running {
		return ErrCloneRunning
	}
	dest, err := clone.Validate(parent, strings.TrimSpace(name))
	if err != nil {
		return err
	}
	if url == "" || strings.HasPrefix(url, "-") {
		return clone.ErrBadURL
	}
	ctx, cancel := context.WithCancel(a.ctx)
	// The status is shown on screen: it carries the URL without credentials.
	shown, _ := cmdlog.RedactArgs([]string{url})
	a.clone = CloneState{Running: true, URL: shown[0], Dest: dest}
	a.cloneCancel = cancel
	go a.runClone(ctx, cancel, url, dest)
	return nil
}

// throttled reports whether p is dropped from clone:progress: another
// update of the same phase too soon after the last one sent. A phase's
// last update (100%) is never dropped.
func throttled(p clone.Progress, lastPhase string, lastSent time.Time) bool {
	return p.Percent != 100 && p.Phase == lastPhase && time.Since(lastSent) < progressEvery
}

func (a *App) runClone(ctx context.Context, cancel context.CancelFunc, url, dest string) {
	defer cancel()
	var lastPhase string
	var lastSent time.Time
	err := clone.Run(ctx, url, dest, clone.Stall, func(p clone.Progress) {
		a.cloneMu.Lock()
		a.clone.Progress = &p
		a.cloneMu.Unlock()
		if throttled(p, lastPhase, lastSent) {
			return
		}
		lastPhase, lastSent = p.Phase, time.Now()
		a.emitUnlessShutdown("clone:progress", p)
	})
	var done CloneDone
	switch {
	case err == nil:
		repo, addErr := a.store.Add(context.Background(), dest)
		if addErr != nil {
			done.Error = addErr.Error()
		} else {
			done.Repo = &repo
		}
	case errors.Is(err, clone.ErrCancelled):
		done.Cancelled = true
	case errors.Is(err, clone.ErrPartial):
		// git kept what it fetched: add it, and say what is missing.
		done.Error = clone.Explain(err, url)
		repo, addErr := a.store.Add(context.Background(), dest)
		if addErr != nil {
			done.Error = addErr.Error()
		} else {
			done.Repo = &repo
		}
	default:
		done.Error = clone.Explain(err, url)
	}
	a.cloneMu.Lock()
	a.clone = CloneState{LastError: done.Error}
	a.cloneCancel = nil
	a.cloneMu.Unlock()
	a.emitUnlessShutdown("clone:done", done)
}

// emitUnlessShutdown is emit, except once the app is quitting: Wails'
// EventsEmit on its cancelled context ends the process with log.Fatalf.
func (a *App) emitUnlessShutdown(name string, data any) {
	if a.ctx.Err() == nil {
		a.emit(name, data)
	}
}

// CancelClone stops the running clone, if any; clone:done follows.
func (a *App) CancelClone() {
	a.cloneMu.Lock()
	defer a.cloneMu.Unlock()
	if a.cloneCancel != nil {
		a.cloneCancel()
	}
}

// CloneStatus is the running clone or the last one's error.
func (a *App) CloneStatus() CloneState {
	a.cloneMu.Lock()
	defer a.cloneMu.Unlock()
	s := a.clone
	if s.Progress != nil {
		p := *s.Progress
		s.Progress = &p
	}
	return s
}

// PickCloneParent asks for the folder to clone into, starting at start;
// "" when the user cancels.
func (a *App) PickCloneParent(start string) (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Clone into folder", DefaultDirectory: start})
}

// DefaultCloneParent is the parent folder offered before any clone: home.
func (a *App) DefaultCloneParent() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

// ListDirs suggests folders for the parent being typed in the Clone
// dialog; never nil, and never an error: a path that cannot be listed
// simply has no suggestions.
func (a *App) ListDirs(partial string) ([]string, error) {
	dirs := clone.ListDirs(partial)
	if dirs == nil {
		dirs = []string{}
	}
	return dirs, nil
}
