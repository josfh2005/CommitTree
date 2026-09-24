package app

import "git-ui/internal/gitlog"

// GetBlame blames path at rev ("" = the working tree). Read-only, so it
// takes no write lock.
func (a *App) GetBlame(id, rev, path string, ignoreWhitespace bool) (gitlog.Blame, error) {
	dir, err := a.dir(id)
	if err != nil {
		return gitlog.Blame{}, err
	}
	return gitlog.GetBlame(a.ctx, dir, rev, path, gitlog.BlameOptions{IgnoreWhitespace: ignoreWhitespace})
}
