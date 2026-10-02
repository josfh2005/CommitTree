package ops

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"git-ui/internal/gitcmd"
)

// RemoteTestTimeout bounds a connection test: long enough for a slow
// server, short enough that the Remotes tab never seems stuck.
const RemoteTestTimeout = 30 * time.Second

// Remote is one configured remote (docs/spec/12-repository-settings.md).
type Remote struct {
	Name     string `json:"name"`
	FetchURL string `json:"fetchURL"`
	PushURL  string `json:"pushURL"`
}

// RemoteTest is how a connection test ended: Message is "Connected", or
// why not, in one line.
type RemoteTest struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

var errEmptyRemoteField = errors.New("a remote needs a name and a URL")

// ListRemotes reads `git remote -v`, sorted by name; empty, not nil.
func ListRemotes(ctx context.Context, dir string) ([]Remote, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "remote", "-v")
	if err != nil {
		return nil, err
	}
	byName := map[string]*Remote{}
	for _, line := range strings.Split(out, "\n") {
		// "<name>\t<url> (fetch)" or "(push)"
		name, rest, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		i := strings.LastIndex(rest, " (")
		if i < 0 {
			continue
		}
		url, kind := rest[:i], strings.TrimSuffix(rest[i+2:], ")")
		r := byName[name]
		if r == nil {
			r = &Remote{Name: name}
			byName[name] = r
		}
		if kind == "push" {
			r.PushURL = url
		} else {
			r.FetchURL = url
		}
	}
	list := []Remote{}
	for _, r := range byName {
		list = append(list, *r)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list, nil
}

func AddRemote(ctx context.Context, dir, name, url string) error {
	name, url = strings.TrimSpace(name), strings.TrimSpace(url)
	if name == "" || url == "" {
		return errEmptyRemoteField
	}
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "remote", "add", "--", name, url)
	return err
}

func SetRemoteURL(ctx context.Context, dir, name, url string) error {
	name, url = strings.TrimSpace(name), strings.TrimSpace(url)
	if name == "" || url == "" {
		return errEmptyRemoteField
	}
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "remote", "set-url", "--", name, url)
	return err
}

// RemoveRemote removes a remote with its remote-tracking branches; branches
// that tracked it lose their upstream (git's own behaviour).
func RemoveRemote(ctx context.Context, dir, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errEmptyRemoteField
	}
	_, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "remote", "remove", "--", name)
	return err
}

// TestRemote asks the remote for its branches without ever prompting. A
// failure to reach it is a result, not an error.
func TestRemote(ctx context.Context, dir, name string) (RemoteTest, error) {
	_, err := gitcmd.RunEnv(ctx, dir, RemoteTestTimeout, NoPromptEnv, "ls-remote", "--heads", "--", strings.TrimSpace(name))
	switch {
	case err == nil:
		return RemoteTest{OK: true, Message: "Connected"}, nil
	case IsAuthError(err):
		return RemoteTest{Message: "Authentication failed"}, nil
	case errors.Is(err, gitcmd.ErrTimeout):
		return RemoteTest{Message: "Timed out"}, nil
	}
	var gerr *gitcmd.Error
	if !errors.As(err, &gerr) {
		return RemoteTest{}, err
	}
	return RemoteTest{Message: firstNonEmptyLine(gerr.Stderr, err.Error())}, nil
}

func firstNonEmptyLine(texts ...string) string {
	for _, t := range texts {
		for _, l := range strings.Split(t, "\n") {
			if l = strings.TrimSpace(l); l != "" {
				return l
			}
		}
	}
	return ""
}
