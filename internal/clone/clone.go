// Package clone clones a remote repository with the git CLI, reporting
// git's progress as it goes.
package clone

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	neturl "net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"git-ui/internal/cmdlog"
	"git-ui/internal/gitcmd"
	"git-ui/internal/ops"
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

// Stall is how long a clone may go without git writing anything before it
// is stopped as a timeout.
const Stall = 5 * time.Minute

var (
	ErrBadURL = errors.New(`Enter a repository URL; it can't start with "-".`)
	// ErrCancelled is returned when the clone was cancelled.
	ErrCancelled = errors.New("clone cancelled")
)

// Run clones url into dest (an absolute path that Validate accepted),
// with its submodules, passing git's progress to onProgress as it comes.
// When the clone fails or is cancelled and dest did not exist before, dest
// is removed; a destination that existed (an empty folder) is left alone.
func Run(ctx context.Context, url, dest string, stall time.Duration, onProgress func(Progress)) error {
	if strings.TrimSpace(url) == "" || strings.HasPrefix(url, "-") {
		return ErrBadURL
	}
	_, statErr := os.Stat(dest)
	existed := statErr == nil
	_, err := gitcmd.RunStream(ctx, filepath.Dir(dest), nil, stall, func(line string) {
		if p, ok := ParseProgress(line); ok {
			onProgress(p)
		}
	}, "clone", "--progress", "--recurse-submodules", "--", url, dest)
	if err == nil {
		return nil
	}
	if !existed {
		_ = os.RemoveAll(dest)
	}
	if ctx.Err() != nil || errors.Is(err, gitcmd.ErrCancelled) {
		return fmt.Errorf("%w: %w", ErrCancelled, err)
	}
	return err
}

// Explain turns a failed Run into the message the user sees, with any
// credentials in url or in git's output hidden.
func Explain(err error, url string) string {
	redacted, secrets := cmdlog.RedactArgs([]string{url})
	if errors.Is(err, gitcmd.ErrTimeout) {
		return "Clone stalled: git sent nothing for 5 minutes."
	}
	var gerr *gitcmd.Error
	if !errors.As(err, &gerr) {
		return cmdlog.MaskOutput(err.Error(), secrets)
	}
	s := strings.ToLower(gerr.Stderr)
	switch {
	// Before IsAuthError, which counts this marker as an auth failure too.
	case strings.Contains(s, "host key verification failed"):
		return fmt.Sprintf("The host's SSH key isn't trusted yet. Connect once from a terminal (ssh -T %s) to accept it.", sshHost(url))
	case ops.IsAuthError(err):
		return "Authentication failed. Set up a credential helper or an SSH key for this host, then try again."
	case strings.Contains(s, "does not exist"), strings.Contains(s, "not found"),
		strings.Contains(s, "does not appear to be a git repository"):
		return fmt.Sprintf("Repository not found at %s.", redacted[0])
	}
	return cmdlog.MaskOutput(lastMessage(gerr), secrets)
}

// lastMessage is git's stderr without its progress lines.
func lastMessage(gerr *gitcmd.Error) string {
	var kept []string
	for _, l := range strings.FieldsFunc(gerr.Stderr, func(r rune) bool { return r == '\r' || r == '\n' }) {
		l = strings.TrimSpace(l)
		if _, ok := ParseProgress(l); ok || l == "" {
			continue
		}
		kept = append(kept, l)
	}
	if len(kept) == 0 {
		return gerr.Err.Error()
	}
	return strings.Join(kept, "\n")
}

// sshHost is the user@host to try `ssh -T` with: from an ssh:// URL or an
// scp-like git@host:path; otherwise url itself.
func sshHost(url string) string {
	if u, err := neturl.Parse(url); err == nil && u.Scheme == "ssh" {
		if u.User != nil {
			return u.User.Username() + "@" + u.Hostname()
		}
		return u.Hostname()
	}
	if host, _, ok := strings.Cut(url, ":"); ok && !strings.Contains(host, "/") {
		return host
	}
	return url
}
