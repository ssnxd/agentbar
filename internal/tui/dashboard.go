package tui

import (
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	tea "charm.land/bubbletea/v2"
	"github.com/ssnxd/workflow/internal/db"
	"github.com/ssnxd/workflow/internal/external"
	"github.com/ssnxd/workflow/internal/tmux"
)

type dashModel struct {
	cursor     int
	confirming bool // pending quit-task confirmation
}

func (a App) updateDashboard(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	nTasks := len(a.snap.tasks)
	total := nTasks + len(a.snap.external)

	if a.dash.confirming {
		a.dash.confirming = false
		if msg.String() == "y" && a.dash.cursor < nTasks {
			mgr, id := a.mgr, a.snap.tasks[a.dash.cursor].ID
			return a, func() tea.Msg { return actionErrMsg{mgr.Archive(id)} }
		}
		return a, nil
	}

	switch msg.String() {
	case "x":
		if a.dash.cursor < nTasks {
			a.dash.confirming = true
		}
		return a, nil
	case "q", "ctrl+c":
		return a, tea.Quit
	case "up", "k":
		if a.dash.cursor > 0 {
			a.dash.cursor--
		}
	case "down", "j":
		if a.dash.cursor < total-1 {
			a.dash.cursor++
		}
	case "enter":
		switch {
		case a.dash.cursor < nTasks:
			t := a.snap.tasks[a.dash.cursor]
			a.scr = screenTask
			a.taskV = newTaskView(t.ID)
			return a, a.refreshCmd()
		case a.dash.cursor < total:
			return a.jumpExternal(a.snap.external[a.dash.cursor-nTasks])
		}
	case "n":
		a.scr = screenNewTask
		a.form.reset()
		return a, a.form.init()
	}
	return a, nil
}

// jumpExternal takes the user to an external claude session running in their
// own tmux.
//
//   - TUI inside tmux (the common case): the session shares the user's default
//     server, so switch-client moves this very terminal there. Attaching
//     nested to a session we are already inside would recurse infinitely —
//     switching is the only correct move. The user returns with their own
//     tmux bindings (e.g. last-session / last-window).
//   - TUI outside tmux: nested-attach the user's default server via
//     ExecProcess; detaching lands back in the TUI as usual.
func (a App) jumpExternal(e external.Session) (tea.Model, tea.Cmd) {
	if !e.InTmux {
		a.status = fmt.Sprintf("pid %d runs outside tmux (on %s) — nothing to attach to", e.PID, e.TTY)
		return a, nil
	}
	target := e.TmuxSession + ":" + e.TmuxWindow
	if tmux.InsideTmux() {
		// Same-server move. If this fails, the TUI sits on a different tmux
		// server than the target — fall through to a nested attach.
		if err := exec.Command("tmux", "switch-client", "-t", target).Run(); err == nil {
			_ = exec.Command("tmux", "select-pane", "-t", e.TmuxPane).Run()
			a.status = fmt.Sprintf("switched to %s — return with your usual tmux bindings", target)
			return a, nil
		}
	}
	c := exec.Command("tmux", "attach-session", "-t", e.TmuxSession, ";",
		"select-window", "-t", target, ";", "select-pane", "-t", e.TmuxPane)
	c.Env = external.EnvWithoutTmux()
	return a, tea.ExecProcess(c, func(err error) tea.Msg { return attachedMsg{err} })
}

// bannerArt: "wf" in shadow blocks. Kept narrow so it fits beside the info
// column even in an 80-col terminal.
var bannerArt = []string{
	`██╗    ██╗ ███████╗`,
	`██║ █╗ ██║ ██╔════╝`,
	`██║███╗██║ █████╗  `,
	`╚███╔███╔╝ ██╔══╝  `,
	` ╚══╝╚══╝  ██║     `,
	`           ╚═╝     `,
}

var bannerTips = []string{
	"press ? on any screen — the keys always tell you what works right now",
	"agents live in tmux: kill this TUI any time, nothing dies",
	"a ✋ needs-you badge means an agent is blocked on a human decision",
	"archive keeps branches — the task branch is the reviewable artifact",
	"enter on an external session jumps straight to it in your tmux",
	"tmux -L workflow ls — peek behind the curtain",
}

func (a App) banner() string {
	grad := []color.Color{cBlue, cBlue, cCyan, cCyan, cPurple, cPurple}
	var art strings.Builder
	for i, l := range bannerArt {
		art.WriteString(lipgloss.NewStyle().Foreground(grad[i]).Render(l) + "\n")
	}
	hour := time.Now().Hour()
	greet := "good evening"
	switch {
	case hour < 5:
		greet = "up late"
	case hour < 12:
		greet = "good morning"
	case hour < 18:
		greet = "good afternoon"
	}
	user := os.Getenv("USER")
	if user == "" {
		user = "operator"
	}
	info := "\n" + sPanelName.Render("workflow") + sDim.Render(" — Claude Code fleets on tmux") + "\n" +
		sNormal.Render(greet+", ") + sPurple.Render(user) + "\n\n" +
		sDim.Render("tip: "+bannerTips[(a.ticks/8)%len(bannerTips)])
	return "\n" + lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().MarginLeft(2).Render(art.String()), "   ", info) + "\n"
}

func (a App) viewDashboard(width int) string {
	var b strings.Builder
	if a.height >= 26 || len(a.snap.tasks) == 0 {
		b.WriteString(a.banner())
	}
	row := func(i int, s string, hot bool) {
		st := sNormal
		if !hot {
			st = sDim
		}
		if i == a.dash.cursor {
			st = sSelected
		}
		b.WriteString(st.Width(width-2).Render(truncate(s, width-2)) + "\n")
	}

	b.WriteString("\n " + sKeyGroup.Render("tasks") + "\n")
	if len(a.snap.tasks) == 0 {
		b.WriteString(sDim.Render("   no active tasks — press ") +
			sKeyChip.Render("n") + sDim.Render(" to start one") + "\n")
	} else {
		b.WriteString(" " + sHeadRow.Render(fmt.Sprintf(" %-4s %-30s %-16s %-22s %-9s %s",
			"id", "title", "project", "agents", "cost", "attention")) + "\n")
	}
	for i, t := range a.snap.tasks {
		agents := a.snap.agents[t.ID]
		var working, needsYou, done int
		var cost float64
		for _, ag := range agents {
			switch ag.Status {
			case db.StatusWorking, db.StatusStarting:
				working++
			case db.StatusNeedsYou, db.StatusError, db.StatusDead:
				needsYou++
			case db.StatusDone:
				done++
			}
			cost += ag.CostUSD
		}
		attention := ""
		if needsYou > 0 {
			attention = sBadgeHot.Render(fmt.Sprintf("%d need you", needsYou))
		}
		if t.Status == "done" {
			attention = sBadgeDone.Render("✓ ready for review") + " " + attention
		}
		line := fmt.Sprintf("  #%-3d %-30s %-16s %d total · %d %s · %d ✓     $%-8.2f %s",
			t.ID, truncate(t.Title, 30), truncate(t.ProjectName, 16),
			len(agents), working, spinnerFrames[a.ticks%len(spinnerFrames)], done, cost, attention)
		row(i, line, true)
	}

	if len(a.snap.external) > 0 {
		b.WriteString("\n " + sKeyGroup.Render("external sessions") + " " +
			sBadgeExt.Render("not managed") + sDim.Render("  — enter jumps to them in your own tmux") + "\n")
		for i, e := range a.snap.external {
			where := "no tmux"
			if e.InTmux {
				where = e.TmuxSession + ":" + e.TmuxWindow
			}
			title := e.Title
			if title == "" {
				title = e.CWD
			}
			line := fmt.Sprintf("  ⬡ %s %-18s %-40s %-11s %-7s %-11s up %-8s",
				extStateCell(e.State, a.ticks), truncate(e.Project, 18), truncate(title, 40),
				where, shortModel(e.Model), shortPermMode(e.PermissionMode), humanSince(e.StartedAt))
			if !e.LastActivity.IsZero() {
				line += " · active " + humanSince(e.LastActivity) + " ago"
			}
			if e.ContextTokens > 0 {
				line += fmt.Sprintf(" · %dk ctx", e.ContextTokens/1000)
			}
			row(len(a.snap.tasks)+i, line, false)
		}
	}
	if a.dash.confirming && a.dash.cursor < len(a.snap.tasks) {
		t := a.snap.tasks[a.dash.cursor]
		b.WriteString("\n " + sBadgeHot.Render(fmt.Sprintf("quit task #%d %q?", t.ID, truncate(t.Title, 30))) +
			sNormal.Render(" kills its agents, removes worktrees, keeps branches — ") +
			sKeyChip.Render("y") + sNormal.Render(" confirm, any other key cancels") + "\n")
	}
	return b.String()
}

// extStateCell renders an external session's derived state, width-padded so
// the columns after it stay aligned despite ANSI codes.
func extStateCell(state string, tick int) string {
	var cell string
	switch state {
	case "working":
		cell = sAccent.Render(spinnerFrames[tick%len(spinnerFrames)] + " working")
	case "waiting":
		cell = sOk.Render("○ waiting")
	case "blocked":
		cell = lipgloss.NewStyle().Bold(true).Foreground(cYellow).Render("✋ blocked?")
	default:
		cell = sDim.Render("· unknown")
	}
	if pad := 11 - lipgloss.Width(cell); pad > 0 {
		cell += strings.Repeat(" ", pad)
	}
	return cell
}

// shortPermMode compresses permission modes for the table: "bypass", "accEdits".
func shortPermMode(m string) string {
	switch m {
	case "bypassPermissions":
		return "bypass"
	case "acceptEdits":
		return "accEdits"
	case "":
		return "-"
	default:
		return m
	}
}

// shortModel compresses "claude-fable-5" → "fable", "claude-haiku-4-5-…" → "haiku".
func shortModel(m string) string {
	m = strings.TrimPrefix(m, "claude-")
	if i := strings.IndexAny(m, "-"); i > 0 {
		return m[:i]
	}
	return m
}

// humanSince renders a compact age like 2h14m or 3m or 12s.
func humanSince(t time.Time) string {
	if t.IsZero() {
		return "?"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
