//go:build !windows

package credprompt

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"git-ui/internal/clone"
	"git-ui/internal/gitcmd"
	"git-ui/internal/testrepo"
)

// newServerWith starts a server whose UI records requests and calls onShow
// for each (nil: nobody answers). The server reaches onShow through an
// atomic pointer: the UI runs on the server's own goroutine.
func newServerWith(t *testing.T, o Options, onShow func(s *Server, r Request)) (*Server, *fakeUI) {
	t.Helper()
	if o.Exe == "" {
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		o.Exe = exe
	}
	if o.Timeout == 0 {
		o.Timeout = 10 * time.Second
	}
	ui := newFakeUI()
	var sp atomic.Pointer[Server]
	u := ui.ui()
	show := u.Show
	u.Show = func(r Request) {
		show(r)
		if onShow != nil {
			onShow(sp.Load(), r)
		}
	}
	s, err := NewServer(o, u)
	if err != nil {
		t.Fatal(err)
	}
	sp.Store(s)
	t.Cleanup(s.Shutdown)
	return s, ui
}

// answering answers the request on screen (not re-sends) with a.
func answering(t *testing.T, o Options, a Answer) (*Server, *fakeUI) {
	return newServerWith(t, o, func(s *Server, r Request) {
		if r.Waiting == 0 {
			go s.Answer(r.ID, a)
		}
	})
}

// remote is a bare repository behind basic auth.
type remote struct {
	bare, url string
	authed    *atomic.Int32
}

// pushedHash is the commit refs/heads/pushed points to in the bare
// repository, "" when there is no such branch.
func (rm remote) pushedHash(t *testing.T) string {
	t.Helper()
	out, err := gitcmd.Run(context.Background(), rm.bare, gitcmd.ReadTimeout, "rev-parse", "--verify", "-q", "refs/heads/pushed")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// pushSetup is a repository with one commit and an "origin" behind basic auth.
func pushSetup(t *testing.T) (*testrepo.Repo, remote) {
	t.Helper()
	hermetic(t)
	r := testrepo.New(t)
	r.Commit("base")
	bare := testrepo.NewBareFrom(t, r)
	url, authed := testrepo.AuthServer(t, bare, "me", "tok")
	r.Git("remote", "add", "origin", url)
	return r, remote{bare: bare, url: url, authed: authed}
}

func push(ctx context.Context, r *testrepo.Repo) error {
	_, err := gitcmd.Run(ctx, r.Dir, gitcmd.NetworkTimeout, "push", "origin", "HEAD:refs/heads/pushed")
	return err
}

func TestPushAsksAndSucceeds(t *testing.T) {
	r, rm := pushSetup(t)
	s, ui := answering(t, Options{}, Answer{Username: "me", Secret: "tok"})
	if err := push(s.Allow(context.Background(), Info{Dir: r.Dir, Context: "Push · r"}), r); err != nil {
		t.Fatal(err)
	}
	shown := ui.shown()
	if len(shown) != 1 || shown[0].Kind != KindHTTPS || !strings.HasPrefix(shown[0].Host, "127.0.0.1:") {
		t.Fatalf("shown %+v", shown)
	}
	if got, want := rm.pushedHash(t), r.Git("rev-parse", "HEAD"); got != want {
		t.Fatalf("remote has %q, want %q", got, want)
	}
	if rm.authed.Load() == 0 {
		t.Fatal("the server never saw the right credentials")
	}
}

func TestFetchAsksAndSucceeds(t *testing.T) {
	r, rm := pushSetup(t)
	// A second commit lands on the remote through a clone that has the token
	// in its URL, so the repository under test has something new to fetch.
	other := testrepo.Clone(t, strings.Replace(rm.url, "http://", "http://me:tok@", 1))
	want := other.Commit("upstream")
	other.Git("push", "origin", "HEAD:refs/heads/main")
	rm.authed.Store(0)

	s, ui := answering(t, Options{}, Answer{Username: "me", Secret: "tok"})
	if _, err := gitcmd.Run(s.Allow(context.Background(), Info{Dir: r.Dir}), r.Dir, gitcmd.NetworkTimeout, "fetch", "origin"); err != nil {
		t.Fatal(err)
	}
	if n := len(ui.shown()); n != 1 {
		t.Fatalf("shown %d dialogs, want 1", n)
	}
	if got := r.Git("rev-parse", "refs/remotes/origin/main"); got != want {
		t.Fatalf("origin/main is %q, want %q", got, want)
	}
	if rm.authed.Load() == 0 {
		t.Fatal("the server never saw the right credentials")
	}
}

func TestCloneAsksAndSucceeds(t *testing.T) {
	hermetic(t)
	src := testrepo.New(t)
	want := src.Commit("base")
	bare := testrepo.NewBareFrom(t, src)
	url, authed := testrepo.AuthServer(t, bare, "me", "tok")
	file := filepath.Join(t.TempDir(), "platform")
	s, ui := answering(t, Options{Platform: "store --file " + file}, Answer{Username: "me", Secret: "tok", Remember: true})

	dest := filepath.Join(t.TempDir(), "cloned")
	ctx := s.Allow(context.Background(), Info{Context: "Clone · remote.git"})
	if err := clone.Run(ctx, url, dest, time.Minute, func(clone.Progress) {}); err != nil {
		t.Fatal(err)
	}
	if n := len(ui.shown()); n != 1 {
		t.Fatalf("shown %d dialogs, want 1", n)
	}
	got, err := gitcmd.Run(context.Background(), dest, gitcmd.ReadTimeout, "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(got) != want {
		t.Fatalf("clone HEAD %q (%v), want %q", got, err, want)
	}
	if _, err := os.Stat(filepath.Join(dest, "file-1.txt")); err != nil {
		t.Fatalf("the work tree lacks the committed file: %v", err)
	}
	if authed.Load() == 0 {
		t.Fatal("the server never saw the right credentials")
	}
	// A clone has no repository yet: Remember goes to the platform helper.
	if b, _ := os.ReadFile(file); !strings.Contains(string(b), "me:tok@") {
		t.Fatalf("platform helper got %q", b)
	}
}

func TestRememberUncheckedUndoesUserHelper(t *testing.T) {
	r, rm := pushSetup(t)
	file := filepath.Join(t.TempDir(), "creds")
	r.Git("config", "credential.helper", "store --file "+file)
	s, ui := answering(t, Options{}, Answer{Username: "me", Secret: "tok", Remember: false})
	if err := push(s.Allow(context.Background(), Info{Dir: r.Dir}), r); err != nil {
		t.Fatal(err)
	}
	if rm.pushedHash(t) == "" {
		t.Fatal("the push did not arrive")
	}
	if rem := ui.shown()[0].Remember; rem == nil || rem.Name != "store" {
		t.Fatalf("remember %+v", rem)
	}
	if b, _ := os.ReadFile(file); strings.Contains(string(b), "tok") {
		t.Fatalf("still stored: %q", b)
	}
}

func TestRememberCheckedKeepsUserHelper(t *testing.T) {
	r, rm := pushSetup(t)
	file := filepath.Join(t.TempDir(), "creds")
	r.Git("config", "credential.helper", "store --file "+file)
	s, _ := answering(t, Options{}, Answer{Username: "me", Secret: "tok", Remember: true})
	if err := push(s.Allow(context.Background(), Info{Dir: r.Dir}), r); err != nil {
		t.Fatal(err)
	}
	if rm.pushedHash(t) == "" {
		t.Fatal("the push did not arrive")
	}
	if b, _ := os.ReadFile(file); !strings.Contains(string(b), "me:tok@") {
		t.Fatalf("not stored: %q", b)
	}
}

func TestRememberCheckedWithoutHelperApprovesIntoPlatform(t *testing.T) {
	r, rm := pushSetup(t)
	file := filepath.Join(t.TempDir(), "platform")
	s, _ := answering(t, Options{Platform: "store --file " + file}, Answer{Username: "me", Secret: "tok", Remember: true})
	if err := push(s.Allow(context.Background(), Info{Dir: r.Dir}), r); err != nil {
		t.Fatal(err)
	}
	if rm.pushedHash(t) == "" {
		t.Fatal("the push did not arrive")
	}
	if b, _ := os.ReadFile(file); !strings.Contains(string(b), "me:tok@") {
		t.Fatalf("platform helper got %q", b)
	}
}

// rejectedPush runs a push that git refuses after the server accepted the
// credential: the remote branch has a commit the local one lacks.
func rejectedPush(t *testing.T, ctx context.Context, r *testrepo.Repo, rm remote) error {
	t.Helper()
	branch := r.Git("rev-parse", "--abbrev-ref", "HEAD")
	other := testrepo.Clone(t, rm.bare)
	other.Commit("ahead")
	other.Git("push", "origin", "HEAD:refs/heads/"+branch)
	r.Commit("local")
	_, err := gitcmd.Run(ctx, r.Dir, gitcmd.NetworkTimeout, "push", "origin", "HEAD:refs/heads/"+branch)
	if err == nil {
		t.Fatal("the push should have been rejected")
	}
	return err
}

func TestRememberUncheckedUndoesUserHelperWhenCommandFails(t *testing.T) {
	r, rm := pushSetup(t)
	file := filepath.Join(t.TempDir(), "creds")
	r.Git("config", "credential.helper", "store --file "+file)
	s, _ := answering(t, Options{}, Answer{Username: "me", Secret: "tok", Remember: false})
	rejectedPush(t, s.Allow(context.Background(), Info{Dir: r.Dir}), r, rm)
	if rm.authed.Load() == 0 {
		t.Fatal("the server never accepted the credential")
	}
	if b, _ := os.ReadFile(file); strings.Contains(string(b), "tok") {
		t.Fatalf("still stored after a failed command: %q", b)
	}
}

func TestRememberCheckedApprovesPlatformWhenCommandFails(t *testing.T) {
	r, rm := pushSetup(t)
	file := filepath.Join(t.TempDir(), "platform")
	s, _ := answering(t, Options{Platform: "store --file " + file}, Answer{Username: "me", Secret: "tok", Remember: true})
	rejectedPush(t, s.Allow(context.Background(), Info{Dir: r.Dir}), r, rm)
	if b, _ := os.ReadFile(file); !strings.Contains(string(b), "me:tok@") {
		t.Fatalf("platform helper got %q", b)
	}
}

func TestWrongTokenMessage(t *testing.T) {
	r, rm := pushSetup(t)
	s, ui := answering(t, Options{}, Answer{Username: "me", Secret: "nope"})
	err := push(s.Allow(context.Background(), Info{Dir: r.Dir}), r)
	if err == nil || err.Error() != "The username or token was not accepted." {
		t.Fatalf("got %v", err)
	}
	if n := len(ui.shown()); n != 1 {
		t.Fatalf("shown %d dialogs, want 1", n)
	}
	if rm.pushedHash(t) != "" {
		t.Fatal("a push with a wrong token arrived")
	}
}

func TestStaleSavedCredential(t *testing.T) {
	r, rm := pushSetup(t)
	file := filepath.Join(t.TempDir(), "creds")
	r.Git("config", "credential.helper", "store --file "+file)
	url := r.Git("remote", "get-url", "origin")
	stale := strings.Replace(url, "http://", "http://me:old@", 1)
	if err := os.WriteFile(file, []byte(strings.TrimSuffix(stale, "/"+filepath.Base(url))+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, ui := answering(t, Options{}, Answer{Username: "me", Secret: "tok", Remember: true})
	err := push(s.Allow(context.Background(), Info{Dir: r.Dir}), r)
	if err == nil || !strings.HasPrefix(err.Error(), "The saved credential was not accepted") {
		t.Fatalf("first attempt: %v", err)
	}
	if n := len(ui.shown()); n != 0 {
		t.Fatalf("the stale credential should have been used without asking, shown %d", n)
	}
	if err := push(s.Allow(context.Background(), Info{Dir: r.Dir}), r); err != nil {
		t.Fatalf("second attempt: %v", err)
	}
	if n := len(ui.shown()); n != 1 {
		t.Fatalf("second attempt should ask once, shown %d", n)
	}
	if rm.pushedHash(t) == "" {
		t.Fatal("the second push did not arrive")
	}
}

func TestCancelShowsOneDialog(t *testing.T) {
	r, rm := pushSetup(t)
	s, ui := newServerWith(t, Options{}, func(s *Server, req Request) { go s.Cancel(req.ID) })
	err := push(s.Allow(context.Background(), Info{Dir: r.Dir}), r)
	if err == nil || err.Error() != "No credentials entered." {
		t.Fatalf("got %v", err)
	}
	if n := len(ui.shown()); n != 1 {
		t.Fatalf("shown %d dialogs, want 1", n)
	}
	if rm.pushedHash(t) != "" {
		t.Fatal("a cancelled push arrived")
	}
}

func TestDialogTimeout(t *testing.T) {
	r, rm := pushSetup(t)
	s, ui := newServerWith(t, Options{Timeout: 200 * time.Millisecond}, nil)
	err := push(s.Allow(context.Background(), Info{Dir: r.Dir}), r)
	if err == nil || err.Error() != "No credentials entered (timed out)." {
		t.Fatalf("got %v", err)
	}
	if n := len(ui.shown()); n != 1 {
		t.Fatalf("shown %d dialogs, want 1", n)
	}
	if rm.pushedHash(t) != "" {
		t.Fatal("a timed-out push arrived")
	}
}

func TestUnmarkedCommandNeverAsks(t *testing.T) {
	r, rm := pushSetup(t)
	_, ui := answering(t, Options{}, Answer{Username: "me", Secret: "tok"})
	if err := push(context.Background(), r); err == nil {
		t.Fatal("an unmarked push to a locked remote succeeded")
	}
	if n := len(ui.shown()); n != 0 {
		t.Fatal("an unmarked command asked")
	}
	if rm.pushedHash(t) != "" {
		t.Fatal("an unmarked push arrived")
	}
}

func TestHelperPathWithSpaceAndQuote(t *testing.T) {
	r, rm := pushSetup(t)
	exe, _ := os.Executable()
	dir := filepath.Join(t.TempDir(), "My Apps", "Commit'Tree")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "CommitTree")
	if err := os.Symlink(exe, link); err != nil {
		t.Fatal(err)
	}
	s, ui := answering(t, Options{Exe: link}, Answer{Username: "me", Secret: "tok"})
	if err := push(s.Allow(context.Background(), Info{Dir: r.Dir}), r); err != nil {
		t.Fatal(err)
	}
	if n := len(ui.shown()); n != 1 {
		t.Fatalf("shown %d dialogs, want 1", n)
	}
	if rm.pushedHash(t) == "" {
		t.Fatal("the push did not arrive")
	}
}

func TestFakeSSHAsksPassphraseThenHostKey(t *testing.T) {
	hermetic(t)
	r := testrepo.New(t)
	marker := filepath.Join(t.TempDir(), "ok")
	script := filepath.Join(t.TempDir(), "fake-ssh")
	body := `#!/bin/sh
pp=$("$SSH_ASKPASS" "Enter passphrase for key '/k/id_test': ") || exit 255
[ "$pp" = "secret-pp" ] || exit 255
hk=$("$SSH_ASKPASS" "The authenticity of host 'example.com (1.2.3.4)' can't be established.
ED25519 key fingerprint is SHA256:fake.
Are you sure you want to continue connecting (yes/no/[fingerprint])? ") || exit 255
[ "$hk" = "yes" ] || exit 255
echo ok > "` + marker + `"
exit 255
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	s, ui := newServerWith(t, Options{}, func(s *Server, req Request) {
		a := Answer{Secret: "secret-pp"}
		if req.Kind == KindHostKey {
			a = Answer{Trust: true}
		}
		go s.Answer(req.ID, a)
	})
	ctx := s.Allow(context.Background(), Info{Dir: r.Dir})
	// A script git cannot name gets probed with "-G" first, which would run it
	// (and ask) a second time: tell git it speaks OpenSSH's options.
	env := []string{"GIT_SSH_COMMAND=" + script, "GIT_SSH_VARIANT=ssh"}
	_, _ = gitcmd.RunEnv(ctx, r.Dir, gitcmd.NetworkTimeout, env, "ls-remote", "ssh://example.com/repo.git")
	if b, _ := os.ReadFile(marker); strings.TrimSpace(string(b)) != "ok" {
		t.Fatal("fake ssh did not get both answers")
	}
	shown := ui.shown()
	if len(shown) != 2 || shown[0].Kind != KindPassphrase || shown[1].Kind != KindHostKey || shown[1].Fingerprint != "SHA256:fake" {
		t.Fatalf("shown %+v", shown)
	}
}
