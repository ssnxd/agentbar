// Package ui is the sidebar viewer: a Bubble Tea app fed by the daemon's
// snapshots, offering jump, accept, kill, and filter. One runs per window.
package ui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/ssnxd/agentbar/internal/daemon"
	"github.com/ssnxd/agentbar/internal/session"
	"github.com/ssnxd/agentbar/internal/state"
	"github.com/ssnxd/agentbar/internal/tmuxctl"
)

const (
	tickEvery = 500 * time.Millisecond
	msgFor    = 3 * time.Second
)

type (
	tickMsg     struct{}
	snapshotMsg daemon.Snapshot
	actionMsg   struct {
		text string
		err  error
		quit bool // the action finished what the popup was opened for
	}
)

// Options select how the viewer runs. The zero value is a sidebar pane.
//
// A popup is a picker: a search line on top, the list below. Every
// printable key goes to the search, so its actions sit on control keys.
type Options struct {
	Popup  bool   // a popup over the client: closes once it has taken you somewhere
	Client string // popup: the tmux client it floats over
	Here   string // popup: the pane the user was in when they opened it
}

// Model is the whole viewer state. Rendering is pure over it.
type Model struct {
	runner  tmuxctl.Runner
	snaps   <-chan daemon.Snapshot
	exe     string
	ownPane string // TMUX_PANE of this viewer
	popup   bool   // see Options
	client  string
	opened  string // popup: Options.Here

	sessions []session.Session
	panes    map[string]tmuxctl.Pane
	selected string // session id, so the cursor survives reorders
	moved    bool   // the user has moved the cursor; stop homing it
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
	connected     bool
	here          string // pane id of the session pane in this viewer's window
}

// herePane finds the active non-sidebar pane in this viewer's own window:
// the pane the user is in when they look at this sidebar.
func (m Model) herePane() string {
	if m.popup {
		return m.opened
	}
	own, ok := m.panes[m.ownPane]
	if !ok {
		return ""
	}
	var first string
	for _, p := range m.panes {
		if p.WindowID != own.WindowID || p.Sidebar {
			continue
		}
		if p.Active {
			return p.PaneID
		}
		if first == "" {
			first = p.PaneID
		}
	}
	return first
}

// Run launches the viewer (blocking). It connects to the daemon, starting
// one if none answers.
func Run(o Options) {
	exe, _ := os.Executable()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := make(chan daemon.Snapshot, 1)
	go daemon.Connect(ctx, daemon.SocketPath(), ch, func() error { return daemon.StartDetached(exe) })
	m := Model{runner: tmuxctl.Exec{}, snaps: ch, exe: exe, ownPane: os.Getenv("TMUX_PANE"), now: time.Now(),
		popup: o.Popup, client: o.Client, opened: o.Here}
	// @agentbar-accent is a format, read for the pane this list belongs
	// to, so an accent that differs per session follows the session
	pane := m.ownPane
	if o.Popup {
		pane = o.Here
	}
	if pane != "" {
		if c, err := m.runner.Run("display-message", "-p", "-t", pane, "#{E:@agentbar-accent}"); err == nil {
			setAccent(c)
		}
	}
	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "agentbar:", err)
		os.Exit(1)
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(waitSnapshot(m.snaps), tick())
}

func tick() tea.Cmd {
	return tea.Tick(tickEvery, func(time.Time) tea.Msg { return tickMsg{} })
}

func waitSnapshot(ch <-chan daemon.Snapshot) tea.Cmd {
	return func() tea.Msg {
		s, ok := <-ch
		if !ok {
			return nil
		}
		return snapshotMsg(s)
	}
}

// windowActive reports whether this viewer's window is the one on screen.
// Idle windows skip the spinner tick so they cost nothing.
func (m Model) windowActive() bool {
	if m.ownPane == "" || m.popup {
		return true
	}
	p, ok := m.panes[m.ownPane]
	return !ok || p.WindowActive
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
	// A search that lost the selected row selects its best match.
	if m.popup && m.filter != "" {
		m.selected = rows[0].ID
		return
	}
	// No valid selection: start on the session in this window if there
	// is one, else the first row.
	if !m.home() {
		m.selected = rows[0].ID
	}
}

// oldestHot puts the cursor on the session that has needed you longest.
// Reports whether there is one.
func (m *Model) oldestHot() bool {
	found := false
	var oldest time.Time
	for _, s := range m.ordered() {
		if s.Status != state.StatusNeedsYou {
			continue
		}
		if !found || s.LastActivity.Before(oldest) {
			m.selected, oldest, found = s.ID, s.LastActivity, true
		}
	}
	return found
}

// otherHot reports whether a session besides id needs you.
func (m Model) otherHot(id string) bool {
	for _, s := range m.ordered() {
		if s.ID != id && s.Status == state.StatusNeedsYou {
			return true
		}
	}
	return false
}

// home puts the cursor on the session running in this viewer's window; a
// popup opens on the oldest request instead, when there is one. Reports
// whether it found a place.
func (m *Model) home() bool {
	if m.popup && m.oldestHot() {
		return true
	}
	if m.here == "" {
		return false
	}
	for _, s := range m.ordered() {
		if s.TmuxPaneID == m.here {
			m.selected = s.ID
			return true
		}
	}
	return false
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
		m.now = time.Now()
		if m.windowActive() {
			m.tick++
		}
		return m, tick()
	case snapshotMsg:
		m.sessions, m.panes = msg.Sessions, msg.Panes
		m.err = nil
		if msg.Err != "" {
			m.err = fmt.Errorf("%s", msg.Err)
		}
		m.connected = true
		m.now = time.Now()
		m.here = m.herePane()
		// The first snapshot predates this sidebar's own pane, so "here"
		// is unknown until a later one. Keep homing the cursor until the
		// user moves it.
		if !m.moved {
			m.home()
		}
		m.ensureSelection()
		return m, waitSnapshot(m.snaps)
	case actionMsg:
		if msg.err != nil {
			m.flash(msg.err.Error())
		} else if msg.quit {
			return m, tea.Quit
		} else if msg.text != "" {
			m.flash(msg.text)
		}
		return m, nil
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
	if m.popup {
		return m.pickerKey(msg)
	}
	switch k {
	case "q":
		return m, m.closeAll()
	case "j", "down":
		m.moved = true
		m.move(1)
	case "k", "up":
		m.moved = true
		m.move(-1)
	case "g", "home":
		m.moved = true
		m.move(-1 << 20)
	case "G", "end":
		m.moved = true
		m.move(1 << 20)
	case "tab":
		m.moved = true
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
		return m, func() tea.Msg { return actionMsg{err: daemon.Nudge()} }
	case "?":
		m.helpOpen = !m.helpOpen
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		// the card's number, as shown on its first line: select and jump
		rows := m.ordered()
		if i := int(k[0] - '1'); i < len(rows) {
			m.moved = true
			m.selected = rows[i].ID
			return m, m.jump()
		}
	}
	return m, nil
}

// pickerKey handles a key in the popup. Letters, digits and punctuation
// edit the search; the list moves with the arrows or ctrl+n/p/j/k.
func (m Model) pickerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k := msg.String(); k {
	case "esc":
		// first the search, then the popup
		if m.filter == "" {
			return m, tea.Quit
		}
		m.filter = ""
	case "down", "ctrl+n", "ctrl+j":
		m.moved = true
		m.move(1)
		return m, nil
	case "up", "ctrl+p", "ctrl+k":
		m.moved = true
		m.move(-1)
		return m, nil
	case "tab":
		m.moved = true
		m.nextHot()
		return m, nil
	case "enter":
		return m, m.jump()
	case "ctrl+y":
		cmd := m.accept()
		if cur, ok := m.current(); ok && m.otherHot(cur.ID) {
			m.moved = true
			m.nextHot()
		}
		return m, cmd
	case "ctrl+x":
		if _, ok := m.current(); ok {
			m.confirmKill = true
		}
		return m, nil
	case "ctrl+r":
		return m, func() tea.Msg { return actionMsg{err: daemon.Nudge()} }
	case "backspace":
		if r := []rune(m.filter); len(r) > 0 {
			m.filter = string(r[:len(r)-1])
		}
	case "ctrl+u":
		m.filter = ""
	case "ctrl+w":
		f := strings.TrimRight(m.filter, " ")
		m.filter = f[:strings.LastIndex(f, " ")+1]
	default:
		if msg.Text == "" {
			return m, nil
		}
		m.filter += msg.Text
	}
	// the search changed: the cursor follows the matches, not the homing
	m.moved = true
	m.ensureSelection()
	return m, nil
}

// closeAll removes every sidebar, this one included, and lets the daemon
// idle out.
func (m Model) closeAll() tea.Cmd {
	r := m.runner
	return func() tea.Msg {
		_ = tmuxctl.SetEnabled(r, false)
		_, err := tmuxctl.CloseAll(r)
		return actionMsg{err: err}
	}
}

func (m Model) jump() tea.Cmd {
	cur, ok := m.current()
	if !ok {
		return nil
	}
	if !cur.InTmux {
		return func() tea.Msg { return actionMsg{text: "not in tmux: cannot jump"} }
	}
	r, pane := m.runner, cur.TmuxPaneID
	if m.popup {
		client := m.client
		return func() tea.Msg {
			return actionMsg{err: tmuxctl.JumpClient(r, client, pane), quit: true}
		}
	}
	return func() tea.Msg {
		return actionMsg{err: tmuxctl.Jump(r, pane)}
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
	// a popup that answered the last request has done its job
	done := m.popup && !m.otherHot(cur.ID)
	return func() tea.Msg {
		if err := tmuxctl.SendEnter(r, pane); err != nil {
			return actionMsg{err: err}
		}
		_ = daemon.Nudge()
		return actionMsg{text: "accepted: " + title, quit: done}
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
