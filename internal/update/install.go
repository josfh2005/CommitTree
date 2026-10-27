package update

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// ErrNotBundle means the running executable is not inside a macOS app bundle.
var ErrNotBundle = errors.New("not running from an app bundle")

// Installability says whether an update can replace the running application
// in place and, when it cannot, why (the Reason is shown to the user).
type Installability struct {
	Target     string
	CanInstall bool
	Reason     string
}

// Target returns what an update replaces. On darwin it is the nearest
// ancestor of exe named *.app that holds exe under Contents/MacOS/; on linux
// it is exe itself. exe must already have its symlinks resolved.
func Target(exe, goos string) (string, error) {
	if goos != "darwin" {
		return exe, nil
	}
	macOS := string(filepath.Separator) + filepath.Join("Contents", "MacOS") + string(filepath.Separator)
	for dir := filepath.Dir(exe); dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if strings.HasSuffix(dir, ".app") && strings.HasPrefix(exe, dir+macOS) {
			return dir, nil
		}
	}
	return "", ErrNotBundle
}

// Check decides whether the running application at exe can update itself.
// dev is true for a development build; hasAsset is whether the release has a
// download for this platform.
func Check(exe, goos string, dev bool, hasAsset bool) Installability {
	if dev {
		return Installability{Reason: "development build"}
	}
	target, err := Target(exe, goos)
	if err != nil {
		return Installability{Reason: "not running from an app bundle"}
	}
	no := func(reason string) Installability { return Installability{Target: target, Reason: reason} }
	if !hasAsset {
		return no("no download for this platform")
	}
	if strings.Contains(target, "/AppTranslocation/") {
		return no("running from a temporary location — move CommitTree to Applications first")
	}
	dir := filepath.Dir(target)
	if !writable(dir) {
		return no("CommitTree can't write to " + dir)
	}
	return Installability{Target: target, CanInstall: true}
}

// writable reports whether a file can be created in dir.
func writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".committree-write-*")
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(f.Name())
	return true
}

// Swap replaces target with staged: the app bundle on darwin, the binary on
// linux. If it fails the old version is left in place.
func Swap(target, staged, goos string) error {
	switch goos {
	case "darwin":
		return swapDarwin(target, staged)
	case "linux":
		return swapLinux(target, staged)
	}
	return fmt.Errorf("cannot install updates on %s", goos)
}

// Hooks the tests replace to force the cross-volume path.
var (
	renameFn   = os.Rename
	copyBundle = dittoBundle
)

// swapDarwin stages first and swaps second: the new bundle is brought next to
// target (a rename, or a copy when staged is on another volume) before the
// installed bundle is touched, so a slow copy never happens while no app
// exists at target. Only two same-folder renames remain after that.
func swapDarwin(target, staged string) error {
	dir, base, pid := filepath.Dir(target), filepath.Base(target), os.Getpid()
	old := filepath.Join(dir, fmt.Sprintf(".%s.old-%d", base, pid))
	next := filepath.Join(dir, fmt.Sprintf(".%s.new-%d", base, pid))
	// Leftovers from an earlier attempt of this pid would make a rename fail.
	os.RemoveAll(old)
	os.RemoveAll(next)
	err := renameFn(staged, next)
	if errors.Is(err, syscall.EXDEV) {
		err = copyBundle(staged, next)
	}
	if err != nil {
		os.RemoveAll(next)
		return err
	}
	if err := renameFn(target, old); err != nil {
		os.RemoveAll(next)
		return err
	}
	if err := renameFn(next, target); err != nil {
		os.RemoveAll(next)
		if rerr := renameFn(old, target); rerr != nil {
			return fmt.Errorf("%w (and the old version could not be restored: %v)", err, rerr)
		}
		return err
	}
	os.RemoveAll(old)
	return nil
}

// dittoBundle copies staged to dst (ditto keeps the bundle's attributes and
// links).
func dittoBundle(staged, dst string) error {
	if out, err := exec.Command("/usr/bin/ditto", staged, dst).CombinedOutput(); err != nil {
		os.RemoveAll(dst)
		return fmt.Errorf("copying the new version: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func swapLinux(target, staged string) (err error) {
	tmp := filepath.Join(filepath.Dir(target), fmt.Sprintf(".%s.new-%d", filepath.Base(target), os.Getpid()))
	defer func() {
		if err != nil {
			os.Remove(tmp)
		}
	}()
	src, err := os.Open(staged)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err = io.Copy(dst, src); err != nil {
		dst.Close()
		return err
	}
	// On disk before the rename makes it the program, so a crash cannot
	// leave a truncated binary under the real name.
	if err = dst.Sync(); err != nil {
		dst.Close()
		return err
	}
	if err = dst.Close(); err != nil {
		return err
	}
	if err = os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, target)
}

// RelaunchCommand builds the command that starts target again once the
// process pid has exited.
func RelaunchCommand(target, goos string, pid int) (name string, args []string) {
	return relaunchCommand(target, goos, pid, relaunchWaitTries)
}

// relaunchWaitTries bounds how long the relaunch shell waits for the old
// process to exit: tries x 0.2 s (30 s), after which it starts the target
// anyway rather than never reopening the app.
const relaunchWaitTries = 150

func relaunchCommand(target, goos string, pid, tries int) (name string, args []string) {
	start := "exec " + shellQuote(target)
	if goos == "darwin" {
		start = "/usr/bin/open -n " + shellQuote(target)
	}
	script := "n=0; while kill -0 " + strconv.Itoa(pid) + " 2>/dev/null && [ $n -lt " + strconv.Itoa(tries) + " ]; do sleep 0.2; n=$((n+1)); done; " + start
	return "/bin/sh", []string{"-c", script}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Relaunch starts target again, detached, after this process has exited. It
// returns without waiting; the caller then quits.
func Relaunch(target, goos string) error {
	name, args := RelaunchCommand(target, goos, os.Getpid())
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
