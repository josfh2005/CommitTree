// Package repoinfo reads how big a repository is and whether git would
// garbage-collect it, for the line under the repository's path in the log
// header (docs/spec/01-repositories-and-sidebar.md, "Repository info").
package repoinfo

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"

	"git-ui/internal/gitcmd"
)

// Defaults of gc.auto and gc.autoPackLimit, as git documents them.
const (
	DefaultAutoLimit = 6700
	DefaultPackLimit = 50
)

// Info is `git count-objects -v`, sizes in bytes, plus git's own GC rule.
type Info struct {
	Loose         int   `json:"loose"`
	LooseSize     int64 `json:"looseSize"`
	Packed        int   `json:"packed"`
	Packs         int   `json:"packs"`
	PackSize      int64 `json:"packSize"`
	PrunePackable int   `json:"prunePackable"`
	Garbage       int   `json:"garbage"`
	GarbageSize   int64 `json:"garbageSize"`
	// Size is everything in the object database: loose + packs + garbage.
	Size int64 `json:"size"`
	// AutoLimit and PackLimit are the repository's gc.auto and
	// gc.autoPackLimit (0 turns that rule off, as in git).
	AutoLimit int `json:"autoLimit"`
	PackLimit int `json:"packLimit"`
	// GCReason says why a garbage collection is recommended; "" when not.
	GCReason string `json:"gcReason"`
}

// Read runs `git count-objects -v` and reads the GC thresholds.
func Read(ctx context.Context, dir string) (Info, error) {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "count-objects", "-v")
	if err != nil {
		return Info{}, err
	}
	info := parse(out)
	info.AutoLimit = configInt(ctx, dir, "gc.auto", DefaultAutoLimit)
	info.PackLimit = configInt(ctx, dir, "gc.autoPackLimit", DefaultPackLimit)
	info.GCReason = gcReason(info.Loose, info.Packs, info.AutoLimit, info.PackLimit)
	return info, nil
}

// parse reads count-objects -v, whose sizes are in KiB.
func parse(out string) Info {
	var info Info
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil {
			continue
		}
		switch key {
		case "count":
			info.Loose = int(n)
		case "size":
			info.LooseSize = n * 1024
		case "in-pack":
			info.Packed = int(n)
		case "packs":
			info.Packs = int(n)
		case "size-pack":
			info.PackSize = n * 1024
		case "prune-packable":
			info.PrunePackable = int(n)
		case "garbage":
			info.Garbage = int(n)
		case "size-garbage":
			info.GarbageSize = n * 1024
		}
	}
	info.Size = info.LooseSize + info.PackSize + info.GarbageSize
	return info
}

// gcReason is git gc --auto's rule: more loose objects than gc.auto, or
// more packs than gc.autoPackLimit; a limit of 0 turns its rule off, and
// gc.auto=0 turns both off, as in git.
func gcReason(loose, packs, autoLimit, packLimit int) string {
	if autoLimit <= 0 {
		return ""
	}
	if loose > autoLimit {
		return fmt.Sprintf("%s loose objects (git suggests GC above %s)", thousands(loose), thousands(autoLimit))
	}
	if packLimit > 0 && packs > packLimit {
		return fmt.Sprintf("%s packs (git suggests GC above %s)", thousands(packs), thousands(packLimit))
	}
	return ""
}

func configInt(ctx context.Context, dir, key string, def int) int {
	out, err := gitcmd.Run(ctx, dir, gitcmd.ReadTimeout, "config", "--type=int", "--get", key)
	if err != nil {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return def
	}
	return n
}

// thousands writes n with comma separators: 6912 → "6,912".
func thousands(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// Collect runs a plain `git gc`: not aggressive, with git's default
// retention for unreachable objects.
func Collect(ctx context.Context, dir string) error {
	_, err := gitcmd.Run(ctx, dir, gitcmd.GCTimeout, "gc")
	return err
}

// WorktreeSize is what the files under dir take on disk: every regular file,
// ignored ones included (node_modules, build output), but no .git directory
// at any depth and no symlink followed. Unreadable directories are skipped.
func WorktreeSize(ctx context.Context, dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			if d != nil && d.IsDir() && path != dir {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" && path != dir {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil // a symlink, socket or device: not followed or counted
		}
		if fi, err := d.Info(); err == nil {
			total += fi.Size()
		}
		return nil
	})
	return total, err
}
