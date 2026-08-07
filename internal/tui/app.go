// Package tui is the human-facing dashboard: tasks, agents, live status,
// attach/detach, recovery, and archival.
package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/ssnxd/workflow/internal/cli"
	"github.com/ssnxd/workflow/internal/config"
	"github.com/ssnxd/workflow/internal/db"
	"github.com/ssnxd/workflow/internal/external"
	"github.com/ssnxd/workflow/internal/gitx"
	"github.com/ssnxd/workflow/internal/manager"
	"github.com/ssnxd/workflow/internal/notify"
	"github.com/ssnxd/workflow/internal/tmux"
)

type screen int

const (
	screenDashboard screen = iota
	screenTask
	screenNewTask
	screenDiff
)

// snapshot is one consistent read of the world, produced off the UI thread
// once per tick and delivered as a single message.
type snapshot struct {
	tasks    []db.Task
	agents   map[int64][]db.Agent
	preview  string // capture-pane of the selected agent (task view only)
	stats    map[int64][3]int
	external []external.Session // claude sessions we do not manage
	extFresh bool               // whether this refresh recomputed external
	err      error
}

type (
	tickMsg     struct{}
	snapshotMsg snapshot
	attachedMsg struct{ err error }
	taskMadeMsg struct {
		taskID int64
		err    error
	}
	actionErrMsg struct{ err error }
	landedMsg    struct {
		what string
		err  error
	}
)

type App struct {
	mgr *manager.Manager

	scr      screen
	dash     dashModel
	taskV    taskModel
	diff     diffModel
	form     formModel
	snap     snapshot
	width    int
	height   int
	ticks    int
	helpOpen bool   // which-key expanded panel
	status   string // transient status/error line
}

// Run launches the TUI (blocking). Preflight failures print and exit.
func Run() {
	ver, err := tmux.ServerVersion()
	if err != nil {
		fmt.Fprintln(os.Stderr, "workflow: tmux is required but not found. Install tmux >= 3.2.")
		os.Exit(1)
	}
	if err := cli.CheckTmuxVersion(ver); err != nil {
		fmt.Fprintln(os.Stderr, "workflow:", err)
		os.Exit(1)
	}
	p, cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "workflow:", err)
		os.Exit(1)
	}
	store, err := db.Open(p.DBPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "workflow:", err)
		os.Exit(1)
	}
	defer store.Close()
	mgr, err := manager.New(p, cfg, store)
	if err != nil {
		fmt.Fprintln(os.Stderr, "workflow:", err)
		os.Exit(1)
	}

	app := App{mgr: mgr, form: newForm(mgr)}
	if _, err := tea.NewProgram(app).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "workflow:", err)
		os.Exit(1)
	}
}

func (a App) Init() tea.Cmd {
	return tea.Batch(a.refreshCmd(), tickCmd())
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

// refreshCmd does all periodic IO off the UI goroutine: reconcile, reload,
// capture the preview pane, compute diff stats for the open task. External
// session discovery (ps + lsof) runs only every fifth tick.
func (a App) refreshCmd() tea.Cmd {
	mgr := a.mgr
	scr := a.scr
	taskID := a.taskV.taskID
	selPane := a.taskV.selectedPane(a.snap)
	scanExternal := a.ticks%5 == 0
	return func() tea.Msg {
		var s snapshot
		if scanExternal {
			s.external = external.Discover(mgr.Paths.WorktreesDir)
			s.extFresh = true
		}
		if err := mgr.Reconcile(); err != nil {
			s.err = err
		}
		tasks, err := mgr.Store.ListTasks(false)
		if err != nil {
			return snapshotMsg(snapshot{err: err})
		}
		s.tasks = tasks
		s.agents = make(map[int64][]db.Agent, len(tasks))
		for _, t := range tasks {
			agents, err := mgr.Store.ListAgents(t.ID)
			if err != nil {
				return snapshotMsg(snapshot{err: err})
			}
			s.agents[t.ID] = agents
		}
		if scr == screenTask {
			if selPane != "" {
				if out, err := mgr.Tmux.CapturePane(selPane); err == nil {
					s.preview = out
				}
			}
			s.stats = make(map[int64][3]int)
			for _, t := range tasks {
				if t.ID != taskID {
					continue
				}
				for _, ag := range s.agents[t.ID] {
					if ag.Role == db.RoleWorker {
						f, i, d := gitx.ShortStat(t.ProjectPath, t.Branch, ag.Branch)
						s.stats[ag.ID] = [3]int{f, i, d}
					}
				}
			}
		}
		return snapshotMsg(s)
	}
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		a.form.setSize(msg.Width, msg.Height)
		return a, nil

	case tickMsg:
		a.ticks++
		return a, tea.Batch(a.refreshCmd(), tickCmd())

	case snapshotMsg:
		s := snapshot(msg)
		if s.err != nil {
			a.status = s.err.Error()
		}
		a.notifyTransitions(s)
		// Keep stats/preview if this refresh didn't compute them.
		if s.stats == nil {
			s.stats = a.snap.stats
		}
		if s.preview == "" {
			s.preview = a.snap.preview
		}
		if !s.extFresh {
			s.external = a.snap.external
		}
		a.snap = s
		return a, nil

	case attachedMsg:
		if msg.err != nil {
			a.status = "attach: " + msg.err.Error()
		}
		// Repaint correctly after the terminal round-trip.
		return a, tea.Batch(func() tea.Msg { return tea.RequestWindowSize() }, a.refreshCmd())

	case taskMadeMsg:
		a.form.busy = false
		if msg.err != nil {
			a.status = "new task: " + msg.err.Error()
			return a, nil
		}
		a.scr = screenTask
		a.taskV = newTaskView(msg.taskID)
		return a, a.refreshCmd()

	case actionErrMsg:
		if msg.err != nil {
			a.status = msg.err.Error()
		}
		return a, a.refreshCmd()

	case diffLoadedMsg:
		return a.updateDiffLoaded(msg)

	case landedMsg:
		a.taskV.busy = ""
		if msg.err != nil {
			a.status = msg.err.Error()
		} else {
			a.status = "✓ " + msg.what
			notify.Send("workflow", msg.what)
		}
		return a, a.refreshCmd()

	case reposScannedMsg:
		a.form.all = msg
		a.form.scanned = true
		a.form.refilter()
		// The user's fzf is the picker of choice when installed; the
		// built-in list is the fallback.
		if a.scr == screenNewTask && a.form.step == 0 && fzfAvailable() {
			return a, a.launchFzf()
		}
		return a, nil

	case fzfDoneMsg:
		if a.scr != screenNewTask {
			return a, nil
		}
		sel := strings.TrimSpace(msg.out)
		if msg.err != nil || sel == "" { // cancelled or no match
			a.scr = screenDashboard
			return a, tea.Batch(func() tea.Msg { return tea.RequestWindowSize() }, a.refreshCmd())
		}
		fields := strings.Split(sel, "\t")
		if len(fields) < 2 {
			a.scr = screenDashboard
			return a, nil
		}
		for _, r := range a.form.all {
			if r.Path == fields[1] {
				a.form.repo = r
				break
			}
		}
		a.form.step = 1
		a.form.focus = 0
		return a, tea.Batch(func() tea.Msg { return tea.RequestWindowSize() },
			a.form.title.Focus())

	case tea.KeyPressMsg:
		a.status = ""
		if msg.String() == "?" && a.scr != screenNewTask {
			a.helpOpen = !a.helpOpen
			return a, nil
		}
		if a.helpOpen {
			a.helpOpen = false // any other key closes help and still acts
		}
		switch a.scr {
		case screenDashboard:
			return a.updateDashboard(msg)
		case screenTask:
			return a.updateTask(msg)
		case screenNewTask:
			return a.updateForm(msg)
		case screenDiff:
			return a.updateDiff(msg)
		}
	}

	if a.scr == screenNewTask {
		var cmd tea.Cmd
		a.form, cmd = a.form.update(msg)
		return a, cmd
	}
	return a, nil
}

func (a App) View() tea.View {
	w, h := a.width, a.height
	if w <= 0 {
		w, h = 80, 24
	}

	var content, crumb, right string
	switch a.scr {
	case screenDashboard:
		content = a.viewDashboard(w)
		crumb = "dashboard"
		right = a.dashSummary()
	case screenTask:
		content = a.viewTask(w, h)
		if t, ok := a.currentTask(); ok {
			crumb = fmt.Sprintf("task #%d · %s", t.ID, truncate(t.Title, 30))
			right = t.ProjectName + " · " + t.Branch
		} else {
			crumb = "task"
		}
	case screenNewTask:
		content = a.form.view()
		crumb = "new task"
	case screenDiff:
		content = a.viewDiff(w, h)
		crumb = fmt.Sprintf("diff · task #%d", a.diff.taskID)
	}

	head := headerBar(w, crumb, right)
	foot := a.footer(w)
	extra := ""
	if a.status != "" {
		extra = " " + sError.Render(truncate(a.status, w-2)) + "\n"
	}
	if a.helpOpen {
		extra += a.helpOverlay(w) + "\n"
	}

	used := lipgloss.Height(head) + lipgloss.Height(content) +
		lipgloss.Height(foot) + strings.Count(extra, "\n")
	fill := h - used - 1
	if fill < 0 {
		fill = 0
	}
	body := head + "\n" + content + strings.Repeat("\n", fill+1) + extra + foot

	v := tea.NewView(body)
	v.AltScreen = true
	v.WindowTitle = "workflow"
	return v
}

// notifyTransitions fires a desktop notification on the transitions a
// detached human cares about: a task turning ready for review, an agent
// starting to need attention. Compared against the previous snapshot so
// only edges notify, never states — and never on the first load.
func (a App) notifyTransitions(next snapshot) {
	if a.snap.agents == nil {
		return
	}
	prevTask := make(map[int64]string, len(a.snap.tasks))
	for _, t := range a.snap.tasks {
		prevTask[t.ID] = t.Status
	}
	for _, t := range next.tasks {
		if t.Status == "done" && prevTask[t.ID] == "active" {
			notify.Send("workflow", fmt.Sprintf("task #%d %q is ready for review", t.ID, t.Title))
		}
	}
	prevAgent := make(map[int64]string)
	for _, agents := range a.snap.agents {
		for _, ag := range agents {
			prevAgent[ag.ID] = ag.Status
		}
	}
	for taskID, agents := range next.agents {
		for _, ag := range agents {
			was, known := prevAgent[ag.ID]
			if !known || was == ag.Status {
				continue
			}
			switch ag.Status {
			case db.StatusNeedsYou:
				notify.Send("workflow", fmt.Sprintf("%s (task #%d) needs you", ag.Name, taskID))
			case db.StatusError, db.StatusDead:
				notify.Send("workflow", fmt.Sprintf("%s (task #%d) %s", ag.Name, taskID, ag.Status))
			}
		}
	}
}

// dashSummary is the header's right side: fleet-wide counts.
func (a App) dashSummary() string {
	working, needs := 0, 0
	for _, agents := range a.snap.agents {
		for _, ag := range agents {
			switch ag.Status {
			case db.StatusWorking, db.StatusStarting:
				working++
			case db.StatusNeedsYou, db.StatusError, db.StatusDead:
				needs++
			}
		}
	}
	s := fmt.Sprintf("%d tasks · %d working", len(a.snap.tasks), working)
	if needs > 0 {
		s += fmt.Sprintf(" · %d need you", needs)
	}
	if n := len(a.snap.external); n > 0 {
		s += fmt.Sprintf(" · %d external", n)
	}
	return s
}

// attachCmd hands the real terminal to a nested tmux client on our dedicated
// server. Works identically whether workflow runs in a plain terminal or
// inside the user's own tmux (TMUX is stripped in AttachCmd).
func (a App) attachCmd(sessionID, windowID string) tea.Cmd {
	c := a.mgr.Tmux.AttachCmd(sessionID, windowID)
	return tea.ExecProcess(c, func(err error) tea.Msg { return attachedMsg{err} })
}
