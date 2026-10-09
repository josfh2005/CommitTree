package app

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"git-ui/internal/clone"
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

// CloneDone is the clone:done event: the added repository, or why not.
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
	dest, err := clone.Validate(parent, strings.TrimSpace(name))
	if err != nil {
		return err
	}
	if url == "" || strings.HasPrefix(url, "-") {
		return clone.ErrBadURL
	}
	a.cloneMu.Lock()
	defer a.cloneMu.Unlock()
	if a.clone.Running {
		return ErrCloneRunning
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.clone = CloneState{Running: true, URL: url, Dest: dest}
	a.cloneCancel = cancel
	go a.runClone(ctx, cancel, url, dest)
	return nil
}

func (a *App) runClone(ctx context.Context, cancel context.CancelFunc, url, dest string) {
	defer cancel()
	var lastPhase string
	var lastSent time.Time
	err := clone.Run(ctx, url, dest, clone.Stall, func(p clone.Progress) {
		a.cloneMu.Lock()
		a.clone.Progress = &p
		a.cloneMu.Unlock()
		if p.Phase == lastPhase && time.Since(lastSent) < progressEvery {
			return
		}
		lastPhase, lastSent = p.Phase, time.Now()
		a.emit("clone:progress", p)
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
	default:
		done.Error = clone.Explain(err, url)
	}
	a.cloneMu.Lock()
	a.clone = CloneState{LastError: done.Error}
	a.cloneCancel = nil
	a.cloneMu.Unlock()
	a.emit("clone:done", done)
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
