package app

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// EventNotifyOpen asks the frontend to show what a clicked OS notification
// was about: select RepoID and, for Target "chat", open its chat.
const EventNotifyOpen = "notify:open"

var (
	ErrNotificationsDenied     = errors.New("notifications are not allowed for CommitTree in the system settings")
	errNotificationsNotStarted = errors.New("not started")
)

// Notification is one OS notification the frontend asks for; the frontend
// decides whether one is due (see lib/notifyRules.ts).
type Notification struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	RepoID string `json:"repoID"`
	Target string `json:"target"`
}

// NotifyOpenEvent is EventNotifyOpen's payload.
type NotifyOpenEvent struct {
	RepoID string `json:"repoID"`
	Target string `json:"target"`
}

// notifier is the part of the Wails runtime notifications use; tests pass
// a fake, since the real one needs a running window.
type notifier interface {
	Init() error
	Authorized() (bool, error)
	RequestAuthorization() (bool, error)
	Send(runtime.NotificationOptions) error
	OnResponse(func(runtime.NotificationResult))
	ShowWindow()
	Cleanup()
}

type wailsNotifier struct{ ctx context.Context }

func (w wailsNotifier) Init() error { return runtime.InitializeNotifications(w.ctx) }
func (w wailsNotifier) Authorized() (bool, error) {
	return runtime.CheckNotificationAuthorization(w.ctx)
}
func (w wailsNotifier) RequestAuthorization() (bool, error) {
	return runtime.RequestNotificationAuthorization(w.ctx)
}
func (w wailsNotifier) Send(o runtime.NotificationOptions) error {
	return runtime.SendNotification(w.ctx, o)
}
func (w wailsNotifier) OnResponse(cb func(runtime.NotificationResult)) {
	runtime.OnNotificationResponse(w.ctx, cb)
}
func (w wailsNotifier) ShowWindow() {
	runtime.WindowUnminimise(w.ctx)
	runtime.WindowShow(w.ctx)
}
func (w wailsNotifier) Cleanup() { runtime.CleanupNotifications(w.ctx) }

// notifyState is nil-notifier until Startup; initErr is why notifications
// are unavailable (e.g. no bundle identifier under `wails dev` on macOS),
// and asked records that permission was already requested this run.
type notifyState struct {
	mu      sync.Mutex
	n       notifier
	initErr error
	asked   bool
}

func (a *App) startNotifications(n notifier) {
	a.notes.mu.Lock()
	defer a.notes.mu.Unlock()
	a.notes.n = n
	if err := n.Init(); err != nil {
		a.notes.initErr = err
		return
	}
	n.OnResponse(func(r runtime.NotificationResult) {
		if r.Error != nil || strings.Contains(strings.ToLower(r.Response.ActionIdentifier), "dismiss") {
			return
		}
		repoID, _ := r.Response.UserInfo["repoID"].(string)
		target, _ := r.Response.UserInfo["target"].(string)
		n.ShowWindow()
		a.emit(EventNotifyOpen, NotifyOpenEvent{RepoID: repoID, Target: target})
	})
}

func (a *App) stopNotifications() {
	a.notes.mu.Lock()
	defer a.notes.mu.Unlock()
	if a.notes.n != nil && a.notes.initErr == nil {
		a.notes.n.Cleanup()
	}
}

// Notify sends an OS notification, asking for permission the first time it
// is missing. The error says why nothing was shown; the frontend then
// falls back to an in-app toast.
func (a *App) Notify(n Notification) error {
	a.notes.mu.Lock()
	defer a.notes.mu.Unlock()
	if a.notes.n == nil {
		return errNotificationsNotStarted
	}
	if a.notes.initErr != nil {
		return a.notes.initErr
	}
	ok, err := a.notes.n.Authorized()
	if err != nil {
		return err
	}
	if !ok && !a.notes.asked {
		a.notes.asked = true
		if ok, err = a.notes.n.RequestAuthorization(); err != nil {
			return err
		}
	}
	if !ok {
		return ErrNotificationsDenied
	}
	return a.notes.n.Send(runtime.NotificationOptions{
		ID:    n.ID,
		Title: n.Title,
		Body:  n.Body,
		Data:  map[string]interface{}{"repoID": n.RepoID, "target": n.Target},
	})
}

// NotificationStatus is "allowed", "not allowed" or "unavailable: <reason>",
// for the line under Settings → Notifications.
func (a *App) NotificationStatus() string {
	a.notes.mu.Lock()
	defer a.notes.mu.Unlock()
	switch {
	case a.notes.n == nil:
		return "unavailable: " + errNotificationsNotStarted.Error()
	case a.notes.initErr != nil:
		return "unavailable: " + a.notes.initErr.Error()
	}
	ok, err := a.notes.n.Authorized()
	if err != nil {
		return "unavailable: " + err.Error()
	}
	if ok {
		return "allowed"
	}
	return "not allowed"
}
