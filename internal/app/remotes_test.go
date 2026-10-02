package app

import (
	"testing"

	"git-ui/internal/cmdlog"
	"git-ui/internal/testrepo"
)

func TestRemotesThroughTheAppLayer(t *testing.T) {
	a, r, id := newPlainApp(t)
	bare := testrepo.NewBareFrom(t, r)
	if err := a.AddRemote(id, "origin", bare); err != nil {
		t.Fatal(err)
	}
	if err := a.AddRemote(id, "backup", bare); err != nil {
		t.Fatal(err)
	}
	list, err := a.ListRemotes(id)
	if err != nil || len(list) != 2 || list[0].Name != "backup" {
		t.Fatalf("list = %+v, %v", list, err)
	}
	res, err := a.TestRemote(id, "origin")
	if err != nil || !res.OK {
		t.Fatalf("test = %+v, %v", res, err)
	}

	dir, _ := a.dir(id)
	key := cmdlog.RepoKey(dir)
	a.paused.pause(key, []string{"origin", "backup"})
	if err := a.SetRemoteURL(id, "origin", bare); err != nil {
		t.Fatal(err)
	}
	if a.paused.paused(key, "origin") || !a.paused.paused(key, "backup") {
		t.Fatal("set-url must clear only that remote's pause")
	}
	if err := a.RemoveRemote(id, "backup"); err != nil {
		t.Fatal(err)
	}
	if a.paused.paused(key, "backup") {
		t.Fatal("remove must clear the remote's pause")
	}
	if list, _ = a.ListRemotes(id); len(list) != 1 {
		t.Fatalf("after remove: %+v", list)
	}
	if _, err := a.ListRemotes("no-such-id"); err == nil {
		t.Error("want an error for an unknown repository")
	}
}
