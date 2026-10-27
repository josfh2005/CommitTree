// Package update finds out whether a newer CommitTree has been released and,
// in later steps, downloads and installs it.
package update

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a MAJOR.MINOR.PATCH release number.
type Version struct{ Major, Minor, Patch int }

// ParseVersion reads "MAJOR.MINOR.PATCH": three groups of digits, no "v"
// prefix and no pre-release suffix.
func ParseVersion(s string) (Version, error) {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("version %q is not MAJOR.MINOR.PATCH", s)
	}
	var n [3]int
	for i, p := range parts {
		if p == "" || len(p) > 9 {
			return Version{}, fmt.Errorf("version %q is not MAJOR.MINOR.PATCH", s)
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return Version{}, fmt.Errorf("version %q is not MAJOR.MINOR.PATCH", s)
			}
		}
		n[i], _ = strconv.Atoi(p)
	}
	return Version{n[0], n[1], n[2]}, nil
}

// Less reports whether v is an older release than o, comparing each part as
// a number.
func (v Version) Less(o Version) bool {
	if v.Major != o.Major {
		return v.Major < o.Major
	}
	if v.Minor != o.Minor {
		return v.Minor < o.Minor
	}
	return v.Patch < o.Patch
}

// String returns "MAJOR.MINOR.PATCH".
func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}
