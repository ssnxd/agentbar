package daemon

import (
	"sort"
	"time"

	"github.com/ssnxd/agentbar/internal/paths"
	"github.com/ssnxd/agentbar/internal/state"
	"github.com/ssnxd/agentbar/internal/tmuxctl"
)

// View is who is looking at what, right now.
type View struct {
	// Focused: a terminal showing tmux has the keyboard focus.
	Focused bool
	// Visible: the panes on screen in a focused terminal.
	Visible map[string]bool
	// Front: the active pane of the window each attached client shows,
	// focused or not. What is written there reaches the terminal.
	Front []string
}

// Look works out the view from the attached clients and the panes of a
// snapshot. A zoomed window shows only its active pane.
func Look(r tmuxctl.Runner, panes map[string]tmuxctl.Pane) View {
	v := View{Visible: map[string]bool{}}
	cs, err := tmuxctl.ListClients(r)
	if err != nil {
		return v
	}
	attached, focused := map[string]bool{}, map[string]bool{}
	for _, c := range cs {
		attached[c.Session] = true
		if c.Focused {
			focused[c.Session] = true
			v.Focused = true
		}
	}
	for _, p := range panes {
		if !p.WindowActive || !attached[p.SessionName] {
			continue
		}
		if p.Active {
			v.Front = append(v.Front, p.PaneID)
		}
		if focused[p.SessionName] && (p.Active || !p.Zoomed) {
			v.Visible[p.PaneID] = true
		}
	}
	sort.Strings(v.Front)
	return v
}

// seenDwell is how long a pane must stay on screen before what finished in
// it counts as seen: passing through a window does not read it.
const seenDwell = time.Second

// Seen turns a finished session into a waiting one once you have seen it:
// its pane was on screen, in a focused terminal, for seenDwell. A session
// that finishes in front of you is seen at once and never shows as done.
type Seen struct {
	marks map[string]time.Time // session id → when it was last seen
	since map[string]time.Time // pane id → on screen since
}

// Apply rewrites the done sessions of s that have been seen.
func (t *Seen) Apply(s *Snapshot, v View) {
	dir := paths.StateDir()
	if t.marks == nil {
		t.marks, t.since = state.ReadSeen(dir), map[string]time.Time{}
	}
	for id := range t.since {
		if !v.Visible[id] {
			delete(t.since, id)
		}
	}
	for id := range v.Visible {
		if _, ok := t.since[id]; !ok {
			t.since[id] = s.At
		}
	}
	live := make(map[string]bool, len(s.Sessions))
	for i := range s.Sessions {
		ss := &s.Sessions[i]
		live[ss.ID] = true
		if ss.Status != state.StatusDone {
			continue
		}
		// outside tmux there is no telling what you see: do not hold a
		// session as unseen for ever
		if !ss.InTmux {
			ss.Status = state.StatusWaiting
			continue
		}
		if m, ok := t.marks[ss.ID]; ok && !m.Before(ss.StatusSince) {
			ss.Status = state.StatusWaiting
			continue
		}
		if at, ok := t.since[ss.TmuxPaneID]; ok && s.At.Sub(at) >= seenDwell {
			t.marks[ss.ID] = s.At
			_ = state.MarkSeen(dir, ss.ID, s.At)
			ss.Status = state.StatusWaiting
		}
	}
	for id := range t.marks {
		if !live[id] {
			delete(t.marks, id)
		}
	}
}
