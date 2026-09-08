// Package external discovers Claude Code sessions that are running on this
// machine but are NOT managed by workflow — sessions the user started
// themselves in other terminals. The TUI surfaces them read-only, clearly
// marked as external.
package transcript

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Session is one externally running claude process.
type Session struct {
	PID          int
	TTY          string
	CWD          string
	Project      string // basename of CWD for display
	StartedAt    time.Time
	LastActivity time.Time // newest transcript write for that CWD (zero if unknown)

	// Enrichment from the newest transcript in the CWD's project dir. When
	// several claude processes share a CWD this reflects the most recent
	// session among them — decoration, not identity. Identity is the TTY.
	Title          string
	Model          string
	Branch         string
	ContextTokens  int64
	PermissionMode string
	// State is derived from the transcript tail:
	//   waiting — last turn closed (turn_duration): it wants your input
	//   working — turn open and transcript written recently
	//   blocked — turn open but no writes for a while: often a permission
	//             prompt or a long-running tool
	State string

	// Where the session lives in the user's own tmux, if it does.
	// (pane_tty is an exact match for the process TTY.)
	InTmux      bool
	TmuxSession string
	TmuxWindow  string // window index on the user's server
	TmuxPane    string // pane id on the user's server

	lastKind string // internal: last substantive transcript line kind
}

// claudeCmdRe matches a claude CLI process: the command is `claude` itself or
// a path ending in /claude, optionally followed by flags. It deliberately
// does not match e.g. `vim claude-notes.md`.
var claudeCmdRe = regexp.MustCompile(`^(?:\S*/)?claude(?:\s|$)`)

// Discover lists external claude sessions. managedRoot is workflow's
// worktrees dir: any claude whose cwd lives under it is one of ours and is
// skipped. Errors degrade to an empty list — this is a best-effort widget,
// never a reason to break the TUI.
func Discover(managedRoot string) []Session {
	if os.Getenv("WORKFLOW_NO_EXTERNAL") != "" {
		return nil // privacy switch: demos, screen shares
	}
	procs := listClaudeProcs()
	if len(procs) == 0 {
		return nil
	}
	cwds := cwdByPID(pids(procs))
	userPanes := userPanesByTTY()
	var out []Session
	for _, p := range procs {
		cwd := cwds[p.PID]
		if cwd == "" {
			continue
		}
		if managedRoot != "" && strings.HasPrefix(cwd, managedRoot+string(os.PathSeparator)) {
			continue // one of ours
		}
		p.CWD = cwd
		p.Project = filepath.Base(cwd)
		enrich(&p)
		if ref, ok := userPanes["/dev/"+p.TTY]; ok {
			p.InTmux = true
			p.TmuxSession, p.TmuxWindow, p.TmuxPane = ref[0], ref[1], ref[2]
		}
		out = append(out, p)
	}
	return out
}

// userPanesByTTY maps pane tty → (session, window index, pane id) on the
// user's DEFAULT tmux server (no -L flag — deliberately not ours). TMUX is
// stripped so the query hits the default socket even when workflow itself
// runs inside some tmux.
func userPanesByTTY() map[string][3]string {
	cmd := exec.Command("tmux", "list-panes", "-a", "-F",
		"#{pane_tty}|#{session_name}|#{window_index}|#{pane_id}")
	cmd.Env = EnvWithoutTmux()
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	res := make(map[string][3]string)
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Split(line, "|")
		if len(f) == 4 {
			res[f[0]] = [3]string{f[1], f[2], f[3]}
		}
	}
	return res
}

func pids(procs []Session) []int {
	ids := make([]int, len(procs))
	for i, p := range procs {
		ids[i] = p.PID
	}
	return ids
}

func listClaudeProcs() []Session {
	out, err := exec.Command("ps", "-axo", "pid=,lstart=,tty=,command=").Output()
	if err != nil {
		return nil
	}
	var procs []Session
	for _, line := range strings.Split(string(out), "\n") {
		s, ok := ParsePSLine(line)
		if ok {
			procs = append(procs, s)
		}
	}
	return procs
}

// ParsePSLine parses one `ps -axo pid=,lstart=,tty=,command=` row and reports
// whether it is a claude CLI process. Exported for testing.
func ParsePSLine(line string) (Session, bool) {
	fields := strings.Fields(line)
	// pid + 5 lstart fields (Day Mon DD HH:MM:SS YYYY) + tty + command...
	if len(fields) < 8 {
		return Session{}, false
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil {
		return Session{}, false
	}
	cmd := strings.Join(fields[7:], " ")
	if !claudeCmdRe.MatchString(cmd) {
		return Session{}, false
	}
	started, _ := time.ParseInLocation("Mon Jan 2 15:04:05 2006",
		strings.Join(fields[1:6], " "), time.Local)
	return Session{PID: pid, TTY: fields[6], StartedAt: started}, true
}

// cwdByPID resolves working directories with one lsof call.
func cwdByPID(ids []int) map[int]string {
	if len(ids) == 0 {
		return nil
	}
	strs := make([]string, len(ids))
	for i, id := range ids {
		strs[i] = strconv.Itoa(id)
	}
	out, err := exec.Command("lsof", "-a", "-p", strings.Join(strs, ","), "-d", "cwd", "-Fpn").Output()
	if err != nil && len(out) == 0 {
		return nil
	}
	res := make(map[int]string)
	pid := 0
	for _, line := range bytes.Split(out, []byte("\n")) {
		if len(line) < 2 {
			continue
		}
		switch line[0] {
		case 'p':
			pid, _ = strconv.Atoi(string(line[1:]))
		case 'n':
			if pid != 0 {
				res[pid] = string(line[1:])
			}
		}
	}
	return res
}

// enrich fills LastActivity and transcript-derived metadata from the newest
// .jsonl in the Claude Code project dir for the session's cwd. Transcripts
// can be tens of MB; only the last 64KB is read.
func enrich(s *Session) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dir := filepath.Join(home, ".claude", "projects", MungeProjectDir(s.CWD))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var newest string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(s.LastActivity) {
			s.LastActivity = info.ModTime()
			newest = filepath.Join(dir, e.Name())
		}
	}
	if newest == "" {
		return
	}
	parseTranscriptTail(newest, s)
	switch {
	case s.lastKind == "turn_end":
		s.State = "waiting"
	case time.Since(s.LastActivity) < 30*time.Second:
		s.State = "working"
	case s.lastKind != "":
		s.State = "blocked"
	}
}

// parseTranscriptTail scans the last chunk of a transcript for the newest
// title, model, branch, and context size.
func parseTranscriptTail(path string, s *Session) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	const chunk = 64 * 1024
	st, err := f.Stat()
	if err != nil {
		return
	}
	off := st.Size() - chunk
	if off < 0 {
		off = 0
	}
	buf := make([]byte, st.Size()-off)
	if _, err := f.ReadAt(buf, off); err != nil && len(buf) == 0 {
		return
	}
	lines := bytes.Split(buf, []byte("\n"))
	if off > 0 && len(lines) > 0 {
		lines = lines[1:] // first line is probably truncated
	}
	for _, line := range lines {
		var d struct {
			Type           string `json:"type"`
			Subtype        string `json:"subtype"`
			IsMeta         bool   `json:"isMeta"`
			AITitle        string `json:"aiTitle"`
			GitBranch      string `json:"gitBranch"`
			PermissionMode string `json:"permissionMode"`
			Message        struct {
				Model string `json:"model"`
				Usage struct {
					InputTokens         int64 `json:"input_tokens"`
					CacheReadTokens     int64 `json:"cache_read_input_tokens"`
					CacheCreationTokens int64 `json:"cache_creation_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &d); err != nil {
			continue
		}
		if d.AITitle != "" {
			s.Title = d.AITitle
		}
		if d.GitBranch != "" {
			s.Branch = d.GitBranch
		}
		if d.PermissionMode != "" {
			s.PermissionMode = d.PermissionMode
		}
		if d.Type == "assistant" && d.Message.Model != "" {
			s.Model = d.Message.Model
			u := d.Message.Usage
			if total := u.InputTokens + u.CacheReadTokens + u.CacheCreationTokens; total > 0 {
				s.ContextTokens = total
			}
		}
		// Track the last substantive line to classify turn state. Meta and
		// bookkeeping lines don't tell us whether a turn is open.
		switch {
		case d.IsMeta:
		case d.Type == "system" && d.Subtype == "turn_duration":
			s.lastKind = "turn_end"
		case d.Type == "user" || d.Type == "assistant" || d.Type == "attachment":
			s.lastKind = "turn_open"
		}
	}
}

// envWithoutTmux returns the current environment minus TMUX/TMUX_PANE.
func EnvWithoutTmux() []string {
	var env []string
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "TMUX=") || strings.HasPrefix(e, "TMUX_PANE=") {
			continue
		}
		env = append(env, e)
	}
	return env
}

var mungeRe = regexp.MustCompile(`[/.]`)

// MungeProjectDir converts a cwd into Claude Code's projects-dir name
// (slashes and dots become dashes).
func MungeProjectDir(cwd string) string {
	return mungeRe.ReplaceAllString(cwd, "-")
}
