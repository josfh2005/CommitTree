package app

import (
	"os/exec"
	"runtime"
)

// revealCommand is the command that opens path in the platform's file
// manager: Finder on macOS, Explorer on Windows, and whatever xdg-open
// hands it to elsewhere.
func revealCommand(goos, path string) (string, []string) {
	switch goos {
	case "darwin":
		return "open", []string{path}
	case "windows":
		return "explorer", []string{path}
	default:
		return "xdg-open", []string{path}
	}
}

// openFolder opens path in the file manager without waiting for it:
// explorer.exe exits non-zero even when it succeeds, and the file manager
// is not ours to wait on.
func openFolder(path string) error {
	name, args := revealCommand(runtime.GOOS, path)
	return exec.Command(name, args...).Start()
}

// OpenRepoFolder shows the repository's working tree in the file manager.
func (a *App) OpenRepoFolder(id string) error {
	dir, err := a.dir(id)
	if err != nil {
		return err
	}
	return openFolder(dir)
}
