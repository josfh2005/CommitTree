package clone

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git-ui/internal/gitcmd"
	"git-ui/internal/testrepo"
)

func bareRepo(t *testing.T) string {
	t.Helper()
	r := testrepo.New(t)
	r.Commit("base")
	return testrepo.NewBareFrom(t, r)
}

func TestRunClones(t *testing.T) {
	bare := bareRepo(t)
	dest := filepath.Join(t.TempDir(), "copy")
	var phases []string
	err := Run(context.Background(), "file://"+bare, dest, Stall, func(p Progress) { phases = append(phases, p.Phase) })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, ".git")); err != nil {
		t.Fatalf("no .git in dest: %v", err)
	}
	if len(phases) == 0 || phases[0] != "Cloning into 'copy'" {
		t.Fatalf("phases = %q", phases)
	}
}

func TestRunWithSubmodule(t *testing.T) {
	sub := testrepo.New(t)
	sub.Commit("sub base")
	subBare := testrepo.NewBareFrom(t, sub)
	top := testrepo.New(t)
	top.Commit("base")
	top.Git("-c", "protocol.file.allow=always", "submodule", "add", "-q", "file://"+subBare, "lib")
	top.Git("commit", "-q", "-m", "add lib")
	bare := testrepo.NewBareFrom(t, top)
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "protocol.file.allow")
	t.Setenv("GIT_CONFIG_VALUE_0", "always")

	dest := filepath.Join(t.TempDir(), "top")
	if err := Run(context.Background(), "file://"+bare, dest, Stall, func(Progress) {}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "lib", ".git")); err != nil {
		t.Fatalf("submodule not cloned: %v", err)
	}
}

func TestRunKeepsAClonePartiallyDoneWhenASubmoduleFails(t *testing.T) {
	sub := testrepo.New(t)
	sub.Commit("sub base")
	subBare := testrepo.NewBareFrom(t, sub)
	top := testrepo.New(t)
	top.Commit("base")
	top.Git("-c", "protocol.file.allow=always", "submodule", "add", "-q", "file://"+subBare, "lib")
	top.Git("commit", "-q", "-m", "add lib")
	bare := testrepo.NewBareFrom(t, top)
	if err := os.RemoveAll(subBare); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "protocol.file.allow")
	t.Setenv("GIT_CONFIG_VALUE_0", "always")

	url := "file://" + bare
	dest := filepath.Join(t.TempDir(), "top")
	err := Run(context.Background(), url, dest, Stall, func(Progress) {})
	if !errors.Is(err, ErrPartial) {
		t.Fatalf("err = %v, want ErrPartial", err)
	}
	var gerr *gitcmd.Error
	if !errors.As(err, &gerr) {
		t.Fatalf("err = %v, git's error is not reachable", err)
	}
	if _, statErr := os.Stat(filepath.Join(dest, ".git")); statErr != nil {
		t.Fatalf("the cloned repository was removed: %v", statErr)
	}
	msg := Explain(err, url)
	if strings.Contains(msg, "Repository not found at "+url) {
		t.Fatalf("Explain blames the top-level URL: %q", msg)
	}
	if !strings.HasPrefix(msg, "Cloned, but some submodules or files could not be checked out: ") {
		t.Fatalf("Explain = %q", msg)
	}
}

func TestRunKeepsNoPartialCloneOnCancelOrStall(t *testing.T) {
	// A cancel or a stall is not a partial outcome: the folder is removed
	// (TestRunCancelLeavesNoFolder, and the stall here).
	t.Setenv("GIT_SSH_COMMAND", "sleep 30;:")
	dest := filepath.Join(t.TempDir(), "x")
	err := Run(context.Background(), "ssh://example.invalid/x.git", dest, 300*time.Millisecond, func(Progress) {})
	if errors.Is(err, ErrPartial) {
		t.Fatalf("a stall is not partial: %v", err)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("dest left behind: %v", statErr)
	}
}

func TestRunIntoEmptyFolder(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "empty")
	os.Mkdir(dest, 0o755)
	if err := Run(context.Background(), "file://"+bareRepo(t), dest, Stall, func(Progress) {}); err != nil {
		t.Fatal(err)
	}
}

func TestRunFailureKeepsPreexistingEmptyFolder(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "empty")
	os.Mkdir(dest, 0o755)
	err := Run(context.Background(), filepath.Join(t.TempDir(), "nope"), dest, Stall, func(Progress) {})
	if err == nil {
		t.Fatal("want an error")
	}
	if info, statErr := os.Stat(dest); statErr != nil || !info.IsDir() {
		t.Fatalf("pre-existing folder removed: %v", statErr)
	}
}

func TestRunFailureLeavesNoFolder(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	dest := filepath.Join(t.TempDir(), "x")
	err := Run(context.Background(), missing, dest, Stall, func(Progress) {})
	if err == nil {
		t.Fatal("want an error")
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("dest left behind: %v", statErr)
	}
	if msg := Explain(err, missing); msg != "Repository not found at "+missing+"." {
		t.Fatalf("Explain = %q", msg)
	}
}

func TestRunCancelLeavesNoFolder(t *testing.T) {
	t.Setenv("GIT_SSH_COMMAND", "sleep 30;:")
	dest := filepath.Join(t.TempDir(), "x")
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(300 * time.Millisecond); cancel() }()
	err := Run(ctx, "ssh://example.invalid/x.git", dest, Stall, func(Progress) {})
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("err = %v, want ErrCancelled", err)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("dest left behind: %v", statErr)
	}
}

func TestRunStall(t *testing.T) {
	t.Setenv("GIT_SSH_COMMAND", "sleep 30;:")
	err := Run(context.Background(), "ssh://example.invalid/x.git", filepath.Join(t.TempDir(), "x"), 300*time.Millisecond, func(Progress) {})
	if !errors.Is(err, gitcmd.ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
	if msg := Explain(err, "ssh://example.invalid/x.git"); msg != "Clone stalled: git sent nothing for 5 minutes." {
		t.Fatalf("Explain = %q", msg)
	}
}

func TestRunRefusesBadURL(t *testing.T) {
	for _, url := range []string{"", "  ", "--upload-pack=touch /tmp/pwned", "-x"} {
		if err := Run(context.Background(), url, filepath.Join(t.TempDir(), "x"), Stall, func(Progress) {}); !errors.Is(err, ErrBadURL) {
			t.Errorf("Run(%q) err = %v, want ErrBadURL", url, err)
		}
	}
}

func gitErr(stderr string) error {
	return &gitcmd.Error{Args: []string{"clone"}, Stderr: stderr, ExitCode: 128, Err: errors.New("exit status 128")}
}

func TestExplain(t *testing.T) {
	cases := []struct{ stderr, url, want string }{
		{"Cloning into 'x'...\nfatal: could not read Username for 'https://github.com': terminal prompts disabled\n",
			"https://github.com/o/x.git",
			"Authentication failed. Set up a credential helper or an SSH key for this host, then try again."},
		{"git@github.com: Permission denied (publickey).\nfatal: Could not read from remote repository.\n",
			"git@github.com:o/x.git",
			"Authentication failed. Set up a credential helper or an SSH key for this host, then try again."},
		{"Host key verification failed.\nfatal: Could not read from remote repository.\n",
			"git@github.com:o/x.git",
			"The host's SSH key isn't trusted yet. Connect once from a terminal (ssh -T git@github.com) to accept it."},
		{"Host key verification failed.\n",
			"ssh://git@example.com:2222/o/x.git",
			"The host's SSH key isn't trusted yet. Connect once from a terminal (ssh -T git@example.com) to accept it."},
		{"Cloning into 'x'...\nremote: Counting objects: 1% (1/9)\rfatal: unable to access 'https://h/x.git/': Could not resolve host: h\n",
			"https://h/x.git",
			"fatal: unable to access 'https://h/x.git/': Could not resolve host: h"},
	}
	for _, c := range cases {
		if got := Explain(gitErr(c.stderr), c.url); got != c.want {
			t.Errorf("Explain(%q) = %q, want %q", c.stderr, got, c.want)
		}
	}
}

func TestExplainHidesCredentials(t *testing.T) {
	url := "https://user:tok3n@h.invalid/x.git"
	msgs := []string{
		Explain(gitErr("fatal: unable to access 'https://user:tok3n@h.invalid/x.git/': Could not resolve host: h.invalid\n"), url),
		Explain(gitErr("fatal: repository 'https://user:tok3n@h.invalid/x.git/' not found\n"), url),
	}
	for _, m := range msgs {
		if strings.Contains(m, "tok3n") {
			t.Fatalf("token leaked: %q", m)
		}
	}
}

func TestRunDoesNotRemoveADanglingSymlink(t *testing.T) {
	parent := t.TempDir()
	dest := filepath.Join(parent, "link")
	if err := os.Symlink(filepath.Join(parent, "gone"), dest); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), filepath.Join(parent, "nope"), dest, Stall, func(Progress) {}); err == nil {
		t.Fatal("want an error")
	}
	if _, err := os.Lstat(dest); err != nil {
		t.Fatalf("the symlink was removed: %v", err)
	}
}

func TestExplainNamesNotTheTopLevelURLForAnotherRepository(t *testing.T) {
	stderr := "Cloning into '/x/top/lib'...\nfatal: repository 'https://user:tok3n@h.invalid/lib.git/' does not exist\nfatal: clone of 'https://user:tok3n@h.invalid/lib.git' into submodule path '/x/top/lib' failed\n"
	msg := Explain(gitErr(stderr), "https://h.invalid/top.git")
	if strings.Contains(msg, "Repository not found at") || strings.Contains(msg, "tok3n") {
		t.Fatalf("Explain = %q", msg)
	}
	if !strings.Contains(msg, "lib.git") {
		t.Fatalf("Explain = %q, want git's own message", msg)
	}
	// git over ssh or file:// quotes just the path.
	if msg := Explain(gitErr("fatal: '/o/top.git' does not appear to be a git repository\n"), "git@h.invalid:o/top.git"); msg != "Repository not found at git@h.invalid:o/top.git." {
		t.Fatalf("Explain = %q", msg)
	}
	// The top-level repository still gets the short message.
	if msg := Explain(gitErr("fatal: repository 'https://h.invalid/top.git/' not found\n"), "https://h.invalid/top.git"); msg != "Repository not found at https://h.invalid/top.git." {
		t.Fatalf("Explain = %q", msg)
	}
}

func TestExplainPartialHidesCredentials(t *testing.T) {
	err := fmt.Errorf("%w: %w", ErrPartial, gitErr("fatal: unable to access 'https://user:tok3n@h.invalid/lib.git/': Could not resolve host: h.invalid\n"))
	msg := Explain(err, "https://user:tok3n@h.invalid/top.git")
	if strings.Contains(msg, "tok3n") || !strings.HasPrefix(msg, "Cloned, but ") {
		t.Fatalf("Explain = %q", msg)
	}
}

func TestSSHHostNeverEchoesCredentials(t *testing.T) {
	if got := sshHost("https://user:tok3n@h.invalid/x.git"); strings.Contains(got, "tok3n") {
		t.Fatalf("sshHost = %q", got)
	}
}
