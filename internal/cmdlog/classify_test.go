package cmdlog

import (
	"context"
	"strings"
	"testing"
)

func TestClassify(t *testing.T) {
	reads := []string{
		"log --format=%H -n 10", "show HEAD", "status --porcelain=v2", "diff --cached",
		"rev-parse HEAD", "rev-list --count HEAD", "for-each-ref refs/heads", "show-ref",
		"cat-file -t HEAD", "blame -p f.txt", "ls-files -u", "ls-tree HEAD", "merge-base a b",
		"name-rev HEAD", "describe --tags", "diff-tree -r HEAD", "diff-index HEAD", "diff-files",
		"check-ignore x", "check-attr -a x", "var GIT_EDITOR", "version", "shortlog -s", "grep foo",
		"symbolic-ref HEAD", "symbolic-ref --short HEAD", "worktree list --porcelain",
		"submodule status", "submodule", "stash list", "stash show -p stash@{0}",
		"config --get user.name", "config --get-all remote.origin.url", "config --list",
		"config -l", "config --get-regexp ^remote", "config get user.name",
		"branch", "branch --list", "branch -a --format=%(refname)", "tag", "tag -l v*",
		"-c core.quotepath=false status",
		"reflog", "reflog show HEAD",
		"remote", "remote -v", "remote get-url origin",
		"check-ref-format --branch x",
	}
	writes := []string{
		"commit -m x", "push", "fetch --all", "pull --rebase", "checkout main", "switch -c b",
		"merge feature", "rebase main", "cherry-pick abc", "reset --hard", "add f", "restore --staged f",
		"stash push -m x", "stash pop", "stash drop", "branch new", "branch -d old", "branch -m a b",
		"branch --set-upstream-to=origin/main", "tag v1", "tag -d v1", "symbolic-ref HEAD refs/heads/x",
		"config user.name Bob", "config set user.name Bob", "worktree add ../x", "worktree remove x",
		"submodule update --init", "reflog expire --all", "ls-remote origin", "clone url", "",
		"remote add o url", "remote remove o", "remote set-url o url",
	}
	for _, c := range reads {
		if got := Classify(strings.Fields(c)); got != KindRead {
			t.Errorf("%q: got %s, want read", c, got)
		}
	}
	for _, c := range writes {
		if got := Classify(strings.Fields(c)); got != KindWrite {
			t.Errorf("%q: got %s, want write", c, got)
		}
	}
}

func TestOriginFromContext(t *testing.T) {
	if _, ok := OriginFrom(context.Background()); ok {
		t.Fatal("plain ctx has no origin")
	}
	if _, ok := OriginFrom(nil); ok { //nolint:staticcheck // nil ctx must be safe
		t.Fatal("nil ctx has no origin")
	}
	o, ok := OriginFrom(WithOrigin(context.Background(), OriginAI))
	if !ok || o != OriginAI {
		t.Fatalf("got %q %v", o, ok)
	}
}

func TestUpdatesFromRemote(t *testing.T) {
	for _, c := range []struct {
		args []string
		want bool
	}{
		{[]string{"fetch", "--all", "--prune"}, true},
		{[]string{"fetch", "--prune", "origin"}, true},
		{[]string{"-c", "x=y", "pull", "--"}, true},
		{[]string{"fetch", ".", "origin/main:refs/heads/main"}, false},
		{[]string{"push"}, false},
		{[]string{"status"}, false},
	} {
		if got := UpdatesFromRemote(c.args); got != c.want {
			t.Errorf("UpdatesFromRemote(%v) = %v, want %v", c.args, got, c.want)
		}
	}
}
