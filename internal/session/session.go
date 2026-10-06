// Package session merges Claude's registry, agentbar's hook state, and the
// transcript tail into the rows the sidebar shows. It is pure: all IO is
// injected through Deps so the merge rules are unit-testable.
package session

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/ssnxd/agentbar/internal/registry"
	"github.com/ssnxd/agentbar/internal/state"
	"github.com/ssnxd/agentbar/internal/transcript"
)

// Where a session's status came from, most to least trusted.
const (
	SourceHook       = "hook"
	SourceRegistry   = "registry"
	SourceTranscript = "transcript"
	SourceNone       = "none"
)

// workingWindow: with a turn open and a transcript write this recent, the
// session is working; silent longer than this it is probably on a prompt.
const workingWindow = 30 * time.Second

// Session is one row in the sidebar.
type Session struct {
	ID            string // Claude session id
	PID           int
	CWD           string
	Project       string // display name for the group header
	Name          string // Claude's own display name, e.g. "miivo-ef"
	Title         string // aiTitle → first prompt → Name
	Model         string
	Branch        string
	ContextTokens int64

	Status string // state.Status*
	Detail string // permission detail, error message, ...
	Source string // Source*
	// StatusSince is when a hook set the status; zero without hook data.
	StatusSince time.Time

	StartedAt    time.Time
	LastActivity time.Time

	InTmux       bool
	TmuxSession  string
	TmuxWindowID string
	TmuxPaneID   string
	TmuxTarget   string // "work:2.1" display label, filled in by the UI from tmux

	Agents []Agent // running subagents, oldest first
}

// Agent is one running subagent of a session.
type Agent struct {
	ID          string
	Type        string // Explore, Plan, general-purpose, a custom agent name
	Description string // Claude's short task label, once its meta file exists
	Model       string
	Tool        string // last tool called
	Detail      string // last tool summary, "Bash go test ./..."
	StartedAt   time.Time
	UpdatedAt   time.Time
}

// Deps are the inputs Build merges.
type Deps struct {
	Registry   []registry.Entry
	States     map[string]state.Record
	Agents     map[string][]state.Agent // by session id
	Transcript func(cwd, sessionID string) transcript.Info
	AgentMeta  func(cwd, sessionID, agentID string) transcript.Meta
	Now        time.Time
}

// Resolve decides a session's status: a hook record newer than the process
// start is exact; else the registry's own waiting flag; else the transcript
// heuristic; else unknown. Claude's "busy"/"idle" registry values are not
// used: they were observed stale.
func Resolve(e registry.Entry, rec state.Record, hasRec bool, ti transcript.Info, now time.Time) (status, detail, source string) {
	if hasRec && rec.UpdatedAt.After(e.StartedAt) {
		return rec.Status, rec.Detail, SourceHook
	}
	if e.Status == "waiting" && e.WaitingFor != "" {
		return state.StatusNeedsYou, e.WaitingFor, SourceRegistry
	}
	if ti.Known {
		switch {
		case !ti.TurnOpen:
			return state.StatusWaiting, "", SourceTranscript
		case now.Sub(ti.LastActivity) < workingWindow:
			return state.StatusWorking, "", SourceTranscript
		default:
			return state.StatusNeedsYou, "probably a prompt (no hook data)", SourceTranscript
		}
	}
	return state.StatusUnknown, "", SourceNone
}

// Build produces one Session per registry entry, in registry order.
func Build(d Deps) []Session {
	out := make([]Session, 0, len(d.Registry))
	for _, e := range d.Registry {
		rec, has := d.States[e.SessionID]
		ti := d.Transcript(e.CWD, e.SessionID)
		s := Session{
			ID: e.SessionID, PID: e.PID, CWD: e.CWD, Name: e.Name,
			Model: ti.Model, Branch: ti.Branch, ContextTokens: ti.ContextTokens,
			StartedAt:   e.StartedAt,
			InTmux:      e.TmuxPaneID != "",
			TmuxSession: e.TmuxSession, TmuxWindowID: e.TmuxWindowID, TmuxPaneID: e.TmuxPaneID,
		}
		s.Status, s.Detail, s.Source = Resolve(e, rec, has, ti, d.Now)
		if s.Source == SourceHook {
			s.StatusSince = rec.UpdatedAt
		}
		switch {
		case ti.Title != "":
			s.Title = ti.Title
		case ti.FirstPrompt != "":
			s.Title = ti.FirstPrompt
		default:
			s.Title = e.Name
		}
		s.LastActivity = latest(e.StartedAt, ti.LastActivity)
		if has {
			s.LastActivity = latest(s.LastActivity, rec.UpdatedAt)
		}
		for _, a := range d.Agents[e.SessionID] {
			ag := Agent{ID: a.AgentID, Type: a.Type, Tool: a.Tool, Detail: a.Detail, StartedAt: a.StartedAt, UpdatedAt: a.UpdatedAt}
			if d.AgentMeta != nil {
				m := d.AgentMeta(e.CWD, e.SessionID, a.AgentID)
				ag.Description, ag.Model = m.Description, m.Model
				if ag.Type == "" {
					ag.Type = m.Type
				}
			}
			s.Agents = append(s.Agents, ag)
		}
		out = append(out, s)
	}
	assignProjects(out)
	return out
}

// projectBase is the short group name for a cwd: its basename, or
// "<repo>/<worktree>" for Claude's own worktrees under <repo>/.claude/worktrees/.
func projectBase(cwd string) string {
	base := filepath.Base(cwd)
	parent := filepath.Dir(cwd)
	if filepath.Base(parent) == "worktrees" && filepath.Base(filepath.Dir(parent)) == ".claude" {
		repo := filepath.Base(filepath.Dir(filepath.Dir(parent)))
		return repo + "/" + base
	}
	return base
}

// assignProjects names each session's group by projectBase, adding the
// parent directory when two different cwds would share a name.
func assignProjects(ss []Session) {
	byName := map[string]map[string]bool{}
	for _, s := range ss {
		name := projectBase(s.CWD)
		if byName[name] == nil {
			byName[name] = map[string]bool{}
		}
		byName[name][s.CWD] = true
	}
	for i := range ss {
		name := projectBase(ss[i].CWD)
		if len(byName[name]) > 1 {
			name = filepath.Join(filepath.Base(filepath.Dir(ss[i].CWD)), filepath.Base(ss[i].CWD))
		}
		ss[i].Project = name
	}
}

func latest(ts ...time.Time) time.Time {
	var m time.Time
	for _, t := range ts {
		if t.After(m) {
			m = t
		}
	}
	return m
}

// Age is how long ago t was, coarse: "5m", "3h", "1d"; empty under a
// minute (it just happened) or for a zero time.
func Age(now, t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return ""
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours())/24)
}

// NeedYou counts sessions waiting on the user: "1 needs you", "2 need you".
func NeedYou(n int) string {
	if n == 1 {
		return "1 needs you"
	}
	return fmt.Sprintf("%d need you", n)
}
