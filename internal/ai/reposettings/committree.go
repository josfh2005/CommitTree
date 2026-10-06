package reposettings

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	Dir        = ".committree"
	SharedFile = "instructions.md"
	MaxFile    = 16 << 10
	MaxTotal   = 32 << 10

	StateNone     = "none"
	StateApproved = "approved"
	StatePending  = "pending"
	StateIgnored  = "ignored"
	StateChanged  = "changed"

	IgnoredPrefix = "ignored:"
)

type RepoFile struct {
	Name    string `json:"name"`
	Text    string `json:"text"`
	Ignored string `json:"ignored,omitempty"`
}

type RepoInstructions struct {
	Files []RepoFile `json:"files"`
	// Hash covers the used files only; "" when none is used.
	Hash  string `json:"hash"`
	Error string `json:"error,omitempty"`
}

// ReadRepo lists the .md files directly in dir/.committree, in name order,
// and hashes the ones that will be used. It never follows a symbolic link
// and never fails: a problem becomes Error or a file's Ignored reason.
func ReadRepo(dir string) RepoInstructions {
	ri := RepoInstructions{Files: []RepoFile{}}
	root := filepath.Join(dir, Dir)
	info, err := os.Lstat(root)
	switch {
	case os.IsNotExist(err):
		return ri
	case err != nil:
		ri.Error = err.Error()
		return ri
	case info.Mode()&os.ModeSymlink != 0:
		ri.Error = Dir + " is a symbolic link"
		return ri
	case !info.IsDir():
		ri.Error = Dir + " is not a folder"
		return ri
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		ri.Error = err.Error()
		return ri
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	total := 0
	h := sha256.New()
	used := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".md") || e.IsDir() {
			continue
		}
		f := RepoFile{Name: name}
		switch {
		case e.Type()&os.ModeSymlink != 0:
			f.Ignored = "a symbolic link"
		case !e.Type().IsRegular():
			f.Ignored = "not a regular file"
		}
		if f.Ignored == "" {
			data, err := os.ReadFile(filepath.Join(root, name))
			switch {
			case err != nil:
				f.Ignored = err.Error()
			case len(data) > MaxFile:
				f.Ignored = "larger than 16 KB"
			case !utf8.Valid(data):
				f.Ignored = "not UTF-8 text"
			case name != SharedFile && !isAction(strings.TrimSuffix(name, ".md")):
				f.Ignored = "not used"
			case total+len(data) > MaxTotal:
				f.Ignored = "past the 32 KB total"
			default:
				total += len(data)
				f.Text = string(data)
				fmt.Fprintf(h, "%s\x00%s\x00", name, data)
				used++
			}
		}
		ri.Files = append(ri.Files, f)
	}
	if used > 0 {
		ri.Hash = "sha256:" + hex.EncodeToString(h.Sum(nil))
	}
	return ri
}

// Text returns the used texts for action: instructions.md, then
// <action>.md, skipping missing or ignored ones.
func (ri RepoInstructions) Text(action string) []string {
	var out []string
	for _, want := range []string{SharedFile, action + ".md"} {
		for _, f := range ri.Files {
			if f.Name == want && f.Ignored == "" && strings.TrimSpace(f.Text) != "" {
				out = append(out, strings.TrimSpace(f.Text))
			}
		}
	}
	return out
}

// StateOf compares the stored approval with the current hash.
func StateOf(approval, hash string) string {
	switch {
	case hash == "":
		return StateNone
	case approval == "":
		return StatePending
	case approval == hash:
		return StateApproved
	case approval == IgnoredPrefix+hash:
		return StateIgnored
	}
	return StateChanged
}
