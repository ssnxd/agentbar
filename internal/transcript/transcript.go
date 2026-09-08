// Package transcript reads the tail of a Claude Code transcript (.jsonl) for
// display metadata: title, first prompt, model, branch, context size, and
// whether a turn is open. Transcripts can be tens of MB; only the last 64KB
// is read, and results are cached by file size and mtime so a 1s poll is
// cheap.
package transcript

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Info is what the tail of a transcript tells us.
type Info struct {
	Title          string // Claude's generated aiTitle
	FirstPrompt    string // first plain user prompt seen in the tail
	Model          string
	Branch         string
	PermissionMode string
	ContextTokens  int64
	LastActivity   time.Time // timestamp of the last substantive line, else file mtime
	TurnOpen       bool      // last substantive line was not a turn_duration
	Known          bool      // file existed and was parsed
}

const chunk = 64 * 1024
const maxPrompt = 80

var mungeRe = regexp.MustCompile(`[/.]`)

// MungeProjectDir converts a cwd into Claude Code's projects-dir name
// (slashes and dots become dashes).
func MungeProjectDir(cwd string) string {
	return mungeRe.ReplaceAllString(cwd, "-")
}

var found = struct {
	sync.Mutex
	m map[string]string // session id → transcript path
}{m: map[string]string{}}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// Newest returns the transcript path for (cwd, sessionID): <sessionID>.jsonl
// under the cwd's project dir, else under any project dir (a session that
// cd'd into a worktree keeps its transcript where it started), else the most
// recently modified .jsonl in the cwd's project dir, or "" when none.
func Newest(projectsDir, cwd, sessionID string) string {
	dir := filepath.Join(projectsDir, MungeProjectDir(cwd))
	if sessionID != "" {
		found.Lock()
		p, ok := found.m[sessionID]
		found.Unlock()
		if ok && isFile(p) {
			return p
		}
		p = filepath.Join(dir, sessionID+".jsonl")
		if !isFile(p) {
			p = ""
			if ms, _ := filepath.Glob(filepath.Join(projectsDir, "*", sessionID+".jsonl")); len(ms) > 0 {
				p = ms[0]
			}
		}
		if p != "" {
			found.Lock()
			found.m[sessionID] = p
			found.Unlock()
			return p
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var newest string
	var newestMod time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(newestMod) {
			newestMod = info.ModTime()
			newest = filepath.Join(dir, e.Name())
		}
	}
	return newest
}

type cached struct {
	size int64
	mod  time.Time
	info Info
}

var cache = struct {
	sync.Mutex
	m map[string]cached
}{m: map[string]cached{}}

// Tail parses the last chunk of the transcript at path. A missing or
// unreadable file yields Info{Known: false}.
func Tail(path string) Info {
	st, err := os.Stat(path)
	if err != nil {
		return Info{}
	}
	cache.Lock()
	c, ok := cache.m[path]
	cache.Unlock()
	if ok && c.size == st.Size() && c.mod.Equal(st.ModTime()) {
		return c.info
	}
	info := parse(path, st.Size())
	if info.LastActivity.IsZero() {
		info.LastActivity = st.ModTime()
	}
	info.Known = true
	cache.Lock()
	cache.m[path] = cached{size: st.Size(), mod: st.ModTime(), info: info}
	cache.Unlock()
	return info
}

type line struct {
	Type           string `json:"type"`
	Subtype        string `json:"subtype"`
	Timestamp      string `json:"timestamp"`
	IsMeta         bool   `json:"isMeta"`
	AITitle        string `json:"aiTitle"`
	GitBranch      string `json:"gitBranch"`
	PermissionMode string `json:"permissionMode"`
	Message        struct {
		Model   string          `json:"model"`
		Content json.RawMessage `json:"content"`
		Usage   struct {
			InputTokens         int64 `json:"input_tokens"`
			CacheReadTokens     int64 `json:"cache_read_input_tokens"`
			CacheCreationTokens int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

func parse(path string, size int64) Info {
	var info Info
	f, err := os.Open(path)
	if err != nil {
		return info
	}
	defer f.Close()
	off := size - chunk
	if off < 0 {
		off = 0
	}
	buf := make([]byte, size-off)
	if _, err := f.ReadAt(buf, off); err != nil && len(buf) == 0 {
		return info
	}
	lines := bytes.Split(buf, []byte("\n"))
	if off > 0 && len(lines) > 0 {
		lines = lines[1:] // first line is probably truncated
	}
	lastKind := ""
	for _, raw := range lines {
		var d line
		if err := json.Unmarshal(raw, &d); err != nil {
			continue
		}
		if d.AITitle != "" {
			info.Title = d.AITitle
		}
		if d.GitBranch != "" {
			info.Branch = d.GitBranch
		}
		if d.PermissionMode != "" {
			info.PermissionMode = d.PermissionMode
		}
		if d.Type == "user" && info.FirstPrompt == "" {
			var s string
			if json.Unmarshal(d.Message.Content, &s) == nil {
				info.FirstPrompt = clip(s)
			}
		}
		if d.Type == "assistant" && d.Message.Model != "" {
			info.Model = d.Message.Model
			u := d.Message.Usage
			if total := u.InputTokens + u.CacheReadTokens + u.CacheCreationTokens; total > 0 {
				info.ContextTokens = total
			}
		}
		// Track the last substantive line to classify turn state. Meta and
		// bookkeeping lines (Claude appends e.g. away_summary while idle)
		// don't tell us whether a turn is open, and their timestamps are not
		// activity.
		substantive := false
		switch {
		case d.IsMeta:
		case d.Type == "system" && d.Subtype == "turn_duration":
			lastKind = "turn_end"
			substantive = true
		case d.Type == "user" || d.Type == "assistant" || d.Type == "attachment":
			lastKind = "turn_open"
			substantive = true
		}
		if substantive && d.Timestamp != "" {
			if ts, err := time.Parse(time.RFC3339Nano, d.Timestamp); err == nil {
				info.LastActivity = ts
			}
		}
	}
	info.TurnOpen = lastKind == "turn_open"
	return info
}

func clip(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > maxPrompt {
		return string(r[:maxPrompt-1]) + "…"
	}
	return s
}
