package credprompt

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git-ui/internal/testrepo"
)

func hermetic(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("HOME", t.TempDir())
}

func TestFindRememberUserHelper(t *testing.T) {
	hermetic(t)
	r := testrepo.New(t)
	r.Git("config", "credential.helper", "store --file "+filepath.Join(t.TempDir(), "c"))
	got := findRemember(context.Background(), r.Dir, "https://h.test/x", "osxkeychain")
	if got == nil || got.Name != "store" || !got.User {
		t.Fatalf("got %+v", got)
	}
}

func TestFindRememberEmptyValueResets(t *testing.T) {
	hermetic(t)
	r := testrepo.New(t)
	r.Git("config", "--add", "credential.helper", "store")
	r.Git("config", "--add", "credential.helper", "")
	got := findRemember(context.Background(), r.Dir, "https://h.test/x", "osxkeychain")
	if got == nil || got.Name != "osxkeychain" || got.User {
		t.Fatalf("want platform fallback, got %+v", got)
	}
}

func TestFindRememberURLScoped(t *testing.T) {
	hermetic(t)
	r := testrepo.New(t)
	r.Git("config", "credential.https://other.test.helper", "cache")
	r.Git("config", "credential.https://h.test.helper", "store")
	got := findRemember(context.Background(), r.Dir, "https://h.test/org/repo.git", "")
	if got == nil || got.Name != "store" {
		t.Fatalf("got %+v", got)
	}
}

func TestFindRememberNone(t *testing.T) {
	hermetic(t)
	r := testrepo.New(t)
	if got := findRemember(context.Background(), r.Dir, "https://h.test", ""); got != nil {
		t.Fatalf("got %+v", got)
	}
}

func TestFindRememberOutsideARepo(t *testing.T) {
	hermetic(t)
	if got := findRemember(context.Background(), "", "https://h.test", "osxkeychain"); got == nil || got.User {
		t.Fatalf("got %+v", got)
	}
}

func TestURLMatches(t *testing.T) {
	cases := []struct {
		pattern, url string
		want         bool
	}{
		{"https://github.com", "https://github.com/org/r.git", true},
		{"https://github.com/org", "https://github.com/org/r.git", true},
		{"https://github.com/other", "https://github.com/org/r.git", false},
		{"http://github.com", "https://github.com/org", false},
		{"https://*.example.com", "https://git.example.com/x", true},
		{"https://*.example.com", "https://example.com/x", false},
		{"https://me@github.com", "https://github.com/x", false},
		{"not a url", "https://github.com", false},
	}
	for _, c := range cases {
		if got := urlMatches(c.pattern, c.url); got != c.want {
			t.Errorf("urlMatches(%q, %q) = %v", c.pattern, c.url, got)
		}
	}
}

func TestApproveAndReject(t *testing.T) {
	hermetic(t)
	r := testrepo.New(t)
	file := filepath.Join(t.TempDir(), "creds")
	f := map[string]string{"protocol": "https", "host": "h.test", "username": "me", "password": "tok"}
	if err := approve(context.Background(), r.Dir, "store --file "+file, f); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(file)
	if !strings.Contains(string(b), "https://me:tok@h.test") {
		t.Fatalf("approve wrote %q", b)
	}
	r.Git("config", "credential.helper", "store --file "+file)
	if err := reject(context.Background(), r.Dir, f); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(file)
	if strings.Contains(string(b), "tok") {
		t.Fatalf("reject left %q", b)
	}
}
