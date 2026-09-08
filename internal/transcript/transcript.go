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
	LastActivity   time.Time // file mtime
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

// Newest returns the transcript path for (cwd, sessionID): <sessionID>.jsonl
// when it exists, otherwise the most recently modified .jsonl in the
// project dir, or "" when there is none.
func Newest(projectsDir, cwd, sessionID string) string {
	dir := filepath.Join(projectsDir, MungeProjectDir(cwd))
	if sessionID != "" {
		p := filepath.Join(dir, sessionID+".jsonl")
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
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
	info.LastActivity = st.ModTime()
	info.Known = true
	cache.Lock()
	cache.m[path] = cached{size: st.Size(), mod: st.ModTime(), info: info}
	cache.Unlock()
	return info
}

type line struct {
	Type           string `json:"type"`
	Subtype        string `json:"subtype"`
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
		// bookkeeping lines don't tell us whether a turn is open.
		switch {
		case d.IsMeta:
		case d.Type == "system" && d.Subtype == "turn_duration":
			lastKind = "turn_end"
		case d.Type == "user" || d.Type == "assistant" || d.Type == "attachment":
			lastKind = "turn_open"
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
