package update

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"strings"
)

// checkZipNames refuses a zip whose entries could write outside the folder it
// is extracted into: absolute names, names with a ".." element, and symbolic
// links that point outside the bundle. It runs before ditto sees the archive.
func checkZipNames(archive string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBadArchive, err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		name := strings.ReplaceAll(f.Name, `\`, "/")
		if strings.HasPrefix(name, "/") || hasDotDot(name) {
			return fmt.Errorf("%w: refusing entry %q", ErrBadArchive, f.Name)
		}
		if f.Mode()&os.ModeSymlink == 0 {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("%w: %v", ErrBadArchive, err)
		}
		b, err := io.ReadAll(io.LimitReader(rc, 4096))
		rc.Close()
		if err != nil {
			return fmt.Errorf("%w: %v", ErrBadArchive, err)
		}
		link := strings.ReplaceAll(string(b), `\`, "/")
		top, _, _ := strings.Cut(name, "/")
		dest := path.Join(path.Dir(name), link)
		if strings.HasPrefix(link, "/") || (dest != top && !strings.HasPrefix(dest, top+"/")) {
			return fmt.Errorf("%w: refusing link %q pointing outside the app", ErrBadArchive, f.Name)
		}
	}
	return nil
}

// hasDotDot reports whether the slash-separated p has a ".." element.
func hasDotDot(p string) bool {
	for _, el := range strings.Split(p, "/") {
		if el == ".." {
			return true
		}
	}
	return false
}

var shortVersionRE = regexp.MustCompile(`<key>CFBundleShortVersionString</key>\s*<string>([^<]*)</string>`)

// ZipBundleVersion reads CFBundleShortVersionString from the XML
// CommitTree.app/Contents/Info.plist inside the release zip at archive,
// without extracting it.
func ZipBundleVersion(archive string) (string, error) {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return "", err
	}
	defer zr.Close()
	want := appName + "/Contents/Info.plist"
	for _, f := range zr.File {
		if f.Name != want {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		b, err := io.ReadAll(io.LimitReader(rc, 1<<20))
		rc.Close()
		if err != nil {
			return "", err
		}
		m := shortVersionRE.FindSubmatch(b)
		if m == nil {
			return "", fmt.Errorf("no CFBundleShortVersionString in %s", want)
		}
		return strings.TrimSpace(string(m[1])), nil
	}
	return "", fmt.Errorf("no %s in the zip", want)
}
