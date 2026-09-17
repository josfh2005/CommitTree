package app

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

// fallbackPathDirs are appended to PATH when the login shell can't be
// consulted, covering the common Homebrew install locations.
var fallbackPathDirs = []string{"/opt/homebrew/bin", "/usr/local/bin"}

// FixPath repairs PATH for a GUI app launched without a login shell (e.g. by
// Finder on macOS), which only gets PATH=/usr/bin:/bin:/usr/sbin:/sbin. That
// PATH is missing git-lfs and Homebrew credential helpers that a shell
// profile would normally add, so git operations relying on them fail. It
// prefers asking the user's login shell for its PATH, falling back to
// appending common Homebrew locations when that isn't possible.
func FixPath() {
	if p := loginShellPath(); p != "" {
		os.Setenv("PATH", p)
		return
	}
	os.Setenv("PATH", mergeFallbackPath(os.Getenv("PATH")))
}

// loginShellPath asks the user's login shell for its PATH, returning "" if
// no shell is configured or the shell can't be run within the timeout.
func loginShellPath() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, shell, "-lc", `printf %s "$PATH"`).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// mergeFallbackPath appends fallbackPathDirs to current (a PATH-style,
// colon-separated string) for any directory not already present, without
// introducing duplicates or reordering existing entries.
func mergeFallbackPath(current string) string {
	seen := make(map[string]bool)
	parts := []string{}
	for _, p := range strings.Split(current, ":") {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		parts = append(parts, p)
	}
	for _, d := range fallbackPathDirs {
		if seen[d] {
			continue
		}
		seen[d] = true
		parts = append(parts, d)
	}
	return strings.Join(parts, ":")
}
