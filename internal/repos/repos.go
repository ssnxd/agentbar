// Package repos discovers git repositories for the new-task picker: known
// projects from the DB merged with a filesystem scan of configured roots.
// The scan is exec-free — branch and activity come from reading .git files
// directly — so hundreds of repos list instantly.
package repos

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sahilm/fuzzy"
)

type Repo struct {
	Path     string
	Name     string
	Branch   string
	Known    bool // registered in workflow's DB (used before)
	Activity time.Time
}

// Discover walks roots (depth-limited) for git repos and merges them with
// known project paths (which sort first, in the order given — pass them
// most-recently-used first).
func Discover(roots []string, known []string) []Repo {
	seen := make(map[string]bool)
	var out []Repo

	for _, k := range known {
		if r, ok := describe(k); ok {
			r.Known = true
			out = append(out, r)
			seen[k] = true
		}
	}

	var scanned []Repo
	for _, root := range roots {
		root = expandHome(root)
		walk(root, 0, 4, func(dir string) {
			if seen[dir] {
				return
			}
			if r, ok := describe(dir); ok {
				scanned = append(scanned, r)
				seen[dir] = true
			}
		})
	}
	// Most recently active first.
	for i := 1; i < len(scanned); i++ {
		for j := i; j > 0 && scanned[j].Activity.After(scanned[j-1].Activity); j-- {
			scanned[j], scanned[j-1] = scanned[j-1], scanned[j]
		}
	}
	return append(out, scanned...)
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}

// walk descends into directories looking for .git, stopping at repos and
// skipping noise. fn is called with each repo dir.
func walk(dir string, depth, maxDepth int, fn func(string)) {
	if depth > maxDepth {
		return
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		fn(dir)
		return // do not descend into repos (submodules, vendored trees)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || strings.HasPrefix(name, ".") ||
			name == "node_modules" || name == "vendor" || name == "target" {
			continue
		}
		walk(filepath.Join(dir, name), depth+1, maxDepth, fn)
	}
}

// describe builds a Repo from a path without exec'ing git: branch parsed from
// .git/HEAD, activity approximated by the newest of HEAD/index mtimes.
func describe(dir string) (Repo, bool) {
	gitPath := filepath.Join(dir, ".git")
	st, err := os.Stat(gitPath)
	if err != nil {
		return Repo{}, false
	}
	r := Repo{Path: dir, Name: filepath.Base(dir), Activity: st.ModTime()}
	gitDir := gitPath
	if !st.IsDir() {
		// Worktree/submodule: `.git` is a file with "gitdir: <path>".
		raw, err := os.ReadFile(gitPath)
		if err != nil {
			return r, true
		}
		gd := strings.TrimSpace(strings.TrimPrefix(string(raw), "gitdir:"))
		if !filepath.IsAbs(gd) {
			gd = filepath.Join(dir, gd)
		}
		gitDir = gd
	}
	r.Branch = ParseHEAD(readFile(filepath.Join(gitDir, "HEAD")))
	for _, f := range []string{"HEAD", "index"} {
		if info, err := os.Stat(filepath.Join(gitDir, f)); err == nil && info.ModTime().After(r.Activity) {
			r.Activity = info.ModTime()
		}
	}
	return r, true
}

func readFile(p string) string {
	raw, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// ParseHEAD extracts a display branch from .git/HEAD content: a ref name, or
// a short hash for detached HEAD.
func ParseHEAD(head string) string {
	if head == "" {
		return ""
	}
	if strings.HasPrefix(head, "ref:") {
		ref := strings.TrimSpace(strings.TrimPrefix(head, "ref:"))
		return strings.TrimPrefix(ref, "refs/heads/")
	}
	if len(head) >= 8 {
		return head[:8] + " (detached)"
	}
	return head
}

// Match is one fuzzy-filtered result with highlight positions into Display.
type Match struct {
	Repo
	Display    string // the string that was matched (name + dimmed path)
	Highlights map[int]bool
}

type source []Repo

func (s source) String(i int) string { return s[i].Path }
func (s source) Len() int            { return len(s) }

// Filter fuzzy-matches query against repo paths. An empty query returns
// everything in discovery order (known/recent first).
func Filter(all []Repo, query string) []Match {
	if strings.TrimSpace(query) == "" {
		out := make([]Match, len(all))
		for i, r := range all {
			out[i] = Match{Repo: r, Display: r.Path}
		}
		return out
	}
	results := fuzzy.FindFrom(query, source(all))
	out := make([]Match, 0, len(results))
	for _, res := range results {
		hl := make(map[int]bool, len(res.MatchedIndexes))
		for _, idx := range res.MatchedIndexes {
			hl[idx] = true
		}
		out = append(out, Match{Repo: all[res.Index], Display: all[res.Index].Path, Highlights: hl})
	}
	return out
}
