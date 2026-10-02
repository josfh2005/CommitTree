package app

import "sync"

// holder is who holds a repository's write lock.
type holder int

const (
	holderFree holder = iota
	holderUser
	holderAuto
	// holderHandoff: a background fetch let go while a user write was
	// waiting for it; the lock is that write's, no one else's.
	holderHandoff
)

// writeLock is one repository's write lock (docs/spec/07, "One write lock
// per repository"). A user write never queues behind another user write
// (ErrBusy); one that finds a background fetch cancels it and gets the
// lock next. Every change of holder happens under mu, so there is no
// moment where the lock is held but its background fetch can't be found.
type writeLock struct {
	mu      sync.Mutex
	cond    *sync.Cond
	holder  holder
	cancel  func()
	waiting bool
}

func newWriteLock() *writeLock {
	l := &writeLock{}
	l.cond = sync.NewCond(&l.mu)
	return l
}

// lockUser takes the lock for a user write: at once when free; after
// cancelling and waiting for a background fetch that holds it, when no
// other user write is already waiting for it; ErrBusy otherwise.
func (l *writeLock) lockUser() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	switch {
	case l.holder == holderFree:
		l.holder = holderUser
		return nil
	case l.holder == holderAuto && !l.waiting:
		l.waiting = true
		l.cancel()
		for l.holder != holderHandoff {
			l.cond.Wait()
		}
		l.waiting = false
		l.holder = holderUser
		return nil
	}
	return ErrBusy
}

// tryLockAuto takes a free lock for a background fetch; cancel is what a
// user write calls to stop it. False when anything holds the lock.
func (l *writeLock) tryLockAuto(cancel func()) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.holder != holderFree {
		return false
	}
	l.holder, l.cancel = holderAuto, cancel
	return true
}

// unlock releases the lock, straight to the user write waiting for this
// background fetch if there is one.
func (l *writeLock) unlock() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.holder == holderAuto && l.waiting {
		l.holder = holderHandoff
	} else {
		l.holder = holderFree
	}
	l.cancel = nil
	l.cond.Broadcast()
}
