package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/ssnxd/agentbar/internal/session"
	"github.com/ssnxd/agentbar/internal/state"
)

// Group is one project heading with its sessions, most urgent first.
type Group struct {
	Name string
	Rows []session.Session
	rank int
}

// Groups buckets sessions by project. Rows sort by urgency then recency;
// groups by their most urgent row then name.
func Groups(ss []session.Session) []Group {
	byName := map[string]*Group{}
	var order []*Group
	for _, s := range ss {
		g := byName[s.Project]
		if g == nil {
			g = &Group{Name: s.Project, rank: 99}
			byName[s.Project] = g
			order = append(order, g)
		}
		g.Rows = append(g.Rows, s)
		if r := rank(s.Status); r < g.rank {
			g.rank = r
		}
	}
	for _, g := range order {
		rows := g.Rows
		sort.SliceStable(rows, func(i, j int) bool {
			ri, rj := rank(rows[i].Status), rank(rows[j].Status)
			if ri != rj {
				return ri < rj
			}
			return rows[i].LastActivity.After(rows[j].LastActivity)
		})
	}
	sort.SliceStable(order, func(i, j int) bool {
		if order[i].rank != order[j].rank {
			return order[i].rank < order[j].rank
		}
		return order[i].Name < order[j].Name
	})
	out := make([]Group, len(order))
	for i, g := range order {
		out[i] = *g
	}
	return out
}

// visible applies the filter text and returns the rows in display order.
func (m Model) visible() []session.Session {
	var out []session.Session
	q := strings.ToLower(strings.TrimSpace(m.filter))
	for _, s := range m.sessions {
		if q == "" || matches(s, q) {
			out = append(out, s)
		}
	}
	return out
}

func matches(s session.Session, q string) bool {
	hay := strings.ToLower(strings.Join([]string{s.Project, s.Title, s.Branch, look(s.Status).label, s.TmuxTarget, s.Name}, " "))
	return strings.Contains(hay, q)
}

// ordered returns the visible rows flattened in the order they are drawn.
func (m Model) ordered() []session.Session {
	var out []session.Session
	for _, g := range Groups(m.visible()) {
		out = append(out, g.Rows...)
	}
	return out
}

func shortModel(model string) string {
	model = strings.TrimPrefix(model, "claude-")
	if i := strings.Index(model, "-"); i > 0 {
		return model[:i]
	}
	return model
}

func age(now, t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= n {
		return s
	}
	return ansi.Truncate(s, n-1, "") + "…"
}

// pad right-fills s with spaces to width w (in cells) using st for the fill
// so a selection band covers the full line.
func pad(s string, w int, st lipgloss.Style) string {
	gap := w - ansi.StringWidth(s)
	if gap <= 0 {
		return s
	}
	return s + st.Render(strings.Repeat(" ", gap))
}

// line is one rendered row of the body with the session it belongs to.
type line struct {
	text string
	id   string
}

func (m Model) header() string {
	n := len(m.visible())
	hot := 0
	for _, s := range m.visible() {
		if s.Status == state.StatusNeedsYou {
			hot++
		}
	}
	left := sTitle.Render(" claude ")
	var right string
	switch {
	case n == 0:
		right = sCount.Render("no sessions ")
	case hot > 0:
		right = sCount.Render(fmt.Sprintf("%d · ", n)) + sHot.Render(fmt.Sprintf("%d needs you", hot)) + " "
	default:
		right = sCount.Render(fmt.Sprintf("%d sessions ", n))
	}
	fill := m.width - ansi.StringWidth(left) - ansi.StringWidth(right) - 1
	if fill < 0 {
		return truncate(left+right, m.width)
	}
	return left + sDim.Render(strings.Repeat("─", fill)) + " " + right
}

// body renders every group and row into lines, unscrolled.
func (m Model) body() []line {
	var out []line
	w := m.width
	narrow := w < 30
	for _, g := range Groups(m.visible()) {
		out = append(out, line{text: ""})
		out = append(out, line{text: " " + sGroup.Render(truncate(g.Name, w-2))})
		for _, s := range g.Rows {
			selected := s.ID == m.selected
			lk := look(s.Status)
			icon := lk.icon
			if s.Status == state.StatusWorking {
				icon = spinnerFrames[m.tick%len(spinnerFrames)]
			}
			// line 1: "  ● needs you  title"
			prefix := sel(sText, selected).Render("  ") + sel(lk.style, selected).Render(icon+" "+fmt.Sprintf("%-9s", lk.label)) + sel(sText, selected).Render("  ")
			title := truncate(s.Title, w-ansi.StringWidth(prefix)-1)
			l1 := prefix + sel(sText, selected).Render(title)
			out = append(out, line{text: pad(l1, w, sel(sText, selected)), id: s.ID})
			if narrow {
				continue
			}
			// line 2: "    work:2.1 · fable · fix/auth      12m"
			var parts []string
			if s.InTmux && s.TmuxTarget != "" {
				parts = append(parts, s.TmuxTarget)
			} else {
				parts = append(parts, "not in tmux")
			}
			if s.Model != "" {
				parts = append(parts, shortModel(s.Model))
			}
			if s.Branch != "" {
				parts = append(parts, s.Branch)
			}
			ag := age(m.now, s.LastActivity)
			left := truncate(strings.Join(parts, " · "), w-4-ansi.StringWidth(ag)-2)
			gap := w - 4 - ansi.StringWidth(left) - ansi.StringWidth(ag) - 1
			if gap < 1 {
				gap = 1
			}
			l2 := sel(sText, selected).Render("    ") + sel(sDim, selected).Render(left) + sel(sText, selected).Render(strings.Repeat(" ", gap)) + sel(sDim, selected).Render(ag)
			out = append(out, line{text: pad(l2, w, sel(sText, selected)), id: s.ID})
			// line 3: detail for rows that need attention
			if (s.Status == state.StatusNeedsYou || s.Status == state.StatusError) && s.Detail != "" {
				l3 := sel(sText, selected).Render("    ") + sel(sSub, selected).Render(truncate(s.Detail, w-5))
				out = append(out, line{text: pad(l3, w, sel(sText, selected)), id: s.ID})
			}
		}
	}
	if len(out) == 0 {
		out = append(out, line{text: ""}, line{text: sDim.Render("  no claude sessions running")})
		if m.filter != "" {
			out[1].text = sDim.Render("  nothing matches " + m.filter)
		}
	}
	return out
}

type hint struct{ key, label string }

func (m Model) hints() []hint {
	cur, ok := m.current()
	hs := []hint{{"enter", "jump"}}
	if ok && cur.Status == state.StatusNeedsYou {
		hs = append(hs, hint{"y", "accept"})
	}
	hs = append(hs, hint{"x", "kill"}, hint{"/", "filter"}, hint{"?", "help"})
	return hs
}

func renderHints(hs []hint, w int) string {
	var b strings.Builder
	used := 0
	for i, h := range hs {
		seg := sKey.Render(h.key) + " " + sKeyLbl.Render(h.label)
		width := ansi.StringWidth(h.key) + 1 + ansi.StringWidth(h.label)
		if i > 0 {
			width += 2
		}
		if used+width > w-1 {
			break
		}
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(seg)
		used += width
	}
	return " " + b.String()
}

var helpRows = []hint{
	{"j / k", "move"},
	{"enter", "jump to pane (sidebar follows)"},
	{"tab", "next needs-you"},
	{"y", "accept permission prompt"},
	{"x", "kill session (asks y/n)"},
	{"/", "filter; esc clears"},
	{"r", "refresh now"},
	{"q", "close sidebar"},
}

func (m Model) footer() string {
	switch {
	case m.filtering:
		return " " + sKey.Render("/") + sText.Render(m.filter) + sDim.Render("▏")
	case m.confirmKill:
		cur, _ := m.current()
		return " " + sErr.Render(truncate("kill "+cur.Title+"? y/n", m.width-2))
	case m.msg != "" && m.now.Before(m.msgUntil):
		return " " + sMsg.Render(truncate(m.msg, m.width-2))
	case m.helpOpen:
		return renderHints([]hint{{"?", "close help"}}, m.width)
	default:
		return renderHints(m.hints(), m.width)
	}
}

func (m Model) helpBlock() []string {
	var out []string
	out = append(out, " "+sGroup.Render("keys"))
	for _, h := range helpRows {
		out = append(out, fmt.Sprintf(" %s %s", sKey.Render(fmt.Sprintf("%-5s", h.key)), sKeyLbl.Render(truncate(h.label, m.width-8))))
	}
	return out
}

// Render draws the whole sidebar at m.width × m.height.
func Render(m Model) string {
	if m.width <= 0 {
		m.width = 42
	}
	if m.height <= 0 {
		m.height = 24
	}
	head := m.header()
	foot := m.footer()
	var extra []string
	if m.helpOpen {
		extra = m.helpBlock()
	}
	if m.err != nil {
		extra = append(extra, " "+sErr.Render(truncate(m.err.Error(), m.width-2)))
	}
	bodyH := m.height - 1 - 1 - len(extra)
	if bodyH < 1 {
		bodyH = 1
	}
	lines := m.body()
	scroll := clampScroll(lines, m.selected, m.scroll, bodyH)
	end := scroll + bodyH
	if end > len(lines) {
		end = len(lines)
	}
	var b strings.Builder
	b.WriteString(head)
	b.WriteString("\n")
	n := 0
	for _, l := range lines[scroll:end] {
		b.WriteString(l.text)
		b.WriteString("\n")
		n++
	}
	for ; n < bodyH; n++ {
		b.WriteString("\n")
	}
	for _, e := range extra {
		b.WriteString(e)
		b.WriteString("\n")
	}
	b.WriteString(foot)
	return b.String()
}

// clampScroll keeps the selected row's lines inside the viewport.
func clampScroll(lines []line, selected string, scroll, bodyH int) int {
	first, last := -1, -1
	for i, l := range lines {
		if l.id == selected {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if scroll > len(lines)-bodyH {
		scroll = len(lines) - bodyH
	}
	if scroll < 0 {
		scroll = 0
	}
	if first < 0 {
		return scroll
	}
	// include the group heading above the first row of a group
	if first > 0 && lines[first-1].id == "" {
		first--
	}
	if last >= scroll+bodyH {
		scroll = last - bodyH + 1
	}
	if first < scroll {
		scroll = first
	}
	return scroll
}
