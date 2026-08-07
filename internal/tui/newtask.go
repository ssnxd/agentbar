package tui

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/ssnxd/workflow/internal/manager"
	"github.com/ssnxd/workflow/internal/repos"
)

// The new-task flow is a two-step wizard:
//
//	step 0  pick a repository — telescope-style fuzzy finder over known
//	        projects (recent first) + repos discovered under repo_roots
//	step 1  describe the task — title + prompt, with a summary card of
//	        exactly what will be created
type formModel struct {
	mgr  *manager.Manager
	step int

	// step 0: repo picker
	query   textinput.Model
	all     []repos.Repo
	matches []repos.Match
	cursor  int
	scanned bool

	// step 1: details
	repo   repos.Repo
	title  textinput.Model
	prompt textarea.Model
	focus  int // 0 title, 1 prompt

	busy   bool
	width  int
	height int
}

type (
	reposScannedMsg []repos.Repo
	fzfDoneMsg      struct {
		out string
		err error
	}
)

func fzfAvailable() bool {
	_, err := exec.LookPath("fzf")
	return err == nil
}

// launchFzf hands the terminal to the user's own fzf for the repo pick —
// their muscle memory, their config. Lines are TSV: display fields shown,
// the hidden path field feeds the git preview and the selection.
func (a App) launchFzf() tea.Cmd {
	var in strings.Builder
	for _, r := range a.form.all {
		star := " "
		if r.Known {
			star = "★"
		}
		branch := r.Branch
		if branch == "" {
			branch = "?"
		}
		fmt.Fprintf(&in, "%s %-28s\t%s\t\x1b[36m%-24s\x1b[0m\t\x1b[2m%s\x1b[0m\n",
			star, truncate(r.Name, 28), r.Path, truncate(branch, 24), humanSince(r.Activity))
	}
	buf := &bytes.Buffer{}
	c := exec.Command("fzf",
		"--ansi", "--delimiter=\t", "--with-nth=1,3,4", "--nth=1",
		"--layout=reverse", "--border=rounded", "--info=inline-right",
		"--prompt=repo ▸ ", "--header=pick a repository for the new task (esc cancels)",
		"--preview", "git -C {2} log --oneline --color=always -10; echo; git -C {2} status -sb",
		"--preview-window=right,45%,border-left",
		"--color=prompt:#7aa2f7,pointer:#bb9af7,hl:#7dcfff,hl+:#7dcfff,header:#565f89,border:#565f89",
	)
	c.Stdin = strings.NewReader(in.String())
	c.Stdout = buf
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return fzfDoneMsg{out: buf.String(), err: err}
	})
}

func newForm(mgr *manager.Manager) formModel {
	query := textinput.New()
	query.Placeholder = "type to filter repos…"
	query.Prompt = sKeyChip.Render("   ")
	title := textinput.New()
	title.Placeholder = "short task title"
	prompt := textarea.New()
	prompt.Placeholder = "What should be done? The orchestrator explores the repo, spawns workers, merges their branches."
	prompt.ShowLineNumbers = false
	return formModel{mgr: mgr, query: query, title: title, prompt: prompt}
}

func (f *formModel) reset() {
	f.step = 0
	f.busy = false
	f.scanned = false
	f.cursor = 0
	f.focus = 0
	f.query.SetValue("")
	f.title.SetValue("")
	f.prompt.SetValue("")
	f.all = nil
	f.matches = nil
}

// init focuses the query and kicks off repo discovery in the background.
func (f *formModel) init() tea.Cmd {
	mgr := f.mgr
	scan := func() tea.Msg {
		var known []string
		if tasks, err := mgr.Store.ListTasks(true); err == nil {
			seen := map[string]bool{}
			for _, t := range tasks { // newest task first == most recently used
				if !seen[t.ProjectPath] {
					known = append(known, t.ProjectPath)
					seen[t.ProjectPath] = true
				}
			}
		}
		return reposScannedMsg(repos.Discover(mgr.Cfg.RepoRoots, known))
	}
	return tea.Batch(f.query.Focus(), scan)
}

func (f *formModel) setSize(w, h int) {
	f.width, f.height = w, h
	inner := max(20, w-8)
	f.query.SetWidth(inner)
	f.title.SetWidth(inner)
	f.prompt.SetWidth(inner)
	f.prompt.SetHeight(max(3, h-20))
}

func (f *formModel) refilter() {
	f.matches = repos.Filter(f.all, f.query.Value())
	if f.cursor >= len(f.matches) {
		f.cursor = max(0, len(f.matches)-1)
	}
}

func (a App) updateForm(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if a.form.busy {
		return a, nil
	}
	if a.form.step == 0 {
		return a.updatePicker(msg)
	}
	return a.updateDetails(msg)
}

func (a App) updatePicker(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		a.scr = screenDashboard
		return a, nil
	case "up", "ctrl+k":
		if a.form.cursor > 0 {
			a.form.cursor--
		}
		return a, nil
	case "down", "ctrl+j":
		if a.form.cursor < len(a.form.matches)-1 {
			a.form.cursor++
		}
		return a, nil
	case "enter":
		if a.form.cursor < len(a.form.matches) {
			a.form.repo = a.form.matches[a.form.cursor].Repo
			a.form.step = 1
			a.form.focus = 0
			a.form.title.Blur()
			return a, tea.Batch(a.form.title.Focus(), textinput.Blink)
		}
		return a, nil
	}
	var cmd tea.Cmd
	a.form.query, cmd = a.form.query.Update(msg)
	a.form.refilter()
	return a, cmd
}

func (a App) updateDetails(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		a.form.step = 0
		if fzfAvailable() {
			return a, a.launchFzf()
		}
		return a, a.form.query.Focus()
	case "tab", "shift+tab":
		a.form.focus = 1 - a.form.focus
		return a, a.form.applyFocus()
	case "ctrl+s":
		return a.submitForm()
	case "enter":
		if a.form.focus == 0 { // enter on title advances to the prompt
			a.form.focus = 1
			return a, a.form.applyFocus()
		}
	}
	var cmd tea.Cmd
	if a.form.focus == 0 {
		a.form.title, cmd = a.form.title.Update(msg)
	} else {
		a.form.prompt, cmd = a.form.prompt.Update(msg)
	}
	return a, cmd
}

func (f *formModel) applyFocus() tea.Cmd {
	f.title.Blur()
	f.prompt.Blur()
	if f.focus == 0 {
		return f.title.Focus()
	}
	return f.prompt.Focus()
}

func (a App) submitForm() (tea.Model, tea.Cmd) {
	title := strings.TrimSpace(a.form.title.Value())
	prompt := strings.TrimSpace(a.form.prompt.Value())
	if title == "" || prompt == "" {
		a.status = "title and prompt are both required"
		return a, nil
	}
	a.form.busy = true
	mgr, path := a.mgr, a.form.repo.Path
	return a, func() tea.Msg {
		id, err := mgr.NewTask(path, title, prompt)
		return taskMadeMsg{taskID: id, err: err}
	}
}

// --- views ---

func (f formModel) view() string {
	if f.step == 0 {
		return f.viewPicker()
	}
	return f.viewDetails()
}

func (f formModel) viewPicker() string {
	var b strings.Builder
	b.WriteString("\n " + sKeyGroup.Render("pick a repository") + "\n")
	b.WriteString(sPanelHot.Width(max(30, f.width-4)).Render(f.query.View()) + "\n")

	count := fmt.Sprintf("%d/%d", len(f.matches), len(f.all))
	if !f.scanned {
		count = "scanning…"
	}
	b.WriteString(" " + sDim.Render(count) + "\n")

	visible := max(5, f.height-12)
	start := 0
	if f.cursor >= visible {
		start = f.cursor - visible + 1
	}
	for i := start; i < len(f.matches) && i < start+visible; i++ {
		m := f.matches[i]
		marker, name := "  ", m.Name
		if m.Known {
			marker = sPurple.Render(" ★")
		}
		branch := m.Branch
		if branch == "" {
			branch = "?"
		}
		pathCol := highlightMatch(m.Display, m.Highlights, i == f.cursor)
		left := fmt.Sprintf("%s %-24s ", marker, truncate(name, 24))
		right := fmt.Sprintf("  %s %s", sCyanChip(branch), sDim.Render(humanSince(m.Activity)))
		line := left + pathCol
		st := lipgloss.NewStyle()
		if i == f.cursor {
			st = sSelected
		}
		b.WriteString(st.Width(f.width-2).Render(truncate(line, max(20, f.width-24))+right) + "\n")
	}
	if f.scanned && len(f.matches) == 0 {
		b.WriteString(sDim.Render("   nothing matches — edit repo_roots in config.json if your repos live elsewhere") + "\n")
	}
	return b.String()
}

// highlightMatch renders the matched path with fuzzy-hit characters lit up.
func highlightMatch(s string, hits map[int]bool, selected bool) string {
	if len(hits) == 0 {
		return sDim.Render(s)
	}
	var b strings.Builder
	base := sDim
	if selected {
		base = sNormal
	}
	run := func(txt string, hit bool) {
		if txt == "" {
			return
		}
		if hit {
			b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(cCyan).Render(txt))
		} else {
			b.WriteString(base.Render(txt))
		}
	}
	cur, curHit := strings.Builder{}, false
	for i, r := range []rune(s) {
		h := hits[i]
		if h != curHit {
			run(cur.String(), curHit)
			cur.Reset()
			curHit = h
		}
		cur.WriteRune(r)
	}
	run(cur.String(), curHit)
	return b.String()
}

func sCyanChip(s string) string {
	return lipgloss.NewStyle().Foreground(cCyan).Render(" " + s)
}

func (f formModel) viewDetails() string {
	var b strings.Builder
	b.WriteString("\n " + sKeyGroup.Render("describe the task") + "\n")

	// Summary card: exactly what will happen on create.
	slugPreview := "…"
	if t := strings.TrimSpace(f.title.Value()); t != "" {
		slugPreview = t
	}
	card := fmt.Sprintf("%s %s   %s %s\n%s task branch wf/<id>-%s · orchestrator %s · workers get worktrees + branches",
		sPanelName.Render("repo"), sNormal.Render(f.repo.Path),
		sPanelName.Render("on"), sCyanChip(f.repo.Branch),
		sDim.Render("→"), sDim.Render(slugify(slugPreview)), sPurple.Render(f.mgr.Cfg.OrchModel))
	b.WriteString(sPanel.Width(max(30, f.width-4)).Render(card) + "\n\n")

	field := func(i int, name, hint string, inner string) {
		label := " " + sPanelName.Render(name)
		if f.focus == i {
			label = " " + sKeyChip.Render("▸ ") + sPanelName.Render(name)
		}
		if hint != "" {
			label += sDim.Render("  " + hint)
		}
		b.WriteString(label + "\n")
		st := sPanel
		if f.focus == i {
			st = sPanelHot
		}
		b.WriteString(st.Width(max(30, f.width-4)).Render(inner) + "\n")
	}
	field(0, "title", "short — names the branch", f.title.View())
	field(1, "prompt", "the orchestrator's marching orders", f.prompt.View())

	if f.busy {
		b.WriteString("\n " + sBadgeHot.Render("creating task") +
			sDim.Render(" worktree + tmux session + orchestrator…") + "\n")
	}
	return b.String()
}

// update routes non-key messages (cursor blink, etc.) to the focused widget.
func (f formModel) update(msg tea.Msg) (formModel, tea.Cmd) {
	var cmd tea.Cmd
	switch {
	case f.step == 0:
		f.query, cmd = f.query.Update(msg)
	case f.focus == 0:
		f.title, cmd = f.title.Update(msg)
	default:
		f.prompt, cmd = f.prompt.Update(msg)
	}
	return f, cmd
}

// slugify mirrors manager.slugify for the branch preview only.
func slugify(s string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteRune('-')
				lastDash = true
			}
		}
	}
	out := strings.TrimSuffix(b.String(), "-")
	if len(out) > 24 {
		out = out[:24]
	}
	if out == "" {
		out = "task"
	}
	return out
}
