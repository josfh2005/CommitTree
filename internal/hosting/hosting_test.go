package hosting

import (
	"testing"

	"git-ui/internal/ops"
)

func TestParseRemoteURL(t *testing.T) {
	cases := []struct {
		in    string
		host  Host
		owner string
		name  string
		ok    bool
	}{
		{"https://bitbucket.org/acme/api.git", Bitbucket, "acme", "api", true},
		{"https://jose@bitbucket.org/acme/api", Bitbucket, "acme", "api", true},
		{"https://user:secret@github.com/o/r.git", GitHub, "o", "r", true},
		{"git@bitbucket.org:acme/api.git", Bitbucket, "acme", "api", true},
		{"ssh://git@github.com/o/r.git", GitHub, "o", "r", true},
		{"git@github.com:o/r", GitHub, "o", "r", true},
		{"https://GitHub.com/o/r/", GitHub, "o", "r", true},
		{"https://gitlab.com/o/r", "", "", "", false},
		{"git@bitbucket-work:acme/api.git", "", "", "", false},
		{"https://bitbucket.org/acme", "", "", "", false},
		{"https://bitbucket.org/acme/api/extra", "", "", "", false},
		{"/local/path", "", "", "", false},
		{"", "", "", "", false},
	}
	for _, c := range cases {
		got, ok := ParseRemoteURL(c.in)
		if ok != c.ok {
			t.Errorf("%q: ok = %v, want %v", c.in, ok, c.ok)
			continue
		}
		if !ok {
			continue
		}
		if got.Host != c.host || got.Owner != c.owner || got.Name != c.name {
			t.Errorf("%q: got %+v, want %s/%s/%s", c.in, got, c.host, c.owner, c.name)
		}
		if got.Remote != "" {
			t.Errorf("%q: Remote = %q, want empty", c.in, got.Remote)
		}
	}
}

func TestRepoURLs(t *testing.T) {
	b := Repo{Host: Bitbucket, Owner: "acme", Name: "api"}
	if b.FullName() != "acme/api" || b.WebURL() != "https://bitbucket.org/acme/api" || b.PullsURL() != "https://bitbucket.org/acme/api/pull-requests" {
		t.Errorf("bitbucket urls: %s %s %s", b.FullName(), b.WebURL(), b.PullsURL())
	}
	g := Repo{Host: GitHub, Owner: "o", Name: "r"}
	if g.WebURL() != "https://github.com/o/r" || g.PullsURL() != "https://github.com/o/r/pulls" {
		t.Errorf("github urls: %s %s", g.WebURL(), g.PullsURL())
	}
}

func TestDetectPrefersOrigin(t *testing.T) {
	r, ok := Detect([]ops.Remote{
		{Name: "upstream", FetchURL: "https://github.com/o/r.git"},
		{Name: "origin", FetchURL: "https://bitbucket.org/acme/api.git"},
	})
	if !ok || r.Host != Bitbucket || r.Remote != "origin" {
		t.Fatalf("got %+v, %v", r, ok)
	}
}

func TestDetectFirstByName(t *testing.T) {
	r, ok := Detect([]ops.Remote{
		{Name: "zeta", FetchURL: "https://github.com/o/z.git"},
		{Name: "alpha", FetchURL: "https://github.com/o/a.git"},
	})
	if !ok || r.Remote != "alpha" || r.Name != "a" {
		t.Fatalf("got %+v, %v", r, ok)
	}
}

func TestDetectSkipsUnsupportedOrigin(t *testing.T) {
	r, ok := Detect([]ops.Remote{
		{Name: "origin", FetchURL: "https://gitlab.com/o/r.git"},
		{Name: "mirror", FetchURL: "https://github.com/o/r.git"},
	})
	if !ok || r.Remote != "mirror" {
		t.Fatalf("got %+v, %v", r, ok)
	}
}

func TestDetectFallsBackToPushURL(t *testing.T) {
	r, ok := Detect([]ops.Remote{{Name: "origin", PushURL: "git@github.com:o/r.git"}})
	if !ok || r.Host != GitHub {
		t.Fatalf("got %+v, %v", r, ok)
	}
}

func TestDetectNone(t *testing.T) {
	if _, ok := Detect([]ops.Remote{{Name: "origin", FetchURL: "https://gitlab.com/o/r.git"}}); ok {
		t.Fatal("gitlab must not be detected")
	}
	if _, ok := Detect(nil); ok {
		t.Fatal("no remotes must not be detected")
	}
}
