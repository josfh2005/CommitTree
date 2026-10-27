package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

type tarEntry struct {
	name     string
	typeflag byte
	body     []byte
	link     string
}

func makeTarGz(t *testing.T, entries ...tarEntry) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Typeflag: e.typeflag, Mode: 0o644, Size: int64(len(e.body)), Linkname: e.link}
		if e.typeflag != tar.TypeReg {
			h.Size = 0
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if e.typeflag == tar.TypeReg {
			tw.Write(e.body)
		}
	}
	tw.Close()
	gz.Close()
	p := filepath.Join(t.TempDir(), "a.tar.gz")
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestUnpackLinux(t *testing.T) {
	bin := []byte("\x7fELF fake binary")
	arc := makeTarGz(t,
		tarEntry{name: "README", typeflag: tar.TypeReg, body: []byte("hi")},
		tarEntry{name: "./CommitTree", typeflag: tar.TypeReg, body: bin})
	dir := t.TempDir()
	p, err := UnpackChecked(arc, dir, "linux", "0.10.0") // want is only checked on darwin
	if err != nil {
		t.Fatal(err)
	}
	if p != filepath.Join(dir, "CommitTree") {
		t.Errorf("path = %q", p)
	}
	got, _ := os.ReadFile(p)
	if !bytes.Equal(got, bin) {
		t.Error("content differs")
	}
	fi, _ := os.Stat(p)
	if fi.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v; want 0755", fi.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("dir has %d entries; want only CommitTree (README must not be extracted)", len(entries))
	}
}

func TestUnpackLinuxEscape(t *testing.T) {
	body := []byte("x")
	cases := map[string][]tarEntry{
		"dotdot":      {{name: "../CommitTree", typeflag: tar.TypeReg, body: body}},
		"absolute":    {{name: "/etc/x", typeflag: tar.TypeReg, body: body}},
		"absolute-ct": {{name: "/CommitTree", typeflag: tar.TypeReg, body: body}},
		"nested":      {{name: "sub/CommitTree", typeflag: tar.TypeReg, body: body}},
		"symlink":     {{name: "CommitTree", typeflag: tar.TypeSymlink, link: "/etc/passwd"}},
		"symlink-other": {
			{name: "evil", typeflag: tar.TypeSymlink, link: "/etc/passwd"}},
		"missing": {{name: "other", typeflag: tar.TypeReg, body: body}},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			_, err := Unpack(makeTarGz(t, entries...), dir, "linux")
			if !errors.Is(err, ErrBadArchive) {
				t.Fatalf("err = %v; want ErrBadArchive", err)
			}
			assertEmpty(t, dir)
		})
	}
	// Not a gzip file at all.
	bad := filepath.Join(t.TempDir(), "bad")
	os.WriteFile(bad, []byte("not gzip"), 0o644)
	if _, err := Unpack(bad, t.TempDir(), "linux"); !errors.Is(err, ErrBadArchive) {
		t.Errorf("garbage: err = %v; want ErrBadArchive", err)
	}
}

func TestUnpackLinuxTooLarge(t *testing.T) {
	old := maxDownload
	maxDownload = 1 << 10
	defer func() { maxDownload = old }()
	arc := makeTarGz(t, tarEntry{name: "CommitTree", typeflag: tar.TypeReg, body: make([]byte, 2<<10)})
	dir := t.TempDir()
	if _, err := Unpack(arc, dir, "linux"); !errors.Is(err, ErrBadArchive) {
		t.Fatalf("err = %v; want ErrBadArchive", err)
	}
	assertEmpty(t, dir)
}

func TestUnpackUnsupportedOS(t *testing.T) {
	if _, err := Unpack("x", t.TempDir(), "plan9"); err == nil {
		t.Error("unknown OS should fail")
	}
}

// makeAppZip zips a fake bundle (or, with withApp false, an unrelated folder).
func makeAppZip(t *testing.T, version string, withApp bool) string {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("needs /usr/bin/ditto")
	}
	src := t.TempDir()
	top := "CommitTree.app"
	if !withApp {
		top = "Other.app"
	}
	contents := filepath.Join(src, top, "Contents")
	if err := os.MkdirAll(contents, 0o755); err != nil {
		t.Fatal(err)
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleShortVersionString</key><string>` + version + `</string>
</dict></plist>`
	if err := os.WriteFile(filepath.Join(contents, "Info.plist"), []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}
	zip := filepath.Join(t.TempDir(), "a.zip")
	if out, err := exec.Command("/usr/bin/ditto", "-c", "-k", "--keepParent", filepath.Join(src, top), zip).CombinedOutput(); err != nil {
		t.Fatalf("ditto: %v: %s", err, out)
	}
	return zip
}

func TestUnpackDarwin(t *testing.T) {
	zip := makeAppZip(t, "0.10.0", true)
	dir := t.TempDir()
	p, err := UnpackChecked(zip, dir, "darwin", "0.10.0")
	if err != nil {
		t.Fatal(err)
	}
	if p != filepath.Join(dir, "CommitTree.app") {
		t.Errorf("path = %q", p)
	}
	if v, err := BundleVersion(p); err != nil || v != "0.10.0" {
		t.Errorf("BundleVersion = %q, %v", v, err)
	}
}

func TestUnpackDarwinVersionMismatch(t *testing.T) {
	zip := makeAppZip(t, "0.10.0", true)
	if _, err := UnpackChecked(zip, t.TempDir(), "darwin", "0.11.0"); !errors.Is(err, ErrVersionMismatch) {
		t.Fatalf("err = %v; want ErrVersionMismatch", err)
	}
}

func TestUnpackDarwinNoBundle(t *testing.T) {
	zip := makeAppZip(t, "0.10.0", false)
	if _, err := Unpack(zip, t.TempDir(), "darwin"); !errors.Is(err, ErrBadArchive) {
		t.Fatalf("err = %v; want ErrBadArchive", err)
	}
}

func TestUnpackDarwinNotAZip(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("needs /usr/bin/ditto")
	}
	bad := filepath.Join(t.TempDir(), "bad.zip")
	os.WriteFile(bad, []byte("not a zip"), 0o644)
	if _, err := Unpack(bad, t.TempDir(), "darwin"); !errors.Is(err, ErrBadArchive) {
		t.Fatalf("err = %v; want ErrBadArchive", err)
	}
}
