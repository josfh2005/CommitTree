package hosting

import "strings"

// SplitUnifiedDiff cuts a multi-file unified diff into one text per file,
// keyed by the file's new path (the old path for a deletion). Each text starts
// at its "diff --git" line.
func SplitUnifiedDiff(diff string) map[string]string {
	out := map[string]string{}
	var lines []string
	flush := func() {
		if len(lines) == 0 {
			return
		}
		out[diffPath(lines)] = strings.Join(lines, "\n") + "\n"
		lines = nil
	}
	for _, l := range strings.Split(strings.TrimRight(diff, "\n"), "\n") {
		if strings.HasPrefix(l, "diff --git ") {
			flush()
		}
		if len(lines) == 0 && !strings.HasPrefix(l, "diff --git ") {
			continue // text before the first file header
		}
		lines = append(lines, l)
	}
	flush()
	return out
}

// diffPath reads the path a file section is filed under from its header.
func diffPath(section []string) string {
	rest := strings.TrimPrefix(section[0], "diff --git ")
	oldP, newP := splitHeaderPaths(rest)
	for _, l := range section[1:] {
		if strings.HasPrefix(l, "@@") {
			break
		}
		if strings.HasPrefix(l, "deleted file mode") {
			return oldP
		}
	}
	return newP
}

// splitHeaderPaths splits "a/old b/new". When old and new are equal (the
// usual case) the split is exact even if the path holds spaces; otherwise it
// falls back to the last " b/".
func splitHeaderPaths(rest string) (oldP, newP string) {
	if n := (len(rest) - 1) / 2; len(rest) == 2*n+1 && n > 2 && rest[n:n+3] == " b/" && rest[:n] == "a/"+rest[n+3:] {
		return rest[2:n], rest[n+3:]
	}
	if i := strings.LastIndex(rest, " b/"); i >= 0 {
		return strings.TrimPrefix(rest[:i], "a/"), rest[i+3:]
	}
	return rest, rest
}

// PatchShowsChanges reports whether a file's section of a unified diff has
// something to show: at least one hunk, or a pure rename or copy (its header
// is the whole story). A binary file's section is only a header and "Binary
// files … differ", which is not a diff.
func PatchShowsChanges(section string) bool {
	renamed, binary := false, false
	for _, l := range strings.Split(section, "\n") {
		switch {
		case strings.HasPrefix(l, "@@"):
			return true
		case strings.HasPrefix(l, "rename from "), strings.HasPrefix(l, "copy from "):
			renamed = true
		case strings.HasPrefix(l, "Binary files "), strings.HasPrefix(l, "GIT binary patch"):
			binary = true
		}
	}
	return renamed && !binary
}
