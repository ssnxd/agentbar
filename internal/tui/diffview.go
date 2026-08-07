package tui

import (
	"fmt"
	"os/exec"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// diffModel is the review screen: the task branch's full colored diff
// against the user's current branch, scrollable, with per-file jumps and
// review comments that flow to the orchestrator's inbox.
type diffModel struct {
	taskID    int64
	branch    string
	lines     []string
	fileAt    []int    // indices into lines where a new file's diff starts
	fileNames []string // parallel to fileAt
	offset    int

	commenting bool
	comment    textinput.Model
}

type diffLoadedMsg struct {
	lines []string
	err   error
}

// openDiff loads `git diff HEAD...branch` (what the task branch adds over
// the common ancestor with the user's current position) off the UI thread.
func (a App) openDiff() (tea.Model, tea.Cmd) {
	task, ok := a.currentTask()
	if !ok {
		return a, nil
	}
	in := textinput.New()
	in.Placeholder = "review comment for the orchestrator — enter sends, esc cancels"
	a.diff = diffModel{taskID: task.ID, branch: task.Branch, comment: in}
	a.scr = screenDiff
	repo, branch := task.ProjectPath, task.Branch
	return a, func() tea.Msg {
		cmd := exec.Command("git", "diff", "--color=always", "HEAD..."+branch)
		cmd.Dir = repo
		out, err := cmd.Output()
		if err != nil {
			return diffLoadedMsg{err: fmt.Errorf("git diff failed for %s", branch)}
		}
		text := strings.TrimRight(string(out), "\n")
		if text == "" {
			return diffLoadedMsg{lines: []string{sDim.Render("no differences against your current branch — already landed?")}}
		}
		return diffLoadedMsg{lines: strings.Split(text, "\n")}
	}
}

func (a App) updateDiffLoaded(msg diffLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		a.status = msg.err.Error()
		a.scr = screenTask
		return a, nil
	}
	a.diff.lines = msg.lines
	a.diff.fileAt = a.diff.fileAt[:0]
	a.diff.fileNames = a.diff.fileNames[:0]
	for i, l := range msg.lines {
		plain := stripANSI(l)
		if strings.HasPrefix(plain, "diff --git ") {
			a.diff.fileAt = append(a.diff.fileAt, i)
			name := plain[len("diff --git "):]
			if j := strings.Index(name, " b/"); j > 0 {
				name = name[j+3:]
			}
			a.diff.fileNames = append(a.diff.fileNames, name)
		}
	}
	return a, nil
}

// currentFile returns the file whose section contains the top of the view.
func (d diffModel) currentFile() string {
	name := ""
	for i, at := range d.fileAt {
		if at > d.offset {
			break
		}
		name = d.fileNames[i]
	}
	return name
}

func (a App) updateDiff(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	d := &a.diff
	page := max(4, a.height-8)

	if d.commenting {
		switch msg.String() {
		case "esc":
			d.commenting = false
			return a, nil
		case "enter":
			text := strings.TrimSpace(d.comment.Value())
			d.commenting = false
			d.comment.SetValue("")
			if text == "" {
				return a, nil
			}
			mgr, id := a.mgr, d.taskID
			body := fmt.Sprintf("Human review comment on `%s` (branch %s):\n%s", d.currentFile(), d.branch, text)
			return a, func() tea.Msg {
				err := mgr.SendToAgent(id, "orchestrator", "human", body)
				if err == nil {
					return landedMsg{what: "comment sent to orchestrator"}
				}
				return actionErrMsg{err}
			}
		}
		var cmd tea.Cmd
		d.comment, cmd = d.comment.Update(msg)
		return a, cmd
	}

	maxOffset := max(0, len(d.lines)-page)
	switch msg.String() {
	case "esc", "q":
		a.scr = screenTask
		return a, a.refreshCmd()
	case "j", "down":
		d.offset = min(maxOffset, d.offset+1)
	case "k", "up":
		d.offset = max(0, d.offset-1)
	case "d", "ctrl+d", "pgdown", "space":
		d.offset = min(maxOffset, d.offset+page/2)
	case "u", "ctrl+u", "pgup":
		d.offset = max(0, d.offset-page/2)
	case "g":
		d.offset = 0
	case "G":
		d.offset = maxOffset
	case "]":
		for _, at := range d.fileAt {
			if at > d.offset {
				d.offset = min(maxOffset, at)
				break
			}
		}
	case "[":
		for i := len(d.fileAt) - 1; i >= 0; i-- {
			if d.fileAt[i] < d.offset {
				d.offset = d.fileAt[i]
				break
			}
		}
	case "c":
		d.commenting = true
		d.comment.SetWidth(max(20, a.width-6))
		return a, d.comment.Focus()
	}
	return a, nil
}

func (a App) viewDiff(width, height int) string {
	d := a.diff
	var b strings.Builder
	page := max(4, height-8)

	file := d.currentFile()
	pos := fmt.Sprintf("%d/%d", min(d.offset+page, len(d.lines)), len(d.lines))
	b.WriteString("\n " + sKeyGroup.Render("reviewing "+d.branch) + "  " +
		sCyanChip(file) + "  " + sDim.Render(pos+" · "+fmt.Sprintf("%d files", len(d.fileAt))) + "\n")

	if len(d.lines) == 0 {
		b.WriteString(sDim.Render("  loading diff…") + "\n")
		return b.String()
	}
	end := min(len(d.lines), d.offset+page)
	for _, l := range d.lines[d.offset:end] {
		b.WriteString(" " + lipgloss.NewStyle().MaxWidth(width-2).Render(l) + "\x1b[0m\n")
	}

	if d.commenting {
		b.WriteString("\n " + sPanelHot.Width(max(20, width-4)).Render(d.comment.View()) + "\n")
	}
	return b.String()
}

// stripANSI removes SGR sequences for prefix matching on colored diff lines.
func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case inEsc:
			if (r >= '@' && r <= '~') && r != '[' {
				inEsc = false
			}
		case r == '\x1b':
			inEsc = true
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
