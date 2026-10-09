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
	ErrParentMissing     = errors.New("The parent folder doesn't exist.")
	ErrParentNotAbsolute = errors.New("Parent folder must be a full path (or start with ~).")
	ErrBadName           = errors.New(`The folder name can't be empty, "." or "..", or contain a slash.`)
	ErrDestNotEmpty      = errors.New("The destination already exists and isn't an empty folder.")
)

// expandParent turns the parent folder as typed into an absolute path:
// surrounding whitespace is trimmed and a leading "~" (alone or before a
// separator) becomes the home folder. "~user" is not expanded, so it stays
// relative and is refused.
func expandParent(parent string) (string, error) {
	parent = strings.TrimSpace(parent)
	if parent == "~" || strings.HasPrefix(parent, "~/") || strings.HasPrefix(parent, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return "", ErrParentNotAbsolute
		}
		parent = filepath.Join(home, parent[1:])
	}
	if !filepath.IsAbs(parent) {
		return "", ErrParentNotAbsolute
	}
	return filepath.Clean(parent), nil
}

// Validate checks that name can be cloned into under parent, as git would
// accept it: parent (as typed: trimmed, with a leading ~ expanded) is an
// absolute path to a directory, name is one plain path segment, and the
// destination does not exist or is an empty directory. It returns the
// destination path.
func Validate(parent, name string) (string, error) {
	parent, err := expandParent(parent)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(parent); err != nil || !info.IsDir() {
		return "", ErrParentMissing
	}
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return "", ErrBadName
	}
	dest := filepath.Join(parent, name)
	// Lstat: a dangling symlink is something that exists, not a free name.
	info, err := os.Lstat(dest)
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
	// ErrPartial is returned when git fetched the repository but failed
	// afterwards (a submodule, the checkout): the folder is kept.
	ErrPartial = errors.New("clone partly done")
)

// Run clones url into dest (an absolute path that Validate accepted),
// with its submodules, passing git's progress to onProgress as it comes.
// When the clone is cancelled or stalls, or fails before git had fetched
// the repository, and dest did not exist before, dest is removed; a
// destination that existed (an empty folder) is left alone. A failure
// after the fetch (a submodule, the checkout) leaves the repository git
// deliberately keeps and returns ErrPartial wrapping git's error.
func Run(ctx context.Context, url, dest string, stall time.Duration, onProgress func(Progress)) error {
	if strings.TrimSpace(url) == "" || strings.HasPrefix(url, "-") {
		return ErrBadURL
	}
	_, statErr := os.Lstat(dest)
	existed := statErr == nil
	_, err := gitcmd.RunStream(ctx, filepath.Dir(dest), nil, stall, func(line string) {
		if p, ok := ParseProgress(line); ok {
			onProgress(p)
		}
	}, "clone", "--progress", "--recurse-submodules", "--", url, dest)
	if err == nil {
		return nil
	}
	cancelled := ctx.Err() != nil || errors.Is(err, gitcmd.ErrCancelled)
	if !cancelled && !errors.Is(err, gitcmd.ErrTimeout) && hasHead(dest) {
		return fmt.Errorf("%w: %w", ErrPartial, err)
	}
	if !existed {
		_ = os.RemoveAll(dest)
	}
	if cancelled {
		return fmt.Errorf("%w: %w", ErrCancelled, err)
	}
	return err
}

// hasHead reports whether dest is a repository of its own with a valid
// HEAD, i.e. git got as far as fetching and checking out the main branch.
func hasHead(dest string) bool {
	if _, err := os.Lstat(filepath.Join(dest, ".git")); err != nil {
		return false // not its own repository: rev-parse would find a parent's
	}
	_, err := gitcmd.Run(context.Background(), dest, 10*time.Second, "rev-parse", "--verify", "-q", "HEAD")
	return err == nil
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
	if errors.Is(err, ErrPartial) {
		return "Cloned, but some submodules or files could not be checked out: " +
			cmdlog.MaskOutput(tail(lastMessage(gerr), 3), secrets)
	}
	s := strings.ToLower(gerr.Stderr)
	switch {
	// Before IsAuthError, which counts this marker as an auth failure too.
	case strings.Contains(s, "host key verification failed"):
		return fmt.Sprintf("The host's SSH key isn't trusted yet. Connect once from a terminal (ssh -T %s) to accept it.", sshHost(url))
	case ops.IsAuthError(err):
		return "Authentication failed. Set up a credential helper or an SSH key for this host, then try again."
	case notFound(s) && notFoundIsTopLevel(gerr.Stderr, redacted[0], secrets):
		return fmt.Sprintf("Repository not found at %s.", redacted[0])
	}
	return cmdlog.MaskOutput(lastMessage(gerr), secrets)
}

func notFound(lowerStderr string) bool {
	return strings.Contains(lowerStderr, "does not exist") || strings.Contains(lowerStderr, "not found") ||
		strings.Contains(lowerStderr, "does not appear to be a git repository")
}

// notFoundIsTopLevel is false when a not-found line of stderr quotes a
// path or URL other than the repository being cloned (a submodule's), so
// the message does not blame the wrong repository.
func notFoundIsTopLevel(stderr, redactedURL string, secrets []string) bool {
	want := normalizeURL(redactedURL)
	for _, l := range strings.FieldsFunc(stderr, func(r rune) bool { return r == '\r' || r == '\n' }) {
		if !notFound(strings.ToLower(l)) {
			continue
		}
		if _, rest, ok := strings.Cut(l, "'"); ok {
			if quoted, _, ok := strings.Cut(rest, "'"); ok && !namesURL(want, normalizeURL(cmdlog.MaskOutput(quoted, secrets))) {
				return false
			}
		}
	}
	return true
}

// namesURL reports whether quoted names the repository at want: the same
// URL, or its path alone (git over ssh or file:// quotes just the path).
func namesURL(want, quoted string) bool {
	if want == quoted {
		return true
	}
	path := strings.TrimLeft(quoted, "/")
	return path != "" && (strings.HasSuffix(want, "/"+path) || strings.HasSuffix(want, ":"+path))
}

func normalizeURL(u string) string {
	return strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(u), "/"), ".git")
}

// tail is the last n lines of s.
func tail(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
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
	_, secrets := cmdlog.RedactArgs([]string{url})
	return cmdlog.MaskOutput(sshHostOf(url), secrets)
}

func sshHostOf(url string) string {
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
