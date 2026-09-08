package ui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/ssnxd/agentbar/internal/session"
	"github.com/ssnxd/agentbar/internal/state"
)

// Layout: a flat list, one card per session, in tmux order. The card for
// the session running in this sidebar's own window carries a bar in the
// left gutter.
//
//	▌repo                              3h 12m
//	▌Session title, wrapped to at most
//	▌two lines
//	▌branch                         ● needs you
//	 Bash touch /tmp/x                          (needs-you / error only)
//
//	 next card...

const hereMark = "▌"

const maxTitleLines = 2

// Ordered returns the visible sessions in display order: by tmux position
// (session, window, pane) so the list matches your window order and never
// reshuffles when a status changes; sessions outside tmux come last.
func Ordered(ss []session.Session) []session.Session {
	out := append([]session.Session(nil), ss...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.InTmux != b.InTmux {
			return a.InTmux
		}
		if a.TmuxSession != b.TmuxSession {
			return a.TmuxSession < b.TmuxSession
		}
		if a.TmuxTarget != b.TmuxTarget {
			return targetKey(a.TmuxTarget) < targetKey(b.TmuxTarget)
		}
		return a.StartedAt.Before(b.StartedAt)
	})
	return out
}

// targetKey turns "sess:12.3" into a sortable "000012.000003".
func targetKey(t string) string {
	i := strings.LastIndex(t, ":")
	if i < 0 {
		return t
	}
	parts := strings.SplitN(t[i+1:], ".", 2)
	w, _ := strconv.Atoi(parts[0])
	p := 0
	if len(parts) == 2 {
		p, _ = strconv.Atoi(parts[1])
	}
	return fmt.Sprintf("%06d.%06d", w, p)
}

// visible applies the filter text.
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

// ordered returns the visible rows in the order they are drawn.
func (m Model) ordered() []session.Session {
	return Ordered(m.visible())
}

// uptime formats how long a session has been running.
func uptime(now, started time.Time) string {
	if started.IsZero() {
		return ""
	}
	d := now.Sub(started)
	if d < 0 {
		d = 0
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %02dm", hours, mins)
	default:
		return fmt.Sprintf("%dm", mins)
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

// wrapLines word-wraps s to width, at most max lines; the last line is
// truncated with an ellipsis when text remains.
func wrapLines(s string, width, max int) []string {
	if width <= 0 || s == "" {
		return nil
	}
	lines := strings.Split(ansi.Wordwrap(s, width, ""), "\n")
	if len(lines) > max {
		rest := strings.Join(lines[max-1:], " ")
		lines = append(lines[:max-1], truncate(rest, width))
	}
	for i, l := range lines {
		lines[i] = truncate(l, width) // a single word longer than width
	}
	return lines
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

// twoSided renders left and right text on one line of width w after a
// one-cell gutter, with the right part right-aligned and the left part
// truncated to fit.
func twoSided(gutter, left, right string, ls, rs, fill lipgloss.Style, w int) string {
	rw := ansi.StringWidth(right)
	lw := w - 1 - rw - 2 // gutter, gap of two
	if lw < 4 {
		lw = 4
	}
	left = truncate(left, lw)
	gap := w - 1 - ansi.StringWidth(left) - rw
	if gap < 1 {
		gap = 1
	}
	return gutter + ls.Render(left) + fill.Render(strings.Repeat(" ", gap)) + rs.Render(right)
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

// body renders every card into lines, unscrolled.
func (m Model) body() []line {
	var out []line
	w := m.width
	narrow := w < 30
	for _, s := range m.ordered() {
		selected := s.ID == m.selected
		fill := sel(sText, selected)
		lk := look(s.Status)
		icon := lk.icon
		if s.Status == state.StatusWorking {
			icon = spinnerFrames[m.tick%len(spinnerFrames)]
		}
		status := icon + " " + lk.label
		add := func(text string) {
			out = append(out, line{text: pad(text, w, fill), id: s.ID})
		}
		// gutter: a bar on the session that lives in this sidebar's window
		gutter := fill.Render(" ")
		if m.here != "" && s.TmuxPaneID == m.here {
			gutter = sel(sHere, selected).Render(hereMark)
		}

		if narrow {
			add(gutter + sel(lk.style, selected).Render(icon) + fill.Render(" ") + sel(sText, selected).Render(truncate(s.Title, w-3)))
			out = append(out, line{text: ""})
			continue
		}

		// 1: repo (left) · uptime (right)
		add(twoSided(gutter, s.Project, uptime(m.now, s.StartedAt), sel(sGroup, selected), sel(sDim, selected), fill, w))
		// 2..3: title, wrapped
		for _, t := range wrapLines(s.Title, w-2, maxTitleLines) {
			add(gutter + sel(sText, selected).Render(t))
		}
		// branch (left) · status (right)
		branch := s.Branch
		if branch == "" {
			branch = "no branch"
		}
		add(twoSided(gutter, branch, status, sel(sDim, selected), sel(lk.style, selected), fill, w))
		// detail for rows that need attention
		if (s.Status == state.StatusNeedsYou || s.Status == state.StatusError) && s.Detail != "" {
			add(gutter + sel(sSub, selected).Render(truncate(s.Detail, w-2)))
		}
		out = append(out, line{text: ""})
	}
	if len(out) == 0 {
		out = append(out, line{text: ""}, line{text: sDim.Render("  no claude sessions running")})
		switch {
		case m.filter != "":
			out[1].text = sDim.Render("  nothing matches " + m.filter)
		case !m.connected && m.sessions == nil && m.width > 0 && m.snaps != nil:
			out[1].text = sDim.Render("  connecting…")
		}
	} else {
		out = append([]line{{text: ""}}, out...)
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
	{"enter", "jump to pane"},
	{"tab", "next needs-you"},
	{"y", "accept permission prompt"},
	{"x", "kill session (asks y/n)"},
	{"/", "filter; esc clears"},
	{"r", "refresh now"},
	{"q", "close every sidebar"},
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

// clampScroll keeps the selected card's lines inside the viewport.
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
	if last >= scroll+bodyH {
		scroll = last - bodyH + 1
	}
	if first < scroll {
		scroll = first
	}
	return scroll
}
