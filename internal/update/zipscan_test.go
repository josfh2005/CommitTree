package update

import (
	"archive/zip"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type zipEntry struct {
	name string
	body string
	mode os.FileMode // 0 = regular file
}

func makeZip(t *testing.T, entries ...zipEntry) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "a.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		m := e.mode
		if m == 0 {
			m = 0o644
		}
		h.SetMode(m)
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(e.body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return p
}

func TestUnpackDarwinRefusesEscapingEntries(t *testing.T) {
	cases := map[string][]zipEntry{
		"dot dot":       {{name: "CommitTree.app/Contents/Info.plist", body: "x"}, {name: "../evil", body: "x"}},
		"nested dotdot": {{name: "CommitTree.app/../../evil", body: "x"}},
		"absolute":      {{name: "/tmp/evil", body: "x"}},
		"link up":       {{name: "CommitTree.app/l", body: "../../outside", mode: os.ModeSymlink | 0o755}},
		"link absolute": {{name: "CommitTree.app/l", body: "/etc", mode: os.ModeSymlink | 0o755}},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			arc := makeZip(t, entries...)
			dir := t.TempDir()
			// Unpack must refuse before extracting anything (and without ditto).
			if _, err := Unpack(arc, dir, "darwin"); !errors.Is(err, ErrBadArchive) {
				t.Fatalf("err = %v; want ErrBadArchive", err)
			}
			assertEmpty(t, dir)
			if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "evil")); err == nil {
				t.Fatal("wrote outside the folder")
			}
		})
	}
}

func TestCheckZipNamesAllowsBundleLinks(t *testing.T) {
	arc := makeZip(t,
		zipEntry{name: "CommitTree.app/Contents/Frameworks/X.framework/Versions/A/X", body: "x"},
		zipEntry{name: "CommitTree.app/Contents/Frameworks/X.framework/Versions/Current", body: "A", mode: os.ModeSymlink | 0o755},
		zipEntry{name: "CommitTree.app/Contents/Frameworks/X.framework/X", body: "Versions/Current/X", mode: os.ModeSymlink | 0o755},
		zipEntry{name: "CommitTree.app/Contents/Frameworks/X.framework/Versions/A/Y", body: "../../../Resources", mode: os.ModeSymlink | 0o755})
	if err := checkZipNames(arc); err != nil {
		t.Fatal(err)
	}
}

func TestZipBundleVersion(t *testing.T) {
	plist := `<?xml version="1.0"?><plist><dict><key>CFBundleShortVersionString</key>
<string>0.10.0</string></dict></plist>`
	arc := makeZip(t, zipEntry{name: "CommitTree.app/Contents/Info.plist", body: plist})
	if v, err := ZipBundleVersion(arc); err != nil || v != "0.10.0" {
		t.Fatalf("v = %q, %v", v, err)
	}
	if _, err := ZipBundleVersion(makeZip(t, zipEntry{name: "Other.app/Contents/Info.plist", body: plist})); err == nil {
		t.Error("want an error when Info.plist is missing")
	}
}
