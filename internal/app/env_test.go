package app

import "testing"

func TestMergeFallbackPathAppendsMissingDirs(t *testing.T) {
	got := mergeFallbackPath("/usr/bin:/bin:/usr/sbin:/sbin")
	want := "/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin:/usr/local/bin"
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestMergeFallbackPathSkipsAlreadyPresentDirs(t *testing.T) {
	got := mergeFallbackPath("/usr/local/bin:/usr/bin:/bin")
	want := "/usr/local/bin:/usr/bin:/bin:/opt/homebrew/bin"
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestMergeFallbackPathNoOpWhenAllPresent(t *testing.T) {
	current := "/opt/homebrew/bin:/usr/local/bin:/usr/bin"
	got := mergeFallbackPath(current)
	if got != current {
		t.Fatalf("got %q, want unchanged %q", got, current)
	}
}

func TestMergeFallbackPathHandlesEmptyCurrent(t *testing.T) {
	got := mergeFallbackPath("")
	want := "/opt/homebrew/bin:/usr/local/bin"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestMergeFallbackPathDedupsRepeatedCurrentEntries(t *testing.T) {
	got := mergeFallbackPath("/usr/bin::/usr/bin:/opt/homebrew/bin")
	want := "/usr/bin:/opt/homebrew/bin:/usr/local/bin"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
