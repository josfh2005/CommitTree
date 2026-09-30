package gitflow

import (
	"context"
	"testing"

	"git-ui/internal/testrepo"
)

var ctx = context.Background()

// newFlowRepo is a repository initialised the way SourceTree leaves the
// owner's repositories: master and develop, feature/release/hotfix prefixes
// and no warmfix prefix. It is left on develop.
func newFlowRepo(t *testing.T) *testrepo.Repo {
	t.Helper()
	r := testrepo.New(t)
	r.Commit("init")
	r.Git("branch", "-m", "master")
	r.Git("branch", "develop")
	r.Git("config", "gitflow.branch.master", "master")
	r.Git("config", "gitflow.branch.develop", "develop")
	r.Git("config", "gitflow.prefix.feature", "feature/")
	r.Git("config", "gitflow.prefix.release", "release/")
	r.Git("config", "gitflow.prefix.hotfix", "hotfix/")
	r.Git("config", "gitflow.prefix.versiontag", "")
	r.Git("switch", "-q", "develop")
	return r
}

// withRemote gives r a bare "origin" holding its branches and makes master
// and develop track it; it returns the bare repository's path.
func withRemote(t *testing.T, r *testrepo.Repo) string {
	t.Helper()
	bare := testrepo.NewBareFrom(t, r)
	r.Git("remote", "add", "origin", bare)
	r.Git("fetch", "-q", "origin")
	r.Git("branch", "-q", "-u", "origin/master", "master")
	r.Git("branch", "-q", "-u", "origin/develop", "develop")
	return bare
}

// writeCommit commits content to file on the current branch.
func writeCommit(r *testrepo.Repo, file, content, msg string) {
	r.WriteFile(file, content)
	r.Git("add", file)
	r.Git("commit", "-q", "-m", msg)
}
