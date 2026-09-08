// Package registry reads Claude Code's own session registry:
// ~/.claude/sessions/<pid>.json, one file per running claude process.
//
// The registry is undocumented; the shape here was observed on Claude Code
// 2.1.263 and is pinned by a fixture test. It gives us discovery (pid, cwd,
// session id) and the exact tmux pane. Its status field is Claude's own and
// is NOT trusted for the sidebar: it was seen reporting "busy" minutes after
// a turn had ended. Only "waiting" + waitingFor is used, as a fallback.
package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
)

// Entry is one running claude process as Claude Code describes it.
type Entry struct {
	PID             int
	SessionID       string
	CWD             string
	Name            string // Claude's display name, e.g. "miivo-ef"
	Kind            string // "interactive" | "background"
	Status          string // busy | idle | waiting | shell (Claude's own)
	WaitingFor      string // set with Status == "waiting": "permission prompt", "input needed", ...
	StartedAt       time.Time
	StatusUpdatedAt time.Time
	TmuxSession     string // empty when the process is not in tmux
	TmuxWindowID    string // "@10"
	TmuxPaneID      string // "%12"
}

type raw struct {
	PID             int    `json:"pid"`
	SessionID       string `json:"sessionId"`
	CWD             string `json:"cwd"`
	Name            string `json:"name"`
	Kind            string `json:"kind"`
	Status          string `json:"status"`
	WaitingFor      string `json:"waitingFor"`
	StartedAt       int64  `json:"startedAt"`
	StatusUpdatedAt int64  `json:"statusUpdatedAt"`
	Tmux            string `json:"tmux"`
}

// tmuxRe matches Claude's "session:@window.%pane". Session names may
// themselves contain colons, so the match is anchored on the id suffix.
var tmuxRe = regexp.MustCompile(`^(.+):(@\d+)\.(%\d+)$`)

// ParseTmux splits Claude's tmux field into session name, window id, pane id.
func ParseTmux(s string) (session, windowID, paneID string, ok bool) {
	m := tmuxRe.FindStringSubmatch(s)
	if m == nil {
		return "", "", "", false
	}
	return m[1], m[2], m[3], true
}

// Alive reports whether pid exists (signal 0). A process we cannot signal
// but that exists (EPERM) counts as alive; none of ours should be.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func ms(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.UnixMilli(v)
}

// Load reads every registry file in dir whose pid passes alive, sorted by
// start time. Unreadable or malformed files are skipped. A missing dir
// yields nil: no Claude has ever run, or the layout changed.
func Load(dir string, alive func(pid int) bool) []Entry {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Entry
	for _, de := range ents {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, de.Name()))
		if err != nil {
			continue
		}
		var r raw
		if json.Unmarshal(b, &r) != nil || r.SessionID == "" || !alive(r.PID) {
			continue
		}
		e := Entry{
			PID: r.PID, SessionID: r.SessionID, CWD: r.CWD, Name: r.Name, Kind: r.Kind,
			Status: r.Status, WaitingFor: r.WaitingFor,
			StartedAt: ms(r.StartedAt), StatusUpdatedAt: ms(r.StatusUpdatedAt),
		}
		e.TmuxSession, e.TmuxWindowID, e.TmuxPaneID, _ = ParseTmux(r.Tmux)
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out
}
