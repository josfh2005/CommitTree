package app

import (
	"errors"
	"strings"

	"git-ui/internal/gitcmd"
)

// Identity is who commits in a repository: git's user.name and user.email
// as that repository resolves them (its own config, then the global one).
type Identity struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// GetIdentity reads the repository's user.name and user.email. A value that
// is not set comes back empty rather than as an error. Read-only, so it
// takes no write lock.
func (a *App) GetIdentity(id string) (Identity, error) {
	dir, err := a.dir(id)
	if err != nil {
		return Identity{}, err
	}
	name, err := configValue(a, dir, "user.name")
	if err != nil {
		return Identity{}, err
	}
	email, err := configValue(a, dir, "user.email")
	if err != nil {
		return Identity{}, err
	}
	return Identity{Name: name, Email: email}, nil
}

// configValue returns a git config value, or "" when it is not set (git
// config exits 1 for a missing key).
func configValue(a *App, dir, key string) (string, error) {
	out, err := gitcmd.Run(a.ctx, dir, gitcmd.ReadTimeout, "config", "--get", key)
	var gerr *gitcmd.Error
	if errors.As(err, &gerr) && gerr.ExitCode == 1 {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}
