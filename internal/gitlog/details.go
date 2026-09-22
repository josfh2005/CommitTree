package gitlog

import (
	"context"
	"fmt"
	"strings"
	"time"

	"git-ui/internal/gitcmd"
)

type FileChange struct {
	Status  string `json:"status"`
	Path    string `json:"path"`
	OldPath string `json:"oldPath,omitempty"`
}

type Details struct {
	Commit
	Body           string       `json:"body"`
	Committer      string       `json:"committer"`
	CommitterEmail string       `json:"committerEmail"`
	CommitDate     time.Time    `json:"commitDate"`
	Files          []FileChange `json:"files"`
}

const detailsFormat = "%cn%x00%ce%x00%cI%x00%b"

func GetDetails(ctx context.Context, dir, hash string) (Details, error) {
	commits, err := Get(ctx, dir, Filters{Branch: hash}, OrderTopo, 0, 1)
	if err != nil {
		return Details{}, err
	}
	if len(commits) == 0 {
		return Details{}, fmt.Errorf("gitlog: commit %s not found", hash)
	}
	d := Details{Commit: commits[0]}

	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout,
		"show", "-s", "--format="+detailsFormat, "--end-of-options", d.Hash)
	if err != nil {
		return Details{}, err
	}
	f := strings.SplitN(out, "\x00", 4)
	if len(f) != 4 {
		return Details{}, fmt.Errorf("gitlog: unexpected details output for %s", hash)
	}
	d.Committer, d.CommitterEmail = f[0], f[1]
	if d.CommitDate, err = time.Parse(time.RFC3339, f[2]); err != nil {
		return Details{}, fmt.Errorf("gitlog: bad commit date %q: %w", f[2], err)
	}
	d.Body = strings.TrimSpace(f[3])

	args := []string{"diff-tree", "--no-commit-id", "-r", "--name-status", "-z", "-M"}
	if len(d.Parents) == 0 {
		args = append(args, "--root", d.Hash)
	} else {
		args = append(args, d.Parents[0], d.Hash)
	}
	names, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, args...)
	if err != nil {
		return Details{}, err
	}
	d.Files = ParseNameStatus(names)
	return d, nil
}

// ParseNameStatus parses `--name-status -z` output.
func ParseNameStatus(out string) []FileChange {
	parts := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	files := []FileChange{}
	for i := 0; i < len(parts); {
		status := parts[i]
		if status == "" {
			i++
			continue
		}
		kind := status[:1]
		if (kind == "R" || kind == "C") && i+2 < len(parts) {
			files = append(files, FileChange{Status: kind, OldPath: parts[i+1], Path: parts[i+2]})
			i += 3
			continue
		}
		if i+1 >= len(parts) {
			break
		}
		files = append(files, FileChange{Status: kind, Path: parts[i+1]})
		i += 2
	}
	return files
}

// Diff returns the patch for paths in hash compared with parent; parent is
// empty for a root commit.
func Diff(ctx context.Context, dir, parent, hash string, paths []string) (string, error) {
	var args []string
	if parent == "" {
		args = []string{"show", "--format=", "--no-color", "--end-of-options", hash}
	} else {
		args = []string{"diff", "--no-color", "-M", "--end-of-options", parent, hash}
	}
	args = append(args, "--")
	args = append(args, paths...)
	return gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, args...)
}
