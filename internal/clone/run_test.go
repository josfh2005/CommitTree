package clone

import (
	"context"
	"errors"
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
