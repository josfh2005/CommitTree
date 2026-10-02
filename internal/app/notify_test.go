package app

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type fakeNotifier struct {
	initErr    error
	authorized bool
	grant      bool
	requests   int
	sent       []runtime.NotificationOptions
	respond    func(runtime.NotificationResult)
	shown      int
	cleaned    int
	// asking is closed when RequestAuthorization starts, which then waits
	// for block: the permission dialog open on screen.
	asking, block chan struct{}
}

func (f *fakeNotifier) Init() error               { return f.initErr }
func (f *fakeNotifier) Authorized() (bool, error) { return f.authorized, nil }
func (f *fakeNotifier) RequestAuthorization() (bool, error) {
	f.requests++
	if f.asking != nil {
		close(f.asking)
		<-f.block
	}
	f.authorized = f.grant
	return f.grant, nil
}
func (f *fakeNotifier) Send(o runtime.NotificationOptions) error {
	f.sent = append(f.sent, o)
	return nil
}
func (f *fakeNotifier) OnResponse(cb func(runtime.NotificationResult)) { f.respond = cb }
func (f *fakeNotifier) ShowWindow()                                    { f.shown++ }
func (f *fakeNotifier) Cleanup()                                       { f.cleaned++ }

var note = Notification{ID: "r1:done", Title: "repo", Body: "Push finished · 14 s", RepoID: "r1", Target: "repo"}

func TestNotifyBeforeStartupFails(t *testing.T) {
	a, _ := newTestApp(t)
	if err := a.Notify(note); err == nil {
		t.Fatal("want an error before startup")
	}
	if got := a.NotificationStatus(); !strings.HasPrefix(got, "unavailable: ") {
		t.Fatalf("status = %q", got)
	}
}

func TestNotifyAsksOnceThenSends(t *testing.T) {
	a, _ := newTestApp(t)
	f := &fakeNotifier{grant: true}
	a.startNotifications(f)
	if err := a.Notify(note); err != nil {
		t.Fatal(err)
	}
	if err := a.Notify(note); err != nil {
		t.Fatal(err)
	}
	if f.requests != 1 || len(f.sent) != 2 {
		t.Fatalf("requests=%d sent=%d", f.requests, len(f.sent))
	}
	s := f.sent[0]
	if s.ID != "r1:done" || s.Title != "repo" || s.Body != note.Body || s.Data["repoID"] != "r1" || s.Data["target"] != "repo" {
		t.Fatalf("sent %+v", s)
	}
	if got := a.NotificationStatus(); got != "allowed" {
		t.Fatalf("status = %q", got)
	}
}

func TestNotifyDeniedAsksOnlyOnce(t *testing.T) {
	a, _ := newTestApp(t)
	f := &fakeNotifier{grant: false}
	a.startNotifications(f)
	for i := 0; i < 2; i++ {
		if err := a.Notify(note); !errors.Is(err, ErrNotificationsDenied) {
			t.Fatalf("call %d: err = %v", i, err)
		}
	}
	if f.requests != 1 || len(f.sent) != 0 {
		t.Fatalf("requests=%d sent=%d", f.requests, len(f.sent))
	}
	if got := a.NotificationStatus(); got != "not allowed" {
		t.Fatalf("status = %q", got)
	}
}

func TestNotifyInitErrorIsTheReason(t *testing.T) {
	a, _ := newTestApp(t)
	f := &fakeNotifier{initErr: errors.New("notifications require a valid bundle identifier")}
	a.startNotifications(f)
	if err := a.Notify(note); err == nil || !strings.Contains(err.Error(), "bundle") {
		t.Fatalf("err = %v", err)
	}
	if got := a.NotificationStatus(); got != "unavailable: notifications require a valid bundle identifier" {
		t.Fatalf("status = %q", got)
	}
	a.stopNotifications()
	if f.cleaned != 0 {
		t.Fatal("cleanup after a failed init")
	}
}

func TestNotificationClickOpensTheRepo(t *testing.T) {
	a, _ := newTestApp(t)
	var mu sync.Mutex
	var got []NotifyOpenEvent
	WithAI(a, AIDeps{Emit: func(name string, data any) {
		if name == EventNotifyOpen {
			mu.Lock()
			got = append(got, data.(NotifyOpenEvent))
			mu.Unlock()
		}
	}})
	f := &fakeNotifier{authorized: true}
	a.startNotifications(f)
	f.respond(runtime.NotificationResult{Response: runtime.NotificationResponse{
		ActionIdentifier: "com.apple.UNNotificationDismissActionIdentifier",
		UserInfo:         map[string]interface{}{"repoID": "r1", "target": "chat"},
	}})
	f.respond(runtime.NotificationResult{Response: runtime.NotificationResponse{
		ActionIdentifier: "DEFAULT_ACTION",
		UserInfo:         map[string]interface{}{"repoID": "r1", "target": "chat"},
	}})
	mu.Lock()
	defer mu.Unlock()
	if f.shown != 1 || len(got) != 1 || got[0] != (NotifyOpenEvent{RepoID: "r1", Target: "chat"}) {
		t.Fatalf("shown=%d events=%+v", f.shown, got)
	}
	a.stopNotifications()
	if f.cleaned != 1 {
		t.Fatalf("cleaned=%d", f.cleaned)
	}
}

func TestPermissionDialogDoesNotBlockOtherNotifications(t *testing.T) {
	a, _ := newTestApp(t)
	f := &fakeNotifier{grant: true, asking: make(chan struct{}), block: make(chan struct{})}
	a.startNotifications(f)
	first := make(chan error, 1)
	go func() { first <- a.Notify(note) }()
	<-f.asking

	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := a.Notify(note); !errors.Is(err, ErrNotificationsDenied) {
			t.Errorf("second Notify during the dialog: err = %v, want ErrNotificationsDenied", err)
		}
		if got := a.NotificationStatus(); got != "not allowed" {
			t.Errorf("status during the dialog = %q", got)
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Notify or NotificationStatus waited for the permission dialog")
	}

	close(f.block)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if len(f.sent) != 1 || f.requests != 1 {
		t.Fatalf("sent=%d requests=%d", len(f.sent), f.requests)
	}
}
