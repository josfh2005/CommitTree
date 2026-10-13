package credprompt

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"git-ui/internal/gitcmd"
)

// RememberTarget is where a credential typed in the dialog is kept when
// "Remember" is checked.
type RememberTarget struct {
	// Name labels the checkbox: "Remember in <Name>".
	Name string `json:"name"`
	// User is true for the user's own helper, which git stores into by
	// itself on success; false for the platform helper CommitTree approves
	// into.
	User bool `json:"-"`
}

// findRemember is the RememberTarget for url as git sees it in dir ("" for
// a clone: config outside any repository), or nil when nothing could keep
// it. platform is DetectPlatformHelper's answer.
func findRemember(ctx context.Context, dir, rawURL, platform string) *RememberTarget {
	if dir == "" {
		dir = os.TempDir()
	}
	helpers := userHelpers(ctx, dir, rawURL)
	if n := len(helpers); n > 0 {
		return &RememberTarget{Name: helperLabel(helpers[n-1]), User: true}
	}
	if platform != "" {
		return &RememberTarget{Name: helperLabel(platform)}
	}
	return nil
}

// userHelpers lists, in git's order, the credential helpers that apply to
// rawURL: credential.helper and every matching credential.<url>.helper; an
// empty value resets the list, as in git. The context is never marked, so
// CommitTree's own helper is not among them.
func userHelpers(ctx context.Context, dir, rawURL string) []string {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "config", "-z", "--get-regexp", `^credential\..*helper$`)
	if err != nil {
		return nil // exit 1: none set
	}
	var list []string
	for _, entry := range strings.Split(out, "\x00") {
		key, value, _ := strings.Cut(entry, "\n")
		if key == "" {
			continue
		}
		if key != "credential.helper" {
			sub := strings.TrimSuffix(strings.TrimPrefix(key, "credential."), ".helper")
			if !urlMatches(sub, rawURL) {
				continue
			}
		}
		if strings.TrimSpace(value) == "" {
			list = nil
			continue
		}
		list = append(list, value)
	}
	return list
}

// urlMatches approximates git's credential.<url> matching: same scheme,
// host equal or matching a "*." wildcard label, path a prefix by segments,
// and a user only when the pattern names the same one (a request without a
// user never matches a pattern with one).
func urlMatches(pattern, rawURL string) bool {
	p, err := url.Parse(pattern)
	if err != nil || p.Scheme == "" || p.Host == "" {
		return false
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if !strings.EqualFold(p.Scheme, u.Scheme) {
		return false
	}
	if p.User != nil && (u.User == nil || p.User.Username() != u.User.Username()) {
		return false
	}
	if !hostMatches(strings.ToLower(p.Host), strings.ToLower(u.Host)) {
		return false
	}
	pp := strings.Trim(p.Path, "/")
	up := strings.Trim(u.Path, "/")
	return pp == "" || up == pp || strings.HasPrefix(up, pp+"/")
}

func hostMatches(pattern, host string) bool {
	pl, hl := strings.Split(pattern, "."), strings.Split(host, ".")
	if len(pl) != len(hl) {
		return false
	}
	for i := range pl {
		if pl[i] != "*" && pl[i] != hl[i] {
			return false
		}
	}
	return true
}

// DetectPlatformHelper is the helper "Remember" uses when the user has none:
// osxkeychain on macOS, libsecret on Linux, when git ships it.
func DetectPlatformHelper(ctx context.Context) string {
	out, err := gitcmd.Run(ctx, os.TempDir(), gitcmd.ReadTimeout, "--exec-path")
	if err != nil {
		return ""
	}
	name := map[string]string{"darwin": "osxkeychain", "linux": "libsecret"}[runtime.GOOS]
	if name == "" {
		return ""
	}
	if _, err := os.Stat(filepath.Join(strings.TrimSpace(out), "git-credential-"+name)); err != nil {
		return ""
	}
	return name
}

// approve keeps f in the platform helper only: the user's helpers are
// switched off for this one command by the empty credential.helper.
func approve(ctx context.Context, dir, platform string, f map[string]string) error {
	if dir == "" {
		dir = os.TempDir()
	}
	_, err := gitcmd.RunInput(ctx, dir, gitcmd.ReadTimeout, nil, FormatCredential(f),
		"-c", "credential.helper=", "-c", "credential.helper="+platform, "credential", "approve")
	return err
}

// reject removes f from the user's helpers — what git stored there on its
// own when "Remember" was unchecked.
func reject(ctx context.Context, dir string, f map[string]string) error {
	if dir == "" {
		dir = os.TempDir()
	}
	_, err := gitcmd.RunInput(ctx, dir, gitcmd.ReadTimeout, nil, FormatCredential(f), "credential", "reject")
	return err
}
