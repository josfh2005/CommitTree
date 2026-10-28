package ops_test

import (
	"strings"
	"testing"

	"git-ui/internal/testrepo"
)

// aheadClone returns a clone of a remote with branches main, develop and
// release/1.0, all tracking origin and all official; main and develop each
// have one commit the remote lacks, release/1.0 has none. feature/x is
// ahead too but is not official.
func aheadClone(t *testing.T) (*testrepo.Repo, string) {
	t.Helper()
	src := testrepo.New(t)
	src.Commit("base")
	src.Git("branch", "develop")
	src.Git("branch", "release/1.0")
	src.Git("branch", "feature/x")
	bare := testrepo.NewBareFrom(t, src)
	a := testrepo.Clone(t, bare)
	a.Git("branch", "--track", "develop", "origin/develop")
	a.Git("branch", "--track", "release/1.0", "origin/release/1.0")
	a.Git("branch", "--track", "feature/x", "origin/feature/x")
	a.Commit("main local")
	a.Git("switch", "-q", "develop")
	a.Commit("develop local")
	a.Git("switch", "-q", "feature/x")
	a.Commit("feature local")
	a.Git("switch", "-q", "main")
	return a, bare
}

// remoteHash is what the remote's branch points at, from ls-remote.
func remoteHash(r *testrepo.Repo, remote, branch string) string {
	return strings.Fields(r.Git("ls-remote", remote, "refs/heads/"+branch) + " ")[0]
}
