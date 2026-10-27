package update

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestTargetDarwin(t *testing.T) {
	got, err := Target("/Applications/CommitTree.app/Contents/MacOS/CommitTree", "darwin")
	if err != nil || got != "/Applications/CommitTree.app" {
		t.Fatalf("Target = %q, %v", got, err)
	}
	if _, err := Target("/usr/local/bin/CommitTree", "darwin"); !errors.Is(err, ErrNotBundle) {
		t.Fatalf("err = %v, want ErrNotBundle", err)
	}
	// An .app folder that is not on the way to Contents/MacOS is not the bundle.
	if _, err := Target("/Users/x/Foo.app/bin/CommitTree", "darwin"); !errors.Is(err, ErrNotBundle) {
		t.Fatalf("err = %v, want ErrNotBundle", err)
	}
	// The nearest bundle wins when bundles nest.
	got, err = Target("/Apps/Outer.app/Contents/MacOS/Inner.app/Contents/MacOS/CommitTree", "darwin")
	if err != nil || got != "/Apps/Outer.app/Contents/MacOS/Inner.app" {
		t.Fatalf("nested Target = %q, %v", got, err)
	}
}

func TestTargetLinux(t *testing.T) {
	got, err := Target("/opt/commit tree/CommitTree", "linux")
	if err != nil || got != "/opt/commit tree/CommitTree" {
		t.Fatalf("Target = %q, %v", got, err)
	}
}

// makeBundle creates dir/CommitTree.app/Contents/MacOS/CommitTree and returns
// the bundle and the executable paths.
func makeBundle(t *testing.T, dir string) (bundle, exe string) {
	t.Helper()
	bundle = filepath.Join(dir, "CommitTree.app")
	exe = filepath.Join(bundle, "Contents", "MacOS", "CommitTree")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	return bundle, exe
}

func TestCanInstallTranslocatedOrReadOnly(t *testing.T) {
	t.Run("translocated", func(t *testing.T) {
		exe := "/private/var/folders/x/AppTranslocation/ABC/d/CommitTree.app/Contents/MacOS/CommitTree"
		got := Check(exe, "darwin", false, true)
		want := "running from a temporary location — move CommitTree to Applications first"
		if got.CanInstall || got.Reason != want {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("read-only folder", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root can write anywhere")
		}
		dir := t.TempDir()
		bundle, exe := makeBundle(t, dir)
		if err := os.Chmod(dir, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(dir, 0o755) })
		got := Check(exe, "darwin", false, true)
		if got.CanInstall || got.Reason != "CommitTree can't write to "+dir {
			t.Fatalf("got %+v", got)
		}
		if got.Target != bundle {
			t.Fatalf("Target = %q, want %q", got.Target, bundle)
		}
	})
	t.Run("development build", func(t *testing.T) {
		_, exe := makeBundle(t, t.TempDir())
		got := Check(exe, "darwin", true, true)
		if got.CanInstall || got.Reason != "development build" {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("not a bundle", func(t *testing.T) {
		got := Check("/usr/local/bin/CommitTree", "darwin", false, true)
		if got.CanInstall || got.Reason != "not running from an app bundle" {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("no asset", func(t *testing.T) {
		_, exe := makeBundle(t, t.TempDir())
		got := Check(exe, "darwin", false, false)
		if got.CanInstall || got.Reason != "no download for this platform" {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("writable bundle", func(t *testing.T) {
		dir := t.TempDir()
		bundle, exe := makeBundle(t, dir)
		got := Check(exe, "darwin", false, true)
		if !got.CanInstall || got.Reason != "" || got.Target != bundle {
			t.Fatalf("got %+v", got)
		}
		entries, _ := os.ReadDir(dir)
		if len(entries) != 1 {
			t.Fatalf("probe file left behind: %v", entries)
		}
	})
	t.Run("writable linux binary", func(t *testing.T) {
		dir := t.TempDir()
		exe := filepath.Join(dir, "CommitTree")
		os.WriteFile(exe, []byte("x"), 0o755)
		got := Check(exe, "linux", false, true)
		if !got.CanInstall || got.Target != exe {
			t.Fatalf("got %+v", got)
		}
	})
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSwapLinux(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "CommitTree")
	os.WriteFile(target, []byte("old"), 0o755)
	staged := filepath.Join(t.TempDir(), "CommitTree")
	os.WriteFile(staged, []byte("new"), 0o644)

	if err := Swap(target, staged, "linux"); err != nil {
		t.Fatal(err)
	}
	if readFile(t, target) != "new" {
		t.Fatal("target not replaced")
	}
	st, _ := os.Stat(target)
	if st.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v", st.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("leftovers: %v", entries)
	}
}

func TestSwapLinuxMissingStagedKeepsTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "CommitTree")
	os.WriteFile(target, []byte("old"), 0o755)
	if err := Swap(target, filepath.Join(dir, "nope"), "linux"); err == nil {
		t.Fatal("want error")
	}
	if readFile(t, target) != "old" {
		t.Fatal("target changed")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("leftovers: %v", entries)
	}
}

func TestSwapDarwin(t *testing.T) {
	x := t.TempDir()
	target := filepath.Join(x, "CommitTree.app")
	os.MkdirAll(target, 0o755)
	os.WriteFile(filepath.Join(target, "old"), []byte("o"), 0o644)
	staged := filepath.Join(t.TempDir(), "CommitTree.app")
	os.MkdirAll(staged, 0o755)
	os.WriteFile(filepath.Join(staged, "new"), []byte("n"), 0o644)

	if err := Swap(target, staged, "darwin"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "new")); err != nil {
		t.Fatalf("new missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "old")); !os.IsNotExist(err) {
		t.Fatalf("old still there: %v", err)
	}
	entries, _ := os.ReadDir(x)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".CommitTree.app.old-") {
			t.Fatalf("old bundle left: %s", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Fatalf("unexpected entries: %v", entries)
	}
}

func TestSwapDarwinRollback(t *testing.T) {
	x := t.TempDir()
	target := filepath.Join(x, "CommitTree.app")
	os.MkdirAll(target, 0o755)
	os.WriteFile(filepath.Join(target, "old"), []byte("o"), 0o644)

	if err := Swap(target, filepath.Join(t.TempDir(), "CommitTree.app"), "darwin"); err == nil {
		t.Fatal("want error")
	}
	if _, err := os.Stat(filepath.Join(target, "old")); err != nil {
		t.Fatalf("old bundle not restored: %v", err)
	}
	entries, _ := os.ReadDir(x)
	if len(entries) != 1 {
		t.Fatalf("unexpected entries: %v", entries)
	}
}

func TestRelaunchCommand(t *testing.T) {
	name, args := RelaunchCommand("/Applications/Commit Tree's.app", "darwin", 4242)
	if name != "/bin/sh" {
		t.Fatalf("name = %q", name)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"kill -0 4242", `/usr/bin/open -n '/Applications/Commit Tree'\''s.app'`} {
		if !strings.Contains(joined, want) {
			t.Errorf("args %q lack %q", joined, want)
		}
	}

	name, args = RelaunchCommand("/opt/x/CommitTree", "linux", 7)
	if name != "/bin/sh" {
		t.Fatalf("name = %q", name)
	}
	joined = strings.Join(args, " ")
	for _, want := range []string{"kill -0 7", "exec '/opt/x/CommitTree'"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args %q lack %q", joined, want)
		}
	}
	if strings.Contains(joined, "open -n") {
		t.Errorf("linux command must not call open: %q", joined)
	}
}

func TestSwapDarwinRemovesStaleOldBundle(t *testing.T) {
	x := t.TempDir()
	target := filepath.Join(x, "CommitTree.app")
	os.MkdirAll(target, 0o755)
	os.WriteFile(filepath.Join(target, "old"), []byte("o"), 0o644)
	stale := filepath.Join(x, fmt.Sprintf(".CommitTree.app.old-%d", os.Getpid()))
	os.MkdirAll(stale, 0o755)
	os.WriteFile(filepath.Join(stale, "junk"), []byte("j"), 0o644)
	staged := filepath.Join(t.TempDir(), "CommitTree.app")
	os.MkdirAll(staged, 0o755)
	os.WriteFile(filepath.Join(staged, "new"), []byte("n"), 0o644)

	if err := Swap(target, staged, "darwin"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "new")); err != nil {
		t.Fatalf("new missing: %v", err)
	}
	entries, _ := os.ReadDir(x)
	if len(entries) != 1 {
		t.Fatalf("leftovers: %v", entries)
	}
}

func TestRelaunchCapsTheWait(t *testing.T) {
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("no sleep")
	}
	// A process that never exits within the test: the relaunch shell must
	// give up waiting and start the target anyway.
	hog := exec.Command("sleep", "60")
	if err := hog.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hog.Process.Kill(); hog.Wait() })

	dir := t.TempDir()
	marker := filepath.Join(dir, "started")
	target := filepath.Join(dir, "CommitTree")
	os.WriteFile(target, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755)

	name, args := relaunchCommand(target, "linux", hog.Process.Pid, 3)
	start := time.Now()
	if err := exec.Command(name, args...).Run(); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("the wait was not capped: took %v", d)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("target was not started after the capped wait: %v", err)
	}
}

func TestRelaunchCommandCapsWaitAtThirtySeconds(t *testing.T) {
	_, args := RelaunchCommand("/opt/x/CommitTree", "linux", 7)
	if joined := strings.Join(args, " "); !strings.Contains(joined, "150") || !strings.Contains(joined, "sleep 0.2") {
		t.Errorf("wait loop is not capped at 150 x 0.2 s: %q", joined)
	}
}

// forceCrossVolume makes the first rename (staged -> next to target) fail
// with EXDEV, like a staging folder on another volume, and replaces the copy.
func forceCrossVolume(t *testing.T, staged string, copyFn func(src, dst string) error) {
	t.Helper()
	oldRename, oldCopy := renameFn, copyBundle
	t.Cleanup(func() { renameFn, copyBundle = oldRename, oldCopy })
	renameFn = func(from, to string) error {
		if from == staged {
			return &os.LinkError{Op: "rename", Old: from, New: to, Err: syscall.EXDEV}
		}
		return os.Rename(from, to)
	}
	copyBundle = copyFn
}

func TestSwapDarwinCrossVolumeCopiesBeforeTouchingTarget(t *testing.T) {
	x := t.TempDir()
	target := filepath.Join(x, "CommitTree.app")
	os.MkdirAll(target, 0o755)
	os.WriteFile(filepath.Join(target, "old"), []byte("o"), 0o644)
	staged := filepath.Join(t.TempDir(), "CommitTree.app")
	os.MkdirAll(staged, 0o755)
	os.WriteFile(filepath.Join(staged, "new"), []byte("n"), 0o644)

	copied := false
	forceCrossVolume(t, staged, func(src, dst string) error {
		// The multi-second copy: the installed bundle must still be there.
		if _, err := os.Stat(filepath.Join(target, "old")); err != nil {
			t.Errorf("installed bundle touched during the copy: %v", err)
		}
		if filepath.Dir(dst) != x {
			t.Errorf("copy goes to %q, want a folder next to the target", dst)
		}
		copied = true
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, "new"), []byte("n"), 0o644)
	})
	if err := Swap(target, staged, "darwin"); err != nil {
		t.Fatal(err)
	}
	if !copied {
		t.Fatal("copy path not taken")
	}
	if _, err := os.Stat(filepath.Join(target, "new")); err != nil {
		t.Fatalf("new missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "old")); !os.IsNotExist(err) {
		t.Fatalf("old still there: %v", err)
	}
	if entries, _ := os.ReadDir(x); len(entries) != 1 {
		t.Fatalf("leftovers: %v", entries)
	}
}

func TestSwapDarwinCrossVolumeCopyFailureKeepsOriginal(t *testing.T) {
	x := t.TempDir()
	target := filepath.Join(x, "CommitTree.app")
	os.MkdirAll(target, 0o755)
	os.WriteFile(filepath.Join(target, "old"), []byte("o"), 0o644)
	staged := filepath.Join(t.TempDir(), "CommitTree.app")
	os.MkdirAll(staged, 0o755)

	forceCrossVolume(t, staged, func(src, dst string) error {
		os.MkdirAll(dst, 0o755) // a half-written copy
		return errors.New("disk full")
	})
	if err := Swap(target, staged, "darwin"); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "old")); err != nil {
		t.Fatalf("original not in place: %v", err)
	}
	if entries, _ := os.ReadDir(x); len(entries) != 1 {
		t.Fatalf("leftovers: %v", entries)
	}
}

func TestSwapDarwinCrossVolumeWithRealDitto(t *testing.T) {
	if _, err := os.Stat("/usr/bin/ditto"); err != nil {
		t.Skip("no ditto")
	}
	x := t.TempDir()
	target := filepath.Join(x, "CommitTree.app")
	os.MkdirAll(target, 0o755)
	os.WriteFile(filepath.Join(target, "old"), []byte("o"), 0o644)
	staged := filepath.Join(t.TempDir(), "CommitTree.app")
	os.MkdirAll(staged, 0o755)
	os.WriteFile(filepath.Join(staged, "new"), []byte("n"), 0o644)

	forceCrossVolume(t, staged, dittoBundle)
	if err := Swap(target, staged, "darwin"); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(target, "new")); err != nil || string(b) != "n" {
		t.Fatalf("new = %q, %v", b, err)
	}
	if entries, _ := os.ReadDir(x); len(entries) != 1 {
		t.Fatalf("leftovers: %v", entries)
	}
}
