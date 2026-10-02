package app

import (
	"errors"
	"testing"
	"time"
)

func (l *writeLock) state() (holder, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.holder, l.waiting
}

func TestWriteLockUserThenUserIsBusy(t *testing.T) {
	l := newWriteLock()
	if err := l.lockUser(); err != nil {
		t.Fatal(err)
	}
	if err := l.lockUser(); !errors.Is(err, ErrBusy) {
		t.Errorf("second lockUser = %v, want ErrBusy", err)
	}
	if l.tryLockAuto(func() {}) {
		t.Error("tryLockAuto took a lock a user write holds")
	}
	l.unlock()
	if h, _ := l.state(); h != holderFree {
		t.Errorf("holder = %v after unlock, want free", h)
	}
}

func TestWriteLockUserCancelsAutoAndGetsItNext(t *testing.T) {
	l := newWriteLock()
	cancelled := make(chan struct{})
	if !l.tryLockAuto(func() { close(cancelled) }) {
		t.Fatal("tryLockAuto on a free lock = false")
	}
	got := make(chan error, 1)
	go func() { got <- l.lockUser() }()
	<-cancelled
	// While the first user write waits, a second one is refused …
	if err := l.lockUser(); !errors.Is(err, ErrBusy) {
		t.Errorf("second lockUser while one waits = %v, want ErrBusy", err)
	}
	l.unlock() // the background fetch stops
	// … and so is one arriving right after the fetch let go: it is handed over.
	if err := l.lockUser(); !errors.Is(err, ErrBusy) {
		t.Errorf("lockUser right after the handoff = %v, want ErrBusy", err)
	}
	select {
	case err := <-got:
		if err != nil {
			t.Fatalf("waiting lockUser = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the waiting user write never got the lock")
	}
	if h, w := l.state(); h != holderUser || w {
		t.Errorf("state = %v waiting=%v, want user, not waiting", h, w)
	}
	l.unlock()
	if !l.tryLockAuto(func() {}) {
		t.Error("the lock is not free after the user write")
	}
}

func TestWriteLockAutoAfterAutoIsRefused(t *testing.T) {
	l := newWriteLock()
	if !l.tryLockAuto(func() {}) || l.tryLockAuto(func() {}) {
		t.Error("want the first tryLockAuto to succeed and the second to fail")
	}
}
