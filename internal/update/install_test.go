package update

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
