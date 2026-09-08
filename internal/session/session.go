// Package session merges Claude's registry, agentbar's hook state, and the
// transcript tail into the rows the sidebar shows. It is pure: all IO is
// injected through Deps so the merge rules are unit-testable.
package session

import (
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

	StartedAt    time.Time
	LastActivity time.Time

	InTmux       bool
	TmuxSession  string
	TmuxWindowID string
	TmuxPaneID   string
	TmuxTarget   string // "work:2.1" display label, filled in by the UI from tmux
}

// Deps are the inputs Build merges.
type Deps struct {
	Registry   []registry.Entry
	States     map[string]state.Record
	Transcript func(cwd, sessionID string) transcript.Info
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
		out = append(out, s)
	}
	assignProjects(out)
	return out
}

// assignProjects names each session's group by its cwd basename, adding the
// parent directory when two different cwds share a basename.
func assignProjects(ss []Session) {
	byBase := map[string]map[string]bool{}
	for _, s := range ss {
		base := filepath.Base(s.CWD)
		if byBase[base] == nil {
			byBase[base] = map[string]bool{}
		}
		byBase[base][s.CWD] = true
	}
	for i := range ss {
		base := filepath.Base(ss[i].CWD)
		if len(byBase[base]) > 1 {
			ss[i].Project = filepath.Join(filepath.Base(filepath.Dir(ss[i].CWD)), base)
		} else {
			ss[i].Project = base
		}
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
