package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/ssnxd/workflow/internal/db"
)

type taskModel struct {
	taskID     int64
	cursor     int
	confirming bool // pending archive confirmation
}

func newTaskView(taskID int64) taskModel { return taskModel{taskID: taskID} }

func (t taskModel) selectedPane(s snapshot) string {
	agents := s.agents[t.taskID]
	if t.cursor >= 0 && t.cursor < len(agents) {
		return agents[t.cursor].TmuxPaneID
	}
	return ""
}

func (a App) currentTask() (db.Task, bool) {
	for _, t := range a.snap.tasks {
		if t.ID == a.taskV.taskID {
			return t, true
		}
	}
	return db.Task{}, false
}

func (a App) updateTask(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	agents := a.snap.agents[a.taskV.taskID]

	if a.taskV.confirming {
		switch msg.String() {
		case "y":
			a.taskV.confirming = false
			mgr, id := a.mgr, a.taskV.taskID
			a.scr = screenDashboard
			return a, func() tea.Msg { return actionErrMsg{mgr.Archive(id)} }
		default:
			a.taskV.confirming = false
		}
		return a, nil
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return a, tea.Quit
	case "esc":
		a.scr = screenDashboard
		return a, nil
	case "up", "k":
		if a.taskV.cursor > 0 {
			a.taskV.cursor--
		}
		return a, a.refreshCmd()
	case "down", "j":
		if a.taskV.cursor < len(agents)-1 {
			a.taskV.cursor++
		}
		return a, a.refreshCmd()
	case "enter":
		if task, ok := a.currentTask(); ok && a.taskV.cursor < len(agents) {
			ag := agents[a.taskV.cursor]
			if ag.TmuxWindowID != "" {
				return a, a.attachCmd(task.TmuxSessionID, ag.TmuxWindowID)
			}
		}
	case "r":
		if a.taskV.cursor < len(agents) {
			ag := agents[a.taskV.cursor]
			if ag.Status == db.StatusDead || ag.Status == db.StatusError || ag.Status == db.StatusEnded {
				mgr := a.mgr
				return a, func() tea.Msg { return actionErrMsg{mgr.Recover(ag.ID)} }
			}
		}
	case "x":
		a.taskV.confirming = true
	}
	return a, nil
}

func (a App) viewTask(width, height int) string {
	task, ok := a.currentTask()
	if !ok {
		return sDim.Render("  task no longer active — esc to go back")
	}
	agents := a.snap.agents[task.ID]

	var b strings.Builder
	if task.Status == "done" {
		b.WriteString("\n " + sBadgeDone.Render("✓ ready for review") + " " +
			sNormal.Render("review branch ") + sPurple.Render(task.Branch) +
			sNormal.Render(" · then ") + sKeyChip.Render("x") + sNormal.Render(" archives (keeps branches)") + "\n")
		if task.Summary != "" {
			b.WriteString(" " + sDim.Render(truncate(task.Summary, max(20, (width-4)*2))) + "\n")
		}
	}
	b.WriteString("\n " + sKeyGroup.Render("agents") + "\n")
	b.WriteString(" " + sHeadRow.Render(fmt.Sprintf(" %-2s %-16s %-8s %-14s %-9s %-6s %s",
		"", "agent", "model", "status", "cost", "ctx", "committed diff")) + "\n")

	for i, ag := range agents {
		diff := sDim.Render("—")
		if st, ok := a.snap.stats[ag.ID]; ok && st[0] > 0 {
			diff = fmt.Sprintf("%d files %s %s", st[0],
				sOk.Render(fmt.Sprintf("+%d", st[1])), sError.Render(fmt.Sprintf("-%d", st[2])))
		}
		role := " "
		if ag.Role == db.RoleOrchestrator {
			role = sPurple.Render("◆")
		}
		status := statusCell(ag.Status, a.ticks)
		// Pad by display width: the styled cell contains ANSI codes, so
		// fmt's %-Ns would misalign the columns after it.
		if pad := 14 - lipgloss.Width(status); pad > 0 {
			status += strings.Repeat(" ", pad)
		}
		line := fmt.Sprintf("  %s %-16s %-8s %s $%-8.2f %3.0f%%   %s",
			role, truncate(ag.Name, 16), ag.Model, status,
			ag.CostUSD, ag.ContextPct, diff)
		st := sNormal
		if i == a.taskV.cursor {
			st = sSelected
		}
		b.WriteString(st.Width(width-2).Render(truncate(line, width-2)) + "\n")
		if ag.Summary != "" {
			b.WriteString("       " + sDim.Render("└ "+truncate(ag.Summary, max(20, width-12))) + "\n")
		}
	}

	if a.taskV.confirming {
		b.WriteString("\n " + sBadgeHot.Render("archive this task?") +
			sNormal.Render(" kills its tmux session, removes worktrees, keeps branches — ") +
			sKeyChip.Render("y") + sNormal.Render(" confirm, any other key cancels") + "\n")
	}

	// Live preview of the selected agent's pane.
	if a.snap.preview != "" {
		previewH := height - len(agents) - 10
		if a.taskV.cursor < len(agents) && previewH > 4 {
			lines := strings.Split(a.snap.preview, "\n")
			if len(lines) > previewH {
				lines = lines[len(lines)-previewH:]
			}
			w := max(20, width-4)
			name := agents[a.taskV.cursor].Name
			b.WriteString("\n" + lipgloss.NewStyle().MarginLeft(1).Render(
				panel("live · "+name, clipLines(lines, w-2), w, true)) + "\n")
		}
	}
	return b.String()
}

func statusPlain(status string) string {
	if l, ok := statusLooks[status]; ok {
		return l.label
	}
	return status
}

// clipLines hard-clips pane content to the preview box width, preserving a
// trailing reset so captured SGR sequences cannot bleed into the TUI.
func clipLines(lines []string, width int) string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = lipgloss.NewStyle().MaxWidth(width).Render(l) + "\x1b[0m"
	}
	return strings.Join(out, "\n")
}
