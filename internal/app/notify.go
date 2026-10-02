package app

import (
	"context"
	"errors"
	goruntime "runtime"
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

// On Linux the system reports a click on a notification and a close by the
// user the same way (Wails' DEFAULT_ACTION), so a notification there carries
// an Open button and only that button opens it.
const (
	openCategory = "open"
	openAction   = "open"
)

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
	SendWithActions(runtime.NotificationOptions) error
	RegisterCategory(runtime.NotificationCategory) error
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
func (w wailsNotifier) SendWithActions(o runtime.NotificationOptions) error {
	return runtime.SendNotificationWithActions(w.ctx, o)
}
func (w wailsNotifier) RegisterCategory(c runtime.NotificationCategory) error {
	return runtime.RegisterNotificationCategory(w.ctx, c)
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
	goos    string // runtime.GOOS unless a test sets it
}

func (a *App) startNotifications(n notifier) {
	a.notes.mu.Lock()
	defer a.notes.mu.Unlock()
	a.notes.n = n
	if err := n.Init(); err != nil {
		a.notes.initErr = err
		return
	}
	if a.notes.goos == "" {
		a.notes.goos = goruntime.GOOS
	}
	linux := a.notes.goos == "linux"
	if linux {
		// Without the category the notification is plain and opens nothing,
		// which still beats opening on close.
		_ = n.RegisterCategory(runtime.NotificationCategory{ID: openCategory, Actions: []runtime.NotificationAction{{ID: openAction, Title: "Open"}}})
	}
	n.OnResponse(func(r runtime.NotificationResult) {
		if r.Error != nil {
			return
		}
		id := r.Response.ActionIdentifier
		if linux && id != openAction || !linux && strings.Contains(strings.ToLower(id), "dismiss") {
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
// falls back to an in-app toast. The permission dialog waits for the user
// without the lock held: a notification due meanwhile sees permission
// missing and already asked for, so it becomes a toast.
func (a *App) Notify(n Notification) error {
	a.notes.mu.Lock()
	nt, initErr, linux := a.notes.n, a.notes.initErr, a.notes.goos == "linux"
	if nt == nil {
		a.notes.mu.Unlock()
		return errNotificationsNotStarted
	}
	if initErr != nil {
		a.notes.mu.Unlock()
		return initErr
	}
	ok, err := nt.Authorized()
	ask := err == nil && !ok && !a.notes.asked
	if ask {
		a.notes.asked = true
	}
	a.notes.mu.Unlock()
	if err != nil {
		return err
	}
	if ask {
		if ok, err = nt.RequestAuthorization(); err != nil {
			return err
		}
	}
	if !ok {
		return ErrNotificationsDenied
	}
	opts := runtime.NotificationOptions{
		ID:    n.ID,
		Title: n.Title,
		Body:  n.Body,
		Data:  map[string]interface{}{"repoID": n.RepoID, "target": n.Target},
	}
	a.notes.mu.Lock()
	defer a.notes.mu.Unlock()
	if linux {
		opts.CategoryID = openCategory
		return nt.SendWithActions(opts)
	}
	return nt.Send(opts)
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
