package daemon

import (
	"fmt"
	"os"

	"github.com/ssnxd/agentbar/internal/session"
	"github.com/ssnxd/agentbar/internal/state"
	"github.com/ssnxd/agentbar/internal/tmuxctl"
)

// tmux user options the daemon keeps current, for status lines:
//
//	@agentbar_hot     number of sessions that need you
//	@agentbar_status  "5 · 2 need you", "3 sessions", or "" when none
//	@agentbar_pill    the styled, clickable indicator; see pill.go
//
// and one the user sets: @agentbar-bell on rings the bell in a session's
// pane the moment it needs you, so tmux flags its window.
const (
	HotOption    = "@agentbar_hot"
	StatusOption = "@agentbar_status"
	BellOption   = "@agentbar-bell"
)

// Publisher pushes each snapshot's summary into tmux, touching tmux only
// when something changed.
type Publisher struct {
	lastHot    int
	lastStatus string
	lastPill   string
	started    bool
	prev       map[string]string // session id → status at the last publish
}

// Summary renders the status-line text for a snapshot.
func Summary(s Snapshot) (hot int, text string) {
	for _, ss := range s.Sessions {
		if ss.Status == state.StatusNeedsYou {
			hot++
		}
	}
	n := len(s.Sessions)
	switch {
	case n == 0:
		text = ""
	case hot > 0:
		text = fmt.Sprintf("%d · %s", n, session.NeedYou(hot))
	case n == 1:
		text = "1 session"
	default:
		text = fmt.Sprintf("%d sessions", n)
	}
	return hot, text
}

// Publish updates the tmux options for s and rings the bell for sessions
// that just started needing you, when @agentbar-bell is on.
func (p *Publisher) Publish(r tmuxctl.Runner, s Snapshot) {
	hot, text := Summary(s)
	if !p.started || hot != p.lastHot {
		_, _ = r.Run("set-option", "-g", HotOption, fmt.Sprint(hot))
	}
	if !p.started || text != p.lastStatus {
		_, _ = r.Run("set-option", "-g", StatusOption, text)
	}
	pill := Pill(s)
	if !p.started || pill != p.lastPill {
		_, _ = r.Run("set-option", "-g", PillOption, pill)
	}
	if !p.started || hot != p.lastHot || text != p.lastStatus || pill != p.lastPill {
		refreshStatus(r)
	}
	p.lastHot, p.lastStatus, p.lastPill = hot, text, pill

	cur := make(map[string]string, len(s.Sessions))
	var ring []string
	for _, ss := range s.Sessions {
		cur[ss.ID] = ss.Status
		if p.started && ss.Status == state.StatusNeedsYou && p.prev[ss.ID] != state.StatusNeedsYou && ss.InTmux {
			ring = append(ring, ss.TmuxPaneID)
		}
	}
	p.prev, p.started = cur, true
	if len(ring) == 0 {
		return
	}
	if v, err := r.Run("show-options", "-gqv", BellOption); err != nil || (v != "on" && v != "1") {
		return
	}
	for _, pane := range ring {
		bell(r, pane)
	}
}

// bell writes BEL to the pane's tty so tmux marks the window (monitor-bell).
func bell(r tmuxctl.Runner, pane string) {
	tty, err := r.Run("display-message", "-p", "-t", pane, "#{pane_tty}")
	if err != nil || tty == "" {
		return
	}
	f, err := os.OpenFile(tty, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write([]byte{'\a'})
}

// Clear empties the published options, so a status line never shows the
// last state of a daemon that is gone.
func (p *Publisher) Clear(r tmuxctl.Runner) {
	_, _ = r.Run("set-option", "-g", HotOption, "0")
	_, _ = r.Run("set-option", "-g", StatusOption, "")
	_, _ = r.Run("set-option", "-g", PillOption, "")
	refreshStatus(r)
	*p = Publisher{}
}
