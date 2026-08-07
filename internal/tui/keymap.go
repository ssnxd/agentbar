package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/ssnxd/workflow/internal/db"
)

// binding is one discoverable key action, which-key style: the footer and
// the expanded help both render from these tables, so what is shown is
// exactly what works on the current screen in the current state.
type binding struct {
	keys  string
	label string
	group string
	on    bool // contextually available right now
}

const (
	gNav    = "navigate"
	gAction = "actions"
	gApp    = "general"
)

func (a App) bindings() []binding {
	switch a.scr {
	case screenTask:
		return a.taskBindings()
	case screenNewTask:
		return a.formBindings()
	default:
		return a.dashBindings()
	}
}

func (a App) dashBindings() []binding {
	nTasks := len(a.snap.tasks)
	onTask := a.dash.cursor < nTasks
	if a.dash.confirming {
		return []binding{
			{"y", "confirm quit task", gAction, true},
			{"any", "cancel", gAction, true},
		}
	}
	var ext *bool
	if !onTask && a.dash.cursor < nTasks+len(a.snap.external) {
		v := a.snap.external[a.dash.cursor-nTasks].InTmux
		ext = &v
	}
	return []binding{
		{"x", "quit task", gAction, onTask},
		{"j/k", "move", gNav, nTasks+len(a.snap.external) > 1},
		{"enter", "open task", gNav, onTask},
		{"enter", "jump to session", gNav, ext != nil && *ext},
		{"n", "new task", gAction, true},
		{"?", "keys", gApp, true},
		{"q", "quit", gApp, true},
	}
}

func (a App) taskBindings() []binding {
	agents := a.snap.agents[a.taskV.taskID]
	var sel *db.Agent
	if a.taskV.cursor < len(agents) {
		sel = &agents[a.taskV.cursor]
	}
	if a.taskV.confirming {
		return []binding{
			{"y", "confirm archive", gAction, true},
			{"any", "cancel", gAction, true},
		}
	}
	recoverable := sel != nil && (sel.Status == db.StatusDead ||
		sel.Status == db.StatusError || sel.Status == db.StatusEnded)
	return []binding{
		{"j/k", "select agent", gNav, len(agents) > 1},
		{"enter", "attach (ctrl-q detaches)", gAction, sel != nil && sel.TmuxWindowID != ""},
		{"r", "recover agent", gAction, recoverable},
		{"x", "archive task", gAction, true},
		{"esc", "back", gNav, true},
		{"?", "keys", gApp, true},
		{"q", "quit", gApp, true},
	}
}

func (a App) formBindings() []binding {
	if a.form.busy {
		return []binding{{"…", "creating task", gAction, true}}
	}
	if a.form.step == 0 {
		return []binding{
			{"type", "fuzzy filter", gNav, true},
			{"↑/↓ ctrl+j/k", "select repo", gNav, len(a.form.matches) > 1},
			{"enter", "choose repo", gAction, len(a.form.matches) > 0},
			{"esc", "cancel", gNav, true},
		}
	}
	return []binding{
		{"tab", "switch field", gNav, true},
		{"ctrl+s", "create task", gAction, true},
		{"esc", "back to repo pick", gNav, true},
	}
}

// footer renders the compact one-line hint bar from the active bindings.
func (a App) footer(width int) string {
	var parts []string
	for _, b := range a.bindings() {
		if !b.on {
			continue
		}
		parts = append(parts, sKeyChip.Render(b.keys)+" "+sKeyLabel.Render(b.label))
	}
	line := " " + strings.Join(parts, sDim.Render("  ·  "))
	return lipgloss.NewStyle().Width(width).Background(cBgAlt).Render(truncate(line, width))
}

// helpOverlay renders the which-key expanded panel: all bindings for the
// current screen, grouped, with unavailable ones dimmed out.
func (a App) helpOverlay(width int) string {
	groups := []string{gNav, gAction, gApp}
	var cols []string
	for _, g := range groups {
		var rows []string
		for _, b := range a.bindings() {
			if b.group != g {
				continue
			}
			key := sKeyChip.Render(padRight(b.keys, 10))
			label := sNormal.Render(b.label)
			if !b.on {
				key = sDim.Render(padRight(b.keys, 10))
				label = sDim.Render(b.label + " (not now)")
			}
			rows = append(rows, "  "+key+" "+label)
		}
		if len(rows) == 0 {
			continue
		}
		cols = append(cols, sKeyGroup.Render(" "+g)+"\n"+strings.Join(rows, "\n"))
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		spaceOut(cols, max(24, (width-8)/max(1, len(cols))))...)
	return panel("keys", body, width-2, true)
}

func spaceOut(cols []string, w int) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = lipgloss.NewStyle().Width(w).Render(c)
	}
	return out
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}
