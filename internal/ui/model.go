// Package ui is the sidebar: a Bubble Tea app that polls session state once
// a second and offers jump, accept, kill, and filter.
package ui

import (
	"fmt"
	"os"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/ssnxd/agentbar/internal/paths"
	"github.com/ssnxd/agentbar/internal/registry"
	"github.com/ssnxd/agentbar/internal/session"
	"github.com/ssnxd/agentbar/internal/state"
	"github.com/ssnxd/agentbar/internal/tmuxctl"
	"github.com/ssnxd/agentbar/internal/transcript"
)

const (
	pollEvery = time.Second
	msgFor    = 3 * time.Second
	sweepAge  = time.Hour
)

type (
	tickMsg     struct{}
	snapshotMsg struct {
		sessions []session.Session
		panes    map[string]tmuxctl.Pane
		err      error
	}
	actionMsg struct {
		text string
		err  error
	}
)

// Model is the whole sidebar state. Rendering is pure over it.
type Model struct {
	runner tmuxctl.Runner
	opts   tmuxctl.Opts

	sessions []session.Session
	panes    map[string]tmuxctl.Pane
	selected string // session id, so the cursor survives reorders
	scroll   int

	filter      string
	filtering   bool
	helpOpen    bool
	confirmKill bool
	msg         string
	msgUntil    time.Time

	width, height int
	tick          int
	now           time.Time
	err           error
}

// Run launches the sidebar (blocking).
func Run() {
	r := tmuxctl.Exec{}
	m := Model{runner: r, opts: tmuxctl.LoadOpts(r), now: time.Now()}
	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "agentbar:", err)
		os.Exit(1)
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(refresh(m.runner), tick())
}

func tick() tea.Cmd {
	return tea.Tick(pollEvery, func(time.Time) tea.Msg { return tickMsg{} })
}

// refresh does all IO off the UI goroutine and returns one snapshot.
func refresh(r tmuxctl.Runner) tea.Cmd {
	return func() tea.Msg {
		now := time.Now()
		reg := registry.Load(paths.SessionsDir(), registry.Alive)
		live := map[string]bool{}
		var interactive []registry.Entry
		for _, e := range reg {
			live[e.SessionID] = true
			if e.Kind == "" || e.Kind == "interactive" {
				interactive = append(interactive, e)
			}
		}
		states := state.ReadAll(paths.StateDir())
		state.Sweep(paths.StateDir(), live, sweepAge, now)

		panes := map[string]tmuxctl.Pane{}
		ps, perr := tmuxctl.ListPanes(r)
		for _, p := range ps {
			panes[p.PaneID] = p
		}
		ss := session.Build(session.Deps{
			Registry: interactive,
			States:   states,
			Transcript: func(cwd, id string) transcript.Info {
				p := transcript.Newest(paths.ProjectsDir(), cwd, id)
				if p == "" {
					return transcript.Info{}
				}
				return transcript.Tail(p)
			},
			Now: now,
		})
		for i := range ss {
			if p, ok := panes[ss[i].TmuxPaneID]; ok {
				ss[i].TmuxTarget = tmuxctl.TargetLabel(p)
			} else {
				ss[i].InTmux = false
			}
		}
		return snapshotMsg{sessions: ss, panes: panes, err: perr}
	}
}

// current returns the selected session.
func (m Model) current() (session.Session, bool) {
	for _, s := range m.ordered() {
		if s.ID == m.selected {
			return s, true
		}
	}
	return session.Session{}, false
}

func (m *Model) ensureSelection() {
	rows := m.ordered()
	if len(rows) == 0 {
		m.selected = ""
		return
	}
	for _, s := range rows {
		if s.ID == m.selected {
			return
		}
	}
	m.selected = rows[0].ID
}

func (m *Model) move(delta int) {
	rows := m.ordered()
	if len(rows) == 0 {
		return
	}
	idx := 0
	for i, s := range rows {
		if s.ID == m.selected {
			idx = i
			break
		}
	}
	idx += delta
	if idx < 0 {
		idx = 0
	}
	if idx >= len(rows) {
		idx = len(rows) - 1
	}
	m.selected = rows[idx].ID
}

// nextHot selects the next needs-you row after the current one, wrapping.
func (m *Model) nextHot() {
	rows := m.ordered()
	start := 0
	for i, s := range rows {
		if s.ID == m.selected {
			start = i + 1
			break
		}
	}
	for k := 0; k < len(rows); k++ {
		s := rows[(start+k)%len(rows)]
		if s.Status == state.StatusNeedsYou {
			m.selected = s.ID
			return
		}
	}
}

func (m *Model) flash(text string) {
	m.msg = text
	m.msgUntil = time.Now().Add(msgFor)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tickMsg:
		m.tick++
		m.now = time.Now()
		return m, tea.Batch(refresh(m.runner), tick())
	case snapshotMsg:
		m.sessions, m.panes, m.err = msg.sessions, msg.panes, msg.err
		m.ensureSelection()
		return m, nil
	case actionMsg:
		if msg.err != nil {
			m.flash(msg.err.Error())
		} else if msg.text != "" {
			m.flash(msg.text)
		}
		return m, refresh(m.runner)
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m Model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	if k == "ctrl+c" {
		return m, tea.Quit
	}
	if m.filtering {
		switch k {
		case "enter":
			m.filtering = false
		case "esc":
			m.filtering = false
			m.filter = ""
		case "backspace":
			if r := []rune(m.filter); len(r) > 0 {
				m.filter = string(r[:len(r)-1])
			}
		default:
			if msg.Text != "" {
				m.filter += msg.Text
			}
		}
		m.ensureSelection()
		return m, nil
	}
	if m.confirmKill {
		m.confirmKill = false
		if k == "y" {
			return m, m.kill()
		}
		m.flash("kill cancelled")
		return m, nil
	}
	switch k {
	case "q":
		return m, tea.Quit
	case "j", "down":
		m.move(1)
	case "k", "up":
		m.move(-1)
	case "g", "home":
		m.move(-1 << 20)
	case "G", "end":
		m.move(1 << 20)
	case "tab":
		m.nextHot()
	case "enter":
		return m, m.jump()
	case "y":
		return m, m.accept()
	case "x":
		if _, ok := m.current(); ok {
			m.confirmKill = true
		}
	case "/":
		m.filtering = true
	case "esc":
		m.filter = ""
		m.helpOpen = false
		m.ensureSelection()
	case "r":
		return m, refresh(m.runner)
	case "?":
		m.helpOpen = !m.helpOpen
	}
	return m, nil
}

func (m Model) jump() tea.Cmd {
	cur, ok := m.current()
	if !ok {
		return nil
	}
	if !cur.InTmux {
		return func() tea.Msg { return actionMsg{text: "not in tmux: cannot jump"} }
	}
	r, o, pane := m.runner, m.opts, cur.TmuxPaneID
	return func() tea.Msg {
		return actionMsg{err: tmuxctl.Jump(r, o, pane)}
	}
}

func (m Model) accept() tea.Cmd {
	cur, ok := m.current()
	if !ok {
		return nil
	}
	if cur.Status != state.StatusNeedsYou {
		return func() tea.Msg { return actionMsg{text: "nothing to accept: not waiting on you"} }
	}
	p, inTmux := m.panes[cur.TmuxPaneID]
	if !cur.InTmux || !inTmux {
		return func() tea.Msg { return actionMsg{text: "not in tmux: cannot send keys"} }
	}
	if !tmuxctl.LooksLikeClaude(p.Command) {
		return func() tea.Msg { return actionMsg{text: "pane is running " + p.Command + ", not claude"} }
	}
	r, pane, title := m.runner, cur.TmuxPaneID, cur.Title
	return func() tea.Msg {
		if err := tmuxctl.SendEnter(r, pane); err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{text: "accepted: " + title}
	}
}

func (m Model) kill() tea.Cmd {
	cur, ok := m.current()
	if !ok {
		return nil
	}
	pid, title := cur.PID, cur.Title
	return func() tea.Msg {
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
			return actionMsg{err: fmt.Errorf("kill %d: %w", pid, err)}
		}
		return actionMsg{text: "sent SIGTERM: " + title}
	}
}

func (m Model) View() tea.View {
	v := tea.NewView(Render(m))
	v.AltScreen = true
	return v
}
