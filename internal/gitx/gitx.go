// Package gitx wraps the git operations workflow needs: task/agent branches,
// worktrees, and diff stats.
package gitx

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

// IsRepo reports whether dir is inside a git work tree.
func IsRepo(dir string) bool {
	out, err := run(dir, "rev-parse", "--is-inside-work-tree")
	return err == nil && out == "true"
}

// AddWorktree creates a worktree at path with a new branch from base.
// If the branch already exists (recovery), it is checked out instead.
func AddWorktree(repo, path, branch, base string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if _, err := run(repo, "worktree", "add", "-b", branch, path, base); err != nil {
		if strings.Contains(err.Error(), "already exists") {
			_, err2 := run(repo, "worktree", "add", path, branch)
			return err2
		}
		return err
	}
	return nil
}

// RemoveWorktree removes a worktree (forced: agent worktrees are disposable
// once the task is archived) and prunes stale registrations.
func RemoveWorktree(repo, path string) error {
	if _, err := run(repo, "worktree", "remove", "--force", path); err != nil {
		if !strings.Contains(err.Error(), "not a working tree") &&
			!strings.Contains(err.Error(), "No such file") {
			return err
		}
	}
	_, err := run(repo, "worktree", "prune")
	return err
}

// ShortStat returns (filesChanged, insertions, deletions) of committed work
// on branch since base, from any checkout of the repo.
func ShortStat(dir, base, branch string) (int, int, int) {
	out, err := run(dir, "diff", "--shortstat", base+"..."+branch)
	if err != nil || out == "" {
		return 0, 0, 0
	}
	var files, ins, del int
	for _, part := range strings.Split(out, ",") {
		part = strings.TrimSpace(part)
		var n int
		switch {
		case strings.Contains(part, "file"):
			fmt.Sscanf(part, "%d", &n)
			files = n
		case strings.Contains(part, "insertion"):
			fmt.Sscanf(part, "%d", &n)
			ins = n
		case strings.Contains(part, "deletion"):
			fmt.Sscanf(part, "%d", &n)
			del = n
		}
	}
	return files, ins, del
}

// CopyGlobs copies files matching the given repo-root-relative globs from
// src to dst (worktree seeding: .env files and friends do not follow a
// checkout).
func CopyGlobs(src, dst string, globs []string) error {
	for _, g := range globs {
		matches, err := filepath.Glob(filepath.Join(src, g))
		if err != nil {
			continue
		}
		for _, m := range matches {
			rel, err := filepath.Rel(src, m)
			if err != nil {
				continue
			}
			info, err := os.Stat(m)
			if err != nil || info.IsDir() {
				continue
			}
			raw, err := os.ReadFile(m)
			if err != nil {
				return err
			}
			target := filepath.Join(dst, rel)
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(target, raw, info.Mode()); err != nil {
				return err
			}
		}
	}
	return nil
}
