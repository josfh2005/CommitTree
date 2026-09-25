package app

import (
	"os/exec"
	"testing"
)

func TestGetIdentity(t *testing.T) {
	// Only the repository's own config counts in this test, not the
	// machine's global one.
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	a, id := newTestApp(t)

	got, err := a.GetIdentity(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Test User" || got.Email != "test@example.com" {
		t.Fatalf("identity = %+v", got)
	}

	dir, err := a.dir(id)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", dir, "config", "--unset", "user.email").CombinedOutput(); err != nil {
		t.Fatalf("unset email: %v\n%s", err, out)
	}
	got, err = a.GetIdentity(id)
	if err != nil {
		t.Fatalf("an unset email is not an error: %v", err)
	}
	if got.Name != "Test User" || got.Email != "" {
		t.Fatalf("identity without email = %+v", got)
	}

	if _, err := a.GetIdentity("nope"); err == nil {
		t.Fatal("want an error for an unknown repository")
	}
}
