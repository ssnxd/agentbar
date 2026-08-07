package manager

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ssnxd/workflow/internal/db"
	"github.com/ssnxd/workflow/internal/gitx"
	"github.com/ssnxd/workflow/internal/msg"
	"github.com/ssnxd/workflow/internal/tmux"
)

// Reconcile compares DB intent against the tmux server (runtime truth) and
// downgrades agents whose panes are gone or dead. Called at TUI startup and
// on every poll tick. tmux "no server" simply means every agent is down —
// e.g. after a reboot — and must not be treated as an app error.
func (m *Manager) Reconcile() error {
	agents, err := m.Store.ListAllActiveAgents()
	if err != nil {
		return err
	}
	panes, err := m.Tmux.ListPanes()
	if err != nil {
		if _, ok := err.(tmux.ErrNoServer); !ok {
			return err
		}
		panes = nil
	}
	byPane := make(map[string]tmux.PaneInfo, len(panes))
	for _, p := range panes {
		byPane[p.PaneID] = p
	}
	for _, a := range agents {
		if a.Status == db.StatusDead || a.Status == db.StatusEnded {
			continue
		}
		// An empty pane ID means the agent is mid-creation (the row exists
		// before the tmux window does) — not dead. Never judge it.
		if a.TmuxPaneID == "" {
			continue
		}
		p, ok := byPane[a.TmuxPaneID]
		switch {
		case !ok:
			if err := m.Store.SetAgentStatus(a.ID, db.StatusDead); err != nil {
				return err
			}
			_ = m.Store.InsertEvent(a.ID, a.ClaudeSessionID, "pane_gone", "")
		case p.Dead:
			if err := m.Store.SetAgentStatus(a.ID, db.StatusDead); err != nil {
				return err
			}
			_ = m.Store.InsertEvent(a.ID, a.ClaudeSessionID, "pane_died",
				"exit status "+p.DeadStatus)
		case a.Status == db.StatusIdle:
			// Mail sweep: delivery must not depend on catching a single
			// Stop event. Any idle agent with pending mail gets re-nudged,
			// rate-limited per agent.
			m.sweepInbox(a)
		}
	}
	return nil
}

func (m *Manager) sweepInbox(a db.Agent) {
	inbox := m.Paths.InboxPath(a.TaskID, a.Name)
	if msg.Pending(inbox) == 0 {
		return
	}
	m.nudgeMu.Lock()
	last, seen := m.lastNudge[a.ID]
	if seen && time.Since(last) < 90*time.Second {
		m.nudgeMu.Unlock()
		return
	}
	m.lastNudge[a.ID] = time.Now()
	m.nudgeMu.Unlock()
	_ = m.Nudge(a)
}

// RecoverAllDead restarts every dead agent of a task. Used after reboots.
func (m *Manager) RecoverAllDead(taskID int64) (int, error) {
	agents, err := m.Store.ListAgents(taskID)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, a := range agents {
		if a.Status != db.StatusDead && a.Status != db.StatusError {
			continue
		}
		if err := m.Recover(a.ID); err != nil {
			return n, fmt.Errorf("recover %s: %w", a.Name, err)
		}
		n++
	}
	return n, nil
}

// Recover restarts a dead agent. Claude Code's own persistence does the heavy
// lifting: we recreate the pane and `claude --resume <session-id>`.
func (m *Manager) Recover(agentID int64) error {
	a, err := m.Store.GetAgent(agentID)
	if err != nil {
		return err
	}
	task, err := m.Store.GetTask(a.TaskID)
	if err != nil {
		return err
	}

	// Ensure the worktree still exists (it survives reboots; recreate if not).
	if _, statErr := os.Stat(a.WorktreePath); statErr != nil {
		base := task.Branch
		if a.Role == db.RoleOrchestrator {
			base = "HEAD"
		}
		if err := gitx.AddWorktree(task.ProjectPath, a.WorktreePath, a.Branch, base); err != nil {
			return fmt.Errorf("recreate worktree: %w", err)
		}
	}

	cmd := m.runAgentCmd(a.ID, true)
	panes, err := m.Tmux.ListPanes()
	if err != nil {
		if _, ok := err.(tmux.ErrNoServer); !ok {
			return err
		}
	}
	var pane *tmux.PaneInfo
	sessionAlive := false
	for i := range panes {
		if panes[i].PaneID == a.TmuxPaneID {
			pane = &panes[i]
		}
		if panes[i].SessionID == task.TmuxSessionID {
			sessionAlive = true
		}
	}
	switch {
	case pane != nil && pane.Dead:
		if err := m.Tmux.RespawnPane(a.TmuxPaneID, a.WorktreePath, cmd); err != nil {
			return err
		}
	case sessionAlive:
		winID, paneID, err := m.Tmux.NewWindow(task.TmuxSessionID, a.Name, a.WorktreePath, nil, cmd)
		if err != nil {
			return err
		}
		if err := m.Store.SetAgentTmux(a.ID, winID, paneID); err != nil {
			return err
		}
	default:
		sessID, winID, paneID, err := m.Tmux.NewSession(task.TmuxSessionName, a.WorktreePath, nil, cmd)
		if err != nil {
			return err
		}
		if err := m.Store.SetTaskSession(task.ID, sessID, task.TmuxSessionName); err != nil {
			return err
		}
		if err := m.Store.SetAgentTmux(a.ID, winID, paneID); err != nil {
			return err
		}
	}
	return m.Store.SetAgentStatus(a.ID, db.StatusStarting)
}

// Archive tears a task down: tmux session, worktrees, task state dir.
// Branches are kept — the task branch is the reviewable artifact.
func (m *Manager) Archive(taskID int64) error {
	task, err := m.Store.GetTask(taskID)
	if err != nil {
		return err
	}
	agents, err := m.Store.ListAgents(taskID)
	if err != nil {
		return err
	}
	if task.TmuxSessionID != "" {
		if err := m.Tmux.KillSession(task.TmuxSessionID); err != nil {
			if _, ok := err.(tmux.ErrNoServer); !ok {
				// Session may be gone already; log-worthy, not fatal.
				_ = m.Store.InsertEvent(0, "", "archive_kill_session_failed", err.Error())
			}
		}
	}
	for _, a := range agents {
		if a.WorktreePath != "" {
			if err := gitx.RemoveWorktree(task.ProjectPath, a.WorktreePath); err != nil {
				_ = m.Store.InsertEvent(a.ID, a.ClaudeSessionID, "archive_worktree_failed", err.Error())
			}
		}
		_ = m.Store.SetAgentStatus(a.ID, db.StatusEnded)
	}
	// git worktree remove deletes the checkouts; clear the empty per-task
	// parent dir as well. Task state (prompts, inbox history) is kept.
	_ = os.RemoveAll(filepath.Join(m.Paths.WorktreesDir, fmt.Sprint(taskID)))
	return m.Store.ArchiveTask(taskID)
}

// Nudge asks an (idle) agent to read its inbox. This is the only send-keys
// path in the whole app.
func (m *Manager) Nudge(a db.Agent) error {
	return m.Tmux.SendText(a.TmuxPaneID, "You have mail. Run `workflow msg read` and act on it.")
}

// SendToAgent delivers a message from the human/manager to an agent's inbox
// and nudges it if it is idle.
func (m *Manager) SendToAgent(taskID int64, agentName, from, body string) error {
	recipient, err := m.Store.GetAgentByName(taskID, agentName)
	if err != nil {
		return err
	}
	inbox := m.Paths.InboxPath(taskID, recipient.Name)
	if err := msg.Send(inbox, from, body); err != nil {
		return err
	}
	_ = m.Store.InsertMessage(taskID, from, recipient.Name, body)
	if recipient.Status == db.StatusIdle {
		_ = m.Nudge(recipient)
	}
	return nil
}
