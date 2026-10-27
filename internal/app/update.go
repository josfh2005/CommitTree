package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"git-ui/internal/update"
)

var (
	// ErrUpdateBusy means an update is already being prepared or installed.
	ErrUpdateBusy = errors.New("an update is already in progress")
	// ErrNothingPrepared means ApplyUpdate was called before PrepareUpdate
	// finished.
	ErrNothingPrepared = errors.New("no update has been downloaded")
	// ErrUpdatesOff means WithUpdates was never called (the build has no
	// version or no resolvable executable).
	ErrUpdatesOff = errors.New("updates are not available in this build")
	// ErrNoNewerVersion means the latest release is not newer than the
	// running version.
	ErrNoNewerVersion = errors.New("CommitTree is already up to date")
)

// checkTimeout bounds CheckForUpdate. update:progress is throttled by
// progressEvery (clone.go): the download calls back every 64 KiB, far more
// often than the frontend needs.
const checkTimeout = 15 * time.Second

// updateSource is where releases come from; *update.Checker in production.
type updateSource interface {
	Latest(ctx context.Context) (update.Release, error)
	Download(ctx context.Context, r update.Release, dir string, progress func(done, total int64)) (string, error)
}

// UpdateOptions configure WithUpdates.
type UpdateOptions struct {
	// Version is the running version (wails.json info.productVersion).
	Version string
	Source  updateSource
	// Exe is the running executable with symlinks resolved.
	Exe  string
	GOOS string
	// Dev is true in a Wails dev build, which never installs in place.
	Dev bool
}

// UpdateInfo is what CheckForUpdate tells the frontend.
type UpdateInfo struct {
	Current string `json:"current"`
	Latest  string `json:"latest"`
	Newer   bool   `json:"newer"`
	PageURL string `json:"pageURL"`
	// CanInstall is false when the update has to be downloaded by hand;
	// Reason says why.
	CanInstall bool   `json:"canInstall"`
	Reason     string `json:"reason"`
}

// UpdateProgress is update:progress. Phase is "download" (Done and Total are
// bytes; Total is -1 when unknown) or "install" (unpacking; Done and Total
// are 0).
type UpdateProgress struct {
	Phase string `json:"phase"`
	Done  int64  `json:"done"`
	Total int64  `json:"total"`
}

// stagedUpdate is a downloaded, unpacked and verified update waiting for
// ApplyUpdate. dir is its private temporary folder.
type stagedUpdate struct {
	dir     string
	path    string // the unpacked bundle (darwin) or binary (linux)
	version string
}

type updateState struct {
	opts UpdateOptions

	// The steps ApplyUpdate and PrepareUpdate take, replaceable in tests.
	unpack   func(archive, dir, goos, want string) (string, error)
	swap     func(target, staged, goos string) error
	relaunch func(target, goos string) error
	quit     func()

	mu     sync.Mutex
	busy   bool
	cancel context.CancelFunc
	staged *stagedUpdate
}

// WithUpdates turns on the update API on a. It is called once during wiring
// in main.go, not exposed as a Wails binding.
func WithUpdates(a *App, o UpdateOptions) {
	s := &updateState{
		opts:     o,
		unpack:   update.UnpackChecked,
		swap:     update.Swap,
		relaunch: update.Relaunch,
	}
	s.quit = func() { runtime.Quit(a.ctx) }
	a.upd = s
}

// updateError is an error whose text is written for the user and whose
// cause stays reachable for errors.Is.
type updateError struct {
	msg string
	err error
}

func (e *updateError) Error() string { return e.msg }
func (e *updateError) Unwrap() error { return e.err }

func userError(msg string, err error) error { return &updateError{msg, err} }

// downloadFailure is err as the user reads it. A cancellation stays as is so
// the caller can tell it from a failure.
func downloadFailure(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return err
	case errors.Is(err, update.ErrChecksum):
		return userError("The download didn't match the expected checksum", err)
	case errors.Is(err, update.ErrBadArchive), errors.Is(err, update.ErrVersionMismatch):
		return userError("The downloaded update isn't valid", err)
	}
	return userError("Couldn't download the update", err)
}

func unpackFailure(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return err
	case errors.Is(err, update.ErrBadArchive), errors.Is(err, update.ErrVersionMismatch):
		return userError("The downloaded update isn't valid", err)
	}
	return userError("Couldn't unpack the update: "+err.Error(), err)
}

// updateParent is the context update work hangs off: the app's, once it has
// started.
func (a *App) updateParent() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func (a *App) emitUpdateProgress(p UpdateProgress) {
	if a.updateParent().Err() == nil {
		a.emit("update:progress", p)
	}
}

// CheckForUpdate asks for the latest release and says whether it is newer
// than the running version and whether it can be installed in place. A
// failed check is an error the caller may ignore.
func (a *App) CheckForUpdate() (UpdateInfo, error) {
	if a.upd == nil {
		return UpdateInfo{}, ErrUpdatesOff
	}
	o := a.upd.opts
	current, err := update.ParseVersion(o.Version)
	if err != nil {
		return UpdateInfo{}, fmt.Errorf("running version: %w", err)
	}
	ctx, cancel := context.WithTimeout(a.updateParent(), checkTimeout)
	defer cancel()
	rel, err := o.Source.Latest(ctx)
	if err != nil {
		return UpdateInfo{}, err
	}
	latest, err := update.ParseVersion(rel.Version)
	if err != nil {
		return UpdateInfo{}, err
	}
	can := update.Check(o.Exe, o.GOOS, o.Dev, rel.AssetURL != "")
	return UpdateInfo{
		Current:    current.String(),
		Latest:     latest.String(),
		Newer:      current.Less(latest),
		PageURL:    rel.PageURL,
		CanInstall: can.CanInstall,
		Reason:     can.Reason,
	}, nil
}

// PrepareUpdate downloads the latest release, verifies and unpacks it, and
// keeps it for ApplyUpdate. It blocks until done, emitting update:progress,
// and fails with ErrUpdateBusy if another Prepare or Apply is running. A
// failed or cancelled attempt leaves nothing behind.
func (a *App) PrepareUpdate() (err error) {
	s := a.upd
	if s == nil {
		return ErrUpdatesOff
	}
	ctx, cancel := context.WithCancel(a.updateParent())
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		cancel()
		return ErrUpdateBusy
	}
	s.busy, s.cancel = true, cancel
	old := s.staged
	s.staged = nil
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		s.busy, s.cancel = false, nil
		s.mu.Unlock()
	}()
	if old != nil {
		os.RemoveAll(old.dir)
	}

	o := s.opts
	current, err := update.ParseVersion(o.Version)
	if err != nil {
		return fmt.Errorf("running version: %w", err)
	}
	rel, err := o.Source.Latest(ctx)
	if err != nil {
		return downloadFailure(err)
	}
	latest, err := update.ParseVersion(rel.Version)
	if err != nil {
		return downloadFailure(err)
	}
	if !current.Less(latest) {
		return ErrNoNewerVersion
	}
	if can := update.Check(o.Exe, o.GOOS, o.Dev, rel.AssetURL != ""); !can.CanInstall {
		return userError("CommitTree can't install this update itself: "+can.Reason, nil)
	}

	// One fresh, empty folder per attempt: Unpack merges into what it is given.
	dir, err := os.MkdirTemp("", "committree-update-*")
	if err != nil {
		return userError("Couldn't download the update", err)
	}
	defer func() {
		if err != nil {
			os.RemoveAll(dir)
		}
	}()

	var (
		last       time.Time
		sent       UpdateProgress
		seen       UpdateProgress
		haveSeen   bool
		progressMu sync.Mutex
		sendIfDue  = func(force bool) {
			if !haveSeen || (!force && time.Since(last) < progressEvery) || seen == sent {
				return
			}
			last, sent = time.Now(), seen
			a.emitUpdateProgress(seen)
		}
	)
	archive, err := o.Source.Download(ctx, rel, dir, func(done, total int64) {
		progressMu.Lock()
		defer progressMu.Unlock()
		seen, haveSeen = UpdateProgress{Phase: "download", Done: done, Total: total}, true
		sendIfDue(false)
	})
	if err != nil {
		return downloadFailure(err)
	}
	progressMu.Lock()
	sendIfDue(true) // the last numbers, even inside the throttle window
	progressMu.Unlock()

	a.emitUpdateProgress(UpdateProgress{Phase: "install"})
	unpacked := filepath.Join(dir, "unpacked")
	if err = os.Mkdir(unpacked, 0o755); err != nil {
		return unpackFailure(err)
	}
	path, err := s.unpack(archive, unpacked, o.GOOS, latest.String())
	if err != nil {
		return unpackFailure(err)
	}
	os.Remove(archive) // only the unpacked app is needed from here on
	if err = ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	s.staged = &stagedUpdate{dir: dir, path: path, version: latest.String()}
	s.mu.Unlock()
	return nil
}

// StagedUpdateVersion is the version PrepareUpdate staged for ApplyUpdate, or
// "" when nothing is staged. PrepareUpdate asks the server again, so it may
// have staged a version other than the one the last check announced.
func (a *App) StagedUpdateVersion() string {
	if a.upd == nil {
		return ""
	}
	a.upd.mu.Lock()
	defer a.upd.mu.Unlock()
	if a.upd.staged == nil {
		return ""
	}
	return a.upd.staged.version
}

// CancelUpdate stops a running PrepareUpdate, which then returns
// context.Canceled. It does nothing when none is running.
func (a *App) CancelUpdate() {
	if a.upd == nil {
		return
	}
	a.upd.mu.Lock()
	defer a.upd.mu.Unlock()
	if a.upd.cancel != nil {
		a.upd.cancel()
	}
}

// ApplyUpdate installs what PrepareUpdate staged over the running
// application, starts it again and quits. When the swap fails the old
// version is still in place and the staged update is kept for a retry.
func (a *App) ApplyUpdate() error {
	s := a.upd
	if s == nil {
		return ErrUpdatesOff
	}
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return ErrUpdateBusy
	}
	staged := s.staged
	if staged == nil {
		s.mu.Unlock()
		return ErrNothingPrepared
	}
	s.busy = true
	s.mu.Unlock()
	installed := false
	defer func() {
		s.mu.Lock()
		s.busy = installed // an installed update leaves the app quitting
		s.mu.Unlock()
	}()

	o := s.opts
	current, err := update.ParseVersion(o.Version)
	if err != nil {
		return fmt.Errorf("running version: %w", err)
	}
	next, err := update.ParseVersion(staged.version)
	if err != nil {
		return err
	}
	if !current.Less(next) {
		return ErrNoNewerVersion
	}
	can := update.Check(o.Exe, o.GOOS, o.Dev, true)
	if !can.CanInstall {
		return userError("CommitTree can't install this update itself: "+can.Reason, nil)
	}
	if err := s.swap(can.Target, staged.path, o.GOOS); err != nil {
		return userError("Couldn't install the update: "+err.Error(), err)
	}
	installed = true
	s.mu.Lock()
	s.staged = nil
	s.mu.Unlock()
	os.RemoveAll(staged.dir)

	if err := s.relaunch(can.Target, o.GOOS); err != nil {
		installed = false
		return userError("The update was installed, but CommitTree couldn't restart itself. Quit and open it again.", err)
	}
	s.quit()
	return nil
}

// cleanupUpdate stops a running PrepareUpdate and removes a staged update
// nobody applied. Called from Shutdown.
func (a *App) cleanupUpdate() {
	if a.upd == nil {
		return
	}
	a.CancelUpdate()
	a.upd.mu.Lock()
	staged := a.upd.staged
	a.upd.staged = nil
	a.upd.mu.Unlock()
	if staged != nil {
		os.RemoveAll(staged.dir)
	}
}
