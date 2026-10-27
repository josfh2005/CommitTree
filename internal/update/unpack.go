package update

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

var (
	// ErrBadArchive means the archive is unreadable or does not hold what a
	// CommitTree release holds.
	ErrBadArchive = errors.New("update archive is not valid")
	// ErrVersionMismatch means the unpacked app is not the version the
	// manifest announced.
	ErrVersionMismatch = errors.New("unpacked app has a different version than announced")
)

const (
	appName    = "CommitTree.app"
	binaryName = "CommitTree"
)

// Unpack extracts archive into dir. On darwin it returns dir/CommitTree.app;
// on linux, the extracted dir/CommitTree binary (mode 0755).
func Unpack(archive, dir, goos string) (string, error) {
	switch goos {
	case "darwin":
		return unpackDarwin(archive, dir)
	case "linux":
		return unpackLinux(archive, dir)
	}
	return "", fmt.Errorf("cannot unpack updates on %s", goos)
}

// UnpackChecked is Unpack plus, on darwin, a check that the bundle's
// CFBundleShortVersionString is want.
func UnpackChecked(archive, dir, goos, want string) (string, error) {
	p, err := Unpack(archive, dir, goos)
	if err != nil {
		return "", err
	}
	if goos == "darwin" {
		got, err := BundleVersion(p)
		if err != nil {
			return "", fmt.Errorf("%w: %v", ErrBadArchive, err)
		}
		if got != want {
			return "", fmt.Errorf("%w: found %s, announced %s", ErrVersionMismatch, got, want)
		}
	}
	return p, nil
}

// BundleVersion reads CFBundleShortVersionString from app's Info.plist.
func BundleVersion(app string) (string, error) {
	out, err := exec.Command("/usr/bin/plutil", "-extract", "CFBundleShortVersionString", "raw", "-o", "-",
		filepath.Join(app, "Contents", "Info.plist")).Output()
	if err != nil {
		return "", fmt.Errorf("read bundle version: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func unpackDarwin(archive, dir string) (string, error) {
	if err := checkZipNames(archive); err != nil {
		return "", err
	}
	if out, err := exec.Command("/usr/bin/ditto", "-x", "-k", archive, dir).CombinedOutput(); err != nil {
		return "", fmt.Errorf("%w: ditto: %v: %s", ErrBadArchive, err, strings.TrimSpace(string(out)))
	}
	app := filepath.Join(dir, appName)
	if fi, err := os.Lstat(app); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("%w: no %s inside", ErrBadArchive, appName)
	}
	return app, nil
}

// unpackLinux extracts the one regular file named exactly CommitTree and
// ignores every other entry, so no entry can write outside dir.
func unpackLinux(archive, dir string) (string, error) {
	f, err := os.Open(archive)
	if err != nil {
		return "", err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrBadArchive, err)
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return "", fmt.Errorf("%w: no %s inside", ErrBadArchive, binaryName)
		}
		if err != nil {
			return "", fmt.Errorf("%w: %v", ErrBadArchive, err)
		}
		if path.Clean(h.Name) != binaryName {
			continue
		}
		if h.Typeflag != tar.TypeReg {
			return "", fmt.Errorf("%w: %s is not a regular file", ErrBadArchive, binaryName)
		}
		if h.Size > maxDownload {
			return "", fmt.Errorf("%w: %s is larger than %d MiB", ErrBadArchive, binaryName, maxDownload>>20)
		}
		return writeBinary(dir, tr)
	}
}

func writeBinary(dir string, r io.Reader) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, binaryName+".*.part")
	if err != nil {
		return "", err
	}
	ok := false
	defer func() {
		tmp.Close()
		if !ok {
			os.Remove(tmp.Name())
		}
	}()
	n, err := io.Copy(tmp, io.LimitReader(r, maxDownload+1))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrBadArchive, err)
	}
	if n > maxDownload {
		return "", fmt.Errorf("%w: %s is larger than %d MiB", ErrBadArchive, binaryName, maxDownload>>20)
	}
	if err := tmp.Chmod(0o755); err != nil {
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	final := filepath.Join(dir, binaryName)
	if err := os.Rename(tmp.Name(), final); err != nil {
		return "", err
	}
	ok = true
	return final, nil
}
