package ops_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"git-ui/internal/ops"
	"git-ui/internal/testrepo"
)

func TestRemotesListAddSetRemove(t *testing.T) {
	ctx := context.Background()
	r := testrepo.New(t)
	r.Commit("base")
	got, err := ops.ListRemotes(ctx, r.Dir)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty repo: %#v, %v", got, err)
	}
	if err := ops.AddRemote(ctx, r.Dir, " upstream ", " https://example.com/u.git "); err != nil {
		t.Fatal(err)
	}
	if err := ops.AddRemote(ctx, r.Dir, "origin", "https://example.com/o.git"); err != nil {
		t.Fatal(err)
	}
	got, _ = ops.ListRemotes(ctx, r.Dir)
	want := []ops.Remote{
		{Name: "origin", FetchURL: "https://example.com/o.git", PushURL: "https://example.com/o.git"},
		{Name: "upstream", FetchURL: "https://example.com/u.git", PushURL: "https://example.com/u.git"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	if err := ops.AddRemote(ctx, r.Dir, "origin", "x"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate add: %v", err)
	}
	// After "--" a leading dash is part of the name, never an option.
	if err := ops.AddRemote(ctx, r.Dir, "-x", "y"); err != nil {
		t.Fatal(err)
	}
	if u := r.Git("remote", "get-url", "--", "-x"); u != "y" {
		t.Fatalf("-x url = %q", u)
	}
	if err := ops.RemoveRemote(ctx, r.Dir, "-x"); err != nil {
		t.Fatal(err)
	}
	if err := ops.AddRemote(ctx, r.Dir, "", "y"); err == nil {
		t.Fatal("empty name must fail")
	}
	if err := ops.SetRemoteURL(ctx, r.Dir, "origin", "https://example.com/new.git"); err != nil {
		t.Fatal(err)
	}
	if u := r.Git("remote", "get-url", "origin"); u != "https://example.com/new.git" {
		t.Fatalf("url = %q", u)
	}
	if err := ops.SetRemoteURL(ctx, r.Dir, "origin", "  "); err == nil {
		t.Fatal("empty url must fail")
	}
	if err := ops.RemoveRemote(ctx, r.Dir, "upstream"); err != nil {
		t.Fatal(err)
	}
	got, _ = ops.ListRemotes(ctx, r.Dir)
	if len(got) != 1 || got[0].Name != "origin" {
		t.Fatalf("after remove: %+v", got)
	}
}

func TestRemoteTest(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	ctx := context.Background()
	r := testrepo.New(t)
	r.Commit("base")
	r.Git("remote", "add", "good", testrepo.NewBareFrom(t, r))
	r.Git("remote", "add", "locked", lockedServer(t))
	r.Git("remote", "add", "gone", filepath.Join(t.TempDir(), "missing.git"))

	for _, c := range []struct {
		name string
		ok   bool
		msg  string
	}{
		{"good", true, "Connected"},
		{"locked", false, "Authentication failed"},
	} {
		res, err := ops.TestRemote(ctx, r.Dir, c.name)
		if err != nil || res.OK != c.ok || res.Message != c.msg {
			t.Errorf("%s: %+v, %v", c.name, res, err)
		}
	}
	res, err := ops.TestRemote(ctx, r.Dir, "gone")
	if err != nil || res.OK || res.Message == "" || strings.Contains(res.Message, "\n") {
		t.Errorf("gone: %+v, %v", res, err)
	}
}
