package app

import (
	"reflect"
	"testing"
)

func TestRevealCommandPerPlatform(t *testing.T) {
	cases := []struct {
		goos, name string
		args       []string
	}{
		{"darwin", "open", []string{"/r"}},
		{"windows", "explorer", []string{"/r"}},
		{"linux", "xdg-open", []string{"/r"}},
		{"freebsd", "xdg-open", []string{"/r"}},
	}
	for _, c := range cases {
		name, args := revealCommand(c.goos, "/r")
		if name != c.name || !reflect.DeepEqual(args, c.args) {
			t.Errorf("%s: got %s %v, want %s %v", c.goos, name, args, c.name, c.args)
		}
	}
}

func TestOpenRepoFolderRejectsAnUnknownRepo(t *testing.T) {
	a, _ := newTestApp(t)
	if err := a.OpenRepoFolder("nope"); err == nil {
		t.Fatal("want an error for an unknown repository")
	}
}

func TestTerminalCommandPerPlatform(t *testing.T) {
	cases := []struct {
		goos, name string
		args       []string
	}{
		{"darwin", "open", []string{"-a", "Terminal", "/r"}},
		{"windows", "cmd", []string{"/c", "start", "cmd"}},
		{"linux", "x-terminal-emulator", nil},
	}
	for _, c := range cases {
		name, args := terminalCommand(c.goos, "/r")
		if name != c.name || !reflect.DeepEqual(args, c.args) {
			t.Errorf("%s: got %s %v, want %s %v", c.goos, name, args, c.name, c.args)
		}
	}
}

func TestOpenRepoTerminalRejectsAnUnknownRepo(t *testing.T) {
	a, _ := newTestApp(t)
	if err := a.OpenRepoTerminal("nope"); err == nil {
		t.Fatal("want an error for an unknown repository")
	}
}
