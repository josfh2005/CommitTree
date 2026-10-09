// Package clone clones a remote repository with the git CLI, reporting
// git's progress as it goes.
package clone

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Progress is one progress line from git clone.
type Progress struct {
	Phase string `json:"phase"`
	// Percent is the phase's percentage, or -1 when git gave none.
	Percent int    `json:"percent"`
	Detail  string `json:"detail"`
}

var (
	// "Receiving objects:  45% (2460/5466), 3.10 MiB | 6.01 MiB/s", with
	// "remote: " in front for the server's phases.
	percentLine = regexp.MustCompile(`^(?:remote: )?([A-Z][a-z]+(?: [a-z]+)*):\s+(\d{1,3})% \(([^)]*)\)(.*)$`)
	// "remote: Enumerating objects: 5466, done."
	countLine = regexp.MustCompile(`^(?:remote: )?([A-Z][a-z]+(?: [a-z]+)*): (\d+), done\.$`)
	// "Cloning into 'app'..." — the repository itself, then each submodule
	// by its absolute path.
	cloningLine = regexp.MustCompile(`^Cloning into '(.+)'\.\.\.$`)
)

// ParseProgress reads one line of git clone's stderr; ok is false for a
// line that is not progress (summaries, submodule notices, errors).
func ParseProgress(line string) (p Progress, ok bool) {
	line = strings.TrimSpace(line)
	if m := percentLine.FindStringSubmatch(line); m != nil {
		pct, _ := strconv.Atoi(m[2])
		detail := m[3]
		rest := strings.TrimSuffix(strings.TrimSpace(m[4]), "done.")
		if rest = strings.Trim(rest, ", "); rest != "" {
			detail += ", " + rest
		}
		return Progress{Phase: m[1], Percent: pct, Detail: detail}, true
	}
	if m := countLine.FindStringSubmatch(line); m != nil {
		return Progress{Phase: m[1], Percent: -1, Detail: m[2]}, true
	}
	if m := cloningLine.FindStringSubmatch(line); m != nil {
		return Progress{Phase: "Cloning into '" + filepath.Base(m[1]) + "'", Percent: -1}, true
	}
	return Progress{}, false
}

var (
	ErrParentMissing = errors.New("The parent folder doesn't exist.")
	ErrBadName       = errors.New(`The folder name can't be empty, "." or "..", or contain a slash.`)
	ErrDestNotEmpty  = errors.New("The destination already exists and isn't an empty folder.")
)

// Validate checks that name can be cloned into under parent, as git would
// accept it: parent is a directory, name is one plain path segment, and the
// destination does not exist or is an empty directory. It returns the
// destination path.
func Validate(parent, name string) (string, error) {
	if info, err := os.Stat(parent); err != nil || !info.IsDir() {
		return "", ErrParentMissing
	}
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return "", ErrBadName
	}
	dest := filepath.Join(parent, name)
	info, err := os.Stat(dest)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return dest, nil
	case err != nil:
		return "", err
	case !info.IsDir():
		return "", ErrDestNotEmpty
	}
	entries, err := os.ReadDir(dest)
	if err != nil {
		return "", err
	}
	if len(entries) > 0 {
		return "", ErrDestNotEmpty
	}
	return dest, nil
}
