package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"git-ui/internal/update"
)

// fakeSource is an updateSource whose download the test controls.
type fakeSource struct {
	rel       update.Release
	latestErr error
	download  func(ctx context.Context, r update.Release, dir string, progress func(done, total int64)) (string, error)

	mu   sync.Mutex
	dirs []string // every dir Download was given
}

func (f *fakeSource) Latest(ctx context.Context) (update.Release, error) {
	return f.rel, f.latestErr
}

func (f *fakeSource) Download(ctx context.Context, r update.Release, dir string, progress func(done, total int64)) (string, error) {
	f.mu.Lock()
	f.dirs = append(f.dirs, dir)
	f.mu.Unlock()
	if f.download != nil {
		return f.download(ctx, r, dir, progress)
	}
	p := filepath.Join(dir, r.AssetName)
	if err := os.WriteFile(p, []byte("archive"), 0o644); err != nil {
		return "", err
	}
	progress(7, 7)
	return p, nil
}

func (f *fakeSource) downloadDirs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.dirs...)
}

// updateHarness is an App wired with WithUpdates, a fake source and fake
// unpack / swap / relaunch / quit steps that record what they were given.
type updateHarness struct {
	a   *App
	src *fakeSource
	ev  *events
	exe string

	mu          sync.Mutex
	unpackDirs  []string
	unpackEmpty []bool
	swapTarget  string
	swapStaged  string
	swapped     bool
	relaunched  string
	quit        bool
	calls       []string
}

func newUpdateHarness(t *testing.T, current, latest string, dev bool) *updateHarness {
	t.Helper()
	a, _ := newTestApp(t)
	a.ctx = context.Background()
	ev := newEvents()
	WithAI(a, AIDeps{Emit: ev.emit})

	exe := filepath.Join(t.TempDir(), "CommitTree.app", "Contents", "MacOS", "CommitTree")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("exe"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := &fakeSource{rel: update.Release{
		Version:     latest,
		PageURL:     "https://github.com/josfh2005/CommitTree/releases/tag/v" + latest,
		AssetName:   "CommitTree-test.zip",
		AssetURL:    "https://example.invalid/CommitTree-test.zip",
		AssetSHA256: strings.Repeat("a", 64),
	}}
	WithUpdates(a, UpdateOptions{Version: current, Source: src, Exe: exe, GOOS: runtime.GOOS, Dev: dev})

	h := &updateHarness{a: a, src: src, ev: ev, exe: exe}
	a.upd.unpack = func(archive, dir, goos, want string) (string, error) {
		entries, _ := os.ReadDir(dir)
		h.mu.Lock()
		h.unpackDirs = append(h.unpackDirs, dir)
		h.unpackEmpty = append(h.unpackEmpty, len(entries) == 0)
		h.calls = append(h.calls, "unpack")
		h.mu.Unlock()
		if _, err := os.Stat(archive); err != nil {
			return "", fmt.Errorf("archive missing: %w", err)
		}
		p := filepath.Join(dir, "CommitTree")
		return p, os.WriteFile(p, []byte("new"), 0o755)
	}
	a.upd.swap = func(target, staged, goos string) error {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.calls = append(h.calls, "swap")
		h.swapTarget, h.swapStaged, h.swapped = target, staged, true
		return nil
	}
	a.upd.relaunch = func(target, goos string) error {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.calls = append(h.calls, "relaunch")
		h.relaunched = target
		return nil
	}
	a.upd.quit = func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.calls = append(h.calls, "quit")
		h.quit = true
	}
	return h
}

func TestCheckForUpdateNewer(t *testing.T) {
	h := newUpdateHarness(t, "0.9.0", "0.10.0", false)
	info, err := h.a.CheckForUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if !info.Newer || !info.CanInstall || info.Reason != "" {
		t.Fatalf("info = %+v", info)
	}
	if info.Current != "0.9.0" || info.Latest != "0.10.0" || info.PageURL != h.src.rel.PageURL {
		t.Fatalf("info = %+v", info)
	}
}

func TestParseManifestNotNewer(t *testing.T) {
	for _, latest := range []string{"0.9.0", "0.8.5"} {
		h := newUpdateHarness(t, "0.9.0", latest, false)
		info, err := h.a.CheckForUpdate()
		if err != nil {
			t.Fatal(err)
		}
		if info.Newer {
			t.Errorf("latest %s reported as newer than 0.9.0: %+v", latest, info)
		}
	}
	h := newUpdateHarness(t, "dev", "0.10.0", false)
	if _, err := h.a.CheckForUpdate(); err == nil {
		t.Fatal("an invalid current version must be an error")
	}
}

func TestCheckForUpdateCannotInstall(t *testing.T) {
	h := newUpdateHarness(t, "0.9.0", "0.10.0", true)
	info, err := h.a.CheckForUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if !info.Newer || info.CanInstall || info.Reason != "development build" {
		t.Fatalf("info = %+v", info)
	}
}

func TestCheckForUpdateNoAsset(t *testing.T) {
	h := newUpdateHarness(t, "0.9.0", "0.10.0", false)
	h.src.rel.AssetName, h.src.rel.AssetURL, h.src.rel.AssetSHA256 = "", "", ""
	info, err := h.a.CheckForUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if info.CanInstall || info.Reason != "no download for this platform" {
		t.Fatalf("info = %+v", info)
	}
}

func TestCheckForUpdateSourceError(t *testing.T) {
	h := newUpdateHarness(t, "0.9.0", "0.10.0", false)
	h.src.latestErr = errors.New("boom")
	if _, err := h.a.CheckForUpdate(); err == nil {
		t.Fatal("want the source error")
	}
}

func TestUpdatesOff(t *testing.T) {
	a, _ := newTestApp(t)
	if _, err := a.CheckForUpdate(); !errors.Is(err, ErrUpdatesOff) {
		t.Errorf("CheckForUpdate: %v", err)
	}
	if err := a.PrepareUpdate(); !errors.Is(err, ErrUpdatesOff) {
		t.Errorf("PrepareUpdate: %v", err)
	}
	if err := a.ApplyUpdate(); !errors.Is(err, ErrUpdatesOff) {
		t.Errorf("ApplyUpdate: %v", err)
	}
	a.CancelUpdate() // must not panic
}

func TestPrepareBusy(t *testing.T) {
	h := newUpdateHarness(t, "0.9.0", "0.10.0", false)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	h.src.download = func(ctx context.Context, r update.Release, dir string, progress func(done, total int64)) (string, error) {
		once.Do(func() { close(entered) })
		<-release
		p := filepath.Join(dir, r.AssetName)
		return p, os.WriteFile(p, []byte("archive"), 0o644)
	}
	first := make(chan error, 1)
	go func() { first <- h.a.PrepareUpdate() }()
	<-entered

	if err := h.a.PrepareUpdate(); !errors.Is(err, ErrUpdateBusy) {
		t.Fatalf("second Prepare: %v", err)
	}
	if err := h.a.ApplyUpdate(); !errors.Is(err, ErrUpdateBusy) {
		t.Fatalf("Apply while preparing: %v", err)
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := h.a.PrepareUpdate(); err != nil {
		t.Fatalf("Prepare after the first finished: %v", err)
	}
}

func TestPrepareRetryAfterFailure(t *testing.T) {
	h := newUpdateHarness(t, "0.9.0", "0.10.0", false)
	fail := true
	h.src.download = func(ctx context.Context, r update.Release, dir string, progress func(done, total int64)) (string, error) {
		p := filepath.Join(dir, r.AssetName)
		if err := os.WriteFile(p, []byte("half"), 0o644); err != nil {
			return "", err
		}
		if fail {
			return "", update.ErrStalled
		}
		return p, nil
	}
	err := h.a.PrepareUpdate()
	if err == nil || !strings.Contains(err.Error(), "Couldn't download the update") {
		t.Fatalf("first Prepare: %v", err)
	}
	dirs := h.src.downloadDirs()
	if len(dirs) != 1 {
		t.Fatalf("dirs = %v", dirs)
	}
	if _, err := os.Stat(dirs[0]); !os.IsNotExist(err) {
		t.Fatalf("staging dir of the failed attempt is still there: %v", err)
	}

	fail = false
	if err := h.a.PrepareUpdate(); err != nil {
		t.Fatalf("retry: %v", err)
	}
	// A later Prepare replaces the staged update and removes its folder.
	staged := h.a.upd.staged.dir
	if err := h.a.PrepareUpdate(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Fatalf("replaced staging dir is still there: %v", err)
	}
	if h.a.upd.staged.dir == staged {
		t.Fatal("the staged update was not replaced")
	}
}

func TestPrepareUnpacksIntoAFreshEmptyDir(t *testing.T) {
	h := newUpdateHarness(t, "0.9.0", "0.10.0", false)
	if err := h.a.PrepareUpdate(); err != nil {
		t.Fatal(err)
	}
	if len(h.unpackDirs) != 1 || !h.unpackEmpty[0] {
		t.Fatalf("unpack dirs %v, empty %v", h.unpackDirs, h.unpackEmpty)
	}
	if _, err := os.Stat(filepath.Join(h.unpackDirs[0], "CommitTree")); err != nil {
		t.Fatal(err)
	}
	// The archive is not kept next to the unpacked app.
	if _, err := os.Stat(filepath.Join(h.src.downloadDirs()[0], h.src.rel.AssetName)); !os.IsNotExist(err) {
		t.Fatalf("archive kept: %v", err)
	}
}

func TestPrepareFailureOfUnpackRemovesTheDir(t *testing.T) {
	h := newUpdateHarness(t, "0.9.0", "0.10.0", false)
	h.a.upd.unpack = func(archive, dir, goos, want string) (string, error) {
		os.WriteFile(filepath.Join(dir, "junk"), []byte("x"), 0o644)
		return "", fmt.Errorf("%w: found 0.9.9, announced 0.10.0", update.ErrVersionMismatch)
	}
	err := h.a.PrepareUpdate()
	if err == nil || !strings.Contains(err.Error(), "The downloaded update isn't valid") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(h.src.downloadDirs()[0]); !os.IsNotExist(err) {
		t.Fatalf("staging dir left: %v", err)
	}
	if h.a.upd.staged != nil {
		t.Fatal("a failed attempt must not stay staged")
	}
}

func TestPrepareRefusesWhenItCannotInstall(t *testing.T) {
	h := newUpdateHarness(t, "0.9.0", "0.10.0", true)
	err := h.a.PrepareUpdate()
	if err == nil || !strings.Contains(err.Error(), "development build") {
		t.Fatalf("err = %v", err)
	}
	if len(h.src.downloadDirs()) != 0 {
		t.Fatal("downloaded although it cannot install")
	}
}

func TestPrepareRefusesWhenNotNewer(t *testing.T) {
	h := newUpdateHarness(t, "0.9.0", "0.9.0", false)
	if err := h.a.PrepareUpdate(); !errors.Is(err, ErrNoNewerVersion) {
		t.Fatalf("err = %v", err)
	}
}

func TestStagedUpdateVersion(t *testing.T) {
	h := newUpdateHarness(t, "0.9.0", "0.10.0", false)
	if got := h.a.StagedUpdateVersion(); got != "" {
		t.Fatalf("before Prepare: %q", got)
	}
	if err := h.a.PrepareUpdate(); err != nil {
		t.Fatal(err)
	}
	if got := h.a.StagedUpdateVersion(); got != "0.10.0" {
		t.Fatalf("after Prepare: %q", got)
	}
	// A newer release than the one a check announced is what gets staged.
	h.src.rel.Version = "0.11.0"
	if err := h.a.PrepareUpdate(); err != nil {
		t.Fatal(err)
	}
	if got := h.a.StagedUpdateVersion(); got != "0.11.0" {
		t.Fatalf("after second Prepare: %q", got)
	}
	var off App
	if got := off.StagedUpdateVersion(); got != "" {
		t.Fatalf("updates off: %q", got)
	}
}

func TestPrepareEmitsProgress(t *testing.T) {
	h := newUpdateHarness(t, "0.9.0", "0.10.0", false)
	h.src.download = func(ctx context.Context, r update.Release, dir string, progress func(done, total int64)) (string, error) {
		const total = 6400
		for done := int64(64); done <= total; done += 64 { // 100 callbacks in a burst
			progress(done, total)
		}
		p := filepath.Join(dir, r.AssetName)
		return p, os.WriteFile(p, []byte("archive"), 0o644)
	}
	if err := h.a.PrepareUpdate(); err != nil {
		t.Fatal(err)
	}
	var downloads []UpdateProgress
	installAt := -1
	h.ev.mu.Lock()
	for i, e := range h.ev.list {
		if e.name != "update:progress" {
			continue
		}
		p, ok := e.data.(UpdateProgress)
		if !ok {
			t.Fatalf("payload is %T", e.data)
		}
		switch p.Phase {
		case "download":
			downloads = append(downloads, p)
		case "install":
			installAt = i
		default:
			t.Fatalf("phase %q", p.Phase)
		}
	}
	h.ev.mu.Unlock()
	if len(downloads) == 0 || len(downloads) > 20 {
		t.Fatalf("%d download events for a burst of 100 callbacks", len(downloads))
	}
	if last := downloads[len(downloads)-1]; last.Done != 6400 || last.Total != 6400 {
		t.Fatalf("last download event = %+v", last)
	}
	if installAt < 0 {
		t.Fatal("no install-phase event")
	}
}

func TestApplyWithoutPrepare(t *testing.T) {
	h := newUpdateHarness(t, "0.9.0", "0.10.0", false)
	if err := h.a.ApplyUpdate(); !errors.Is(err, ErrNothingPrepared) {
		t.Fatalf("err = %v", err)
	}
	if h.swapped || h.quit {
		t.Fatal("swapped or quit without anything prepared")
	}
}

func TestApplySwapsRelaunchesAndQuits(t *testing.T) {
	h := newUpdateHarness(t, "0.9.0", "0.10.0", false)
	if err := h.a.PrepareUpdate(); err != nil {
		t.Fatal(err)
	}
	staged := h.a.upd.staged.path
	dir := h.a.upd.staged.dir
	if err := h.a.ApplyUpdate(); err != nil {
		t.Fatal(err)
	}
	wantTarget, err := update.Target(h.exe, runtime.GOOS)
	if err != nil {
		t.Fatal(err)
	}
	if h.swapTarget != wantTarget || h.swapStaged != staged || h.relaunched != wantTarget {
		t.Fatalf("swap(%q, %q), relaunch(%q); want target %q staged %q", h.swapTarget, h.swapStaged, h.relaunched, wantTarget, staged)
	}
	if got := strings.Join(h.calls, ","); got != "unpack,swap,relaunch,quit" {
		t.Fatalf("calls = %s", got)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("staging dir left after installing: %v", err)
	}
}

func TestApplyChecksTheVersionAgain(t *testing.T) {
	h := newUpdateHarness(t, "0.9.0", "0.10.0", false)
	if err := h.a.PrepareUpdate(); err != nil {
		t.Fatal(err)
	}
	h.a.upd.staged.version = "0.9.0"
	if err := h.a.ApplyUpdate(); !errors.Is(err, ErrNoNewerVersion) {
		t.Fatalf("err = %v", err)
	}
	if h.swapped || h.quit {
		t.Fatal("swapped a version that is not newer")
	}
}

func TestApplySwapFailureKeepsTheStagedUpdate(t *testing.T) {
	h := newUpdateHarness(t, "0.9.0", "0.10.0", false)
	if err := h.a.PrepareUpdate(); err != nil {
		t.Fatal(err)
	}
	h.a.upd.swap = func(target, staged, goos string) error { return errors.New("read-only volume") }
	err := h.a.ApplyUpdate()
	if err == nil || !strings.Contains(err.Error(), "Couldn't install the update") || !strings.Contains(err.Error(), "read-only volume") {
		t.Fatalf("err = %v", err)
	}
	if h.quit || h.relaunched != "" {
		t.Fatal("quit or relaunched after a failed swap")
	}
	if h.a.upd.staged == nil {
		t.Fatal("the staged update must stay so Apply can be retried")
	}
	// ... and it can be retried.
	h.a.upd.swap = func(target, staged, goos string) error { return nil }
	if err := h.a.ApplyUpdate(); err != nil {
		t.Fatalf("retry: %v", err)
	}
}

func TestApplyRelaunchFailureDoesNotQuit(t *testing.T) {
	h := newUpdateHarness(t, "0.9.0", "0.10.0", false)
	if err := h.a.PrepareUpdate(); err != nil {
		t.Fatal(err)
	}
	h.a.upd.relaunch = func(target, goos string) error { return errors.New("no sh") }
	err := h.a.ApplyUpdate()
	if err == nil || !strings.Contains(err.Error(), "open it again") {
		t.Fatalf("err = %v", err)
	}
	if h.quit {
		t.Fatal("quit although nothing will reopen the app")
	}
}

func TestCancelUpdate(t *testing.T) {
	h := newUpdateHarness(t, "0.9.0", "0.10.0", false)
	entered := make(chan struct{})
	h.src.download = func(ctx context.Context, r update.Release, dir string, progress func(done, total int64)) (string, error) {
		close(entered)
		<-ctx.Done()
		return "", ctx.Err()
	}
	res := make(chan error, 1)
	go func() { res <- h.a.PrepareUpdate() }()
	<-entered
	h.a.CancelUpdate()
	select {
	case err := <-res:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Prepare did not return after Cancel")
	}
	if _, err := os.Stat(h.src.downloadDirs()[0]); !os.IsNotExist(err) {
		t.Fatalf("staging dir left after cancel: %v", err)
	}
	// Cancel with nothing running is harmless, and Prepare can run again.
	h.a.CancelUpdate()
	h.src.download = nil
	if err := h.a.PrepareUpdate(); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateErrorTexts(t *testing.T) {
	for _, c := range []struct {
		name     string
		download error
		unpack   error
		want     string
	}{
		{"checksum", update.ErrChecksum, nil, "The download didn't match the expected checksum"},
		{"stalled", update.ErrStalled, nil, "Couldn't download the update"},
		{"network", errors.New("dial tcp: no route to host"), nil, "Couldn't download the update"},
		{"too large", update.ErrTooLarge, nil, "Couldn't download the update"},
		{"version mismatch", nil, update.ErrVersionMismatch, "The downloaded update isn't valid"},
		{"bad archive", nil, update.ErrBadArchive, "The downloaded update isn't valid"},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newUpdateHarness(t, "0.9.0", "0.10.0", false)
			if c.download != nil {
				h.src.download = func(ctx context.Context, r update.Release, dir string, progress func(done, total int64)) (string, error) {
					return "", fmt.Errorf("wrapped: %w", c.download)
				}
			}
			if c.unpack != nil {
				h.a.upd.unpack = func(archive, dir, goos, want string) (string, error) {
					return "", fmt.Errorf("wrapped: %w", c.unpack)
				}
			}
			err := h.a.PrepareUpdate()
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want text %q", err, c.want)
			}
			if cause := errors.Unwrap(err); cause == nil {
				t.Error("the cause must stay reachable with errors.Is")
			}
		})
	}
}
