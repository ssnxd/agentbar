package ui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/ssnxd/agentbar/internal/session"
	"github.com/ssnxd/agentbar/internal/state"
)

// Layout: a flat list, one card per session, in tmux order. The card for
// the session running in this sidebar's own window carries a bar in the
// left gutter.
//
//	▌repo · branch              ● needs you 3m
//	▌Session title, wrapped to at most
//	▌two lines
//	▌Bash touch /tmp/x                          (what it needs you for, or
//	 ↳ Map hook payloads                   Grep   what it is doing right now)
//	                                            (one per running subagent)
//	 next card...
//
// Status leads, top right, with how long the session has sat there. A
// working session shows no age: it is busy now.

const hereMark = "▌"

const maxTitleLines = 2

// Subagent rows: at most maxAgentLines per card, then "+N more".
const (
	agentMark     = "↳ "
	maxAgentLines = 4
)

// agentLabel is what a subagent row says: Claude's task description once
// its meta file exists, else the agent type.
func agentLabel(a session.Agent) string {
	if a.Description != "" {
		return a.Description
	}
	if a.Type != "" {
		return a.Type
	}
	return "agent"
}

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

// matches reports whether every word of q is somewhere in the session.
func matches(s session.Session, q string) bool {
	hay := strings.ToLower(strings.Join([]string{s.Project, s.Title, s.Branch, look(s.Status).label, s.TmuxTarget, s.Name}, " "))
	for _, w := range strings.Fields(q) {
		if !strings.Contains(hay, w) {
			return false
		}
	}
	return true
}

// ordered returns the visible rows in the order they are drawn.
func (m Model) ordered() []session.Session {
	return Ordered(m.visible())
}

func age(now, t time.Time) string { return session.Age(now, t) }

// repoBranch renders "repo · branch" to fit in maxW cells: the branch gives
// way first, then the repo.
func repoBranch(s session.Session, maxW int, selected bool) string {
	st := sGroup
	if quiet(s.Status) {
		st = sQuiet
	}
	repo := truncate(s.Project, maxW)
	rest := maxW - ansi.StringWidth(repo) - 3
	if s.Branch == "" || rest < 4 {
		return sel(st, selected).Render(repo)
	}
	return sel(st, selected).Render(repo) + dim(selected).Render(" · "+truncate(s.Branch, rest))
}

// knownTools are the built-in tool names a detail line can start with.
var knownTools = map[string]bool{
	"Agent": true, "Bash": true, "Edit": true, "Glob": true, "Grep": true,
	"MultiEdit": true, "NotebookEdit": true, "Read": true, "Skill": true,
	"Task": true, "TodoWrite": true, "WebFetch": true, "WebSearch": true,
	"Write": true,
}

// mcpTool splits "mcp__server__tool_name" into "server" and "tool name".
func mcpTool(t string) (server, tool string, ok bool) {
	rest, found := strings.CutPrefix(t, "mcp__")
	if !found {
		return "", "", false
	}
	server, tool, _ = strings.Cut(rest, "__")
	server = strings.TrimPrefix(server, "plugin_")
	return server, strings.ReplaceAll(tool, "_", " "), true
}

// toolName is a tool as shown in a narrow column: MCP tools lose their
// prefix and server.
func toolName(t string) string {
	if _, tool, ok := mcpTool(t); ok && tool != "" {
		return tool
	}
	return t
}

// splitDetail parts a detail line into the tool that leads it and the rest.
// A line that does not start with a tool comes back whole, as rest.
func splitDetail(d string) (head, rest string) {
	first, after, _ := strings.Cut(d, " ")
	if server, tool, ok := mcpTool(first); ok {
		head = server
		if tool != "" {
			head += " · " + tool
		}
		return head, after
	}
	if knownTools[first] {
		return first, after
	}
	return "", d
}

// detailLine renders a detail in w cells, the tool in hs and the rest in rs.
func detailLine(d string, w int, hs, rs lipgloss.Style) string {
	head, rest := splitDetail(d)
	if head == "" {
		return rs.Render(truncate(rest, w))
	}
	head = truncate(head, w)
	out := hs.Render(head)
	if left := w - ansi.StringWidth(head) - 1; rest != "" && left > 1 {
		out += rs.Render(" " + truncate(rest, left))
	}
	return out
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

// twoSided renders left and right text on one line of width w, with the
// right part right-aligned and the left part truncated to fit.
func twoSided(left, right string, ls, rs, fill lipgloss.Style, w int) string {
	rw := ansi.StringWidth(right)
	lw := w - rw - 2 // gap of two
	if lw < 4 {
		lw = 4
	}
	left = truncate(left, lw)
	gap := w - ansi.StringWidth(left) - rw
	if gap < 1 {
		gap = 1
	}
	return ls.Render(left) + fill.Render(strings.Repeat(" ", gap)) + rs.Render(right)
}

// fmtTokens renders a context size compactly: "950", "142k", "1.2M".
func fmtTokens(n int64) string {
	switch {
	case n <= 0:
		return ""
	case n < 1000:
		return fmt.Sprint(n)
	case n < 1_000_000:
		return fmt.Sprintf("%dk", (n+500)/1000)
	}
	return fmt.Sprintf("%.1fM", float64(n)/1e6)
}

// line is one rendered row of the body with the session it belongs to.
type line struct {
	text string
	id   string
}

const (
	promptMark  = "❯ "
	promptCaret = "▏"
	promptHint  = "search sessions"
)

// pickerHead is the top of the popup: the search line with the match
// count on the right, and a rule between it and the list.
func (m Model) pickerHead() []string {
	n, hot := len(m.visible()), 0
	for _, s := range m.visible() {
		if s.Status == state.StatusNeedsYou {
			hot++
		}
	}
	var right string
	switch {
	case m.filter != "":
		right = sCount.Render(fmt.Sprintf("%d/%d", n, len(m.sessions)))
	case hot > 0:
		right = sCount.Render(fmt.Sprintf("%d · ", n)) + sHot.Render(session.NeedYou(hot))
	case n > 0:
		right = sCount.Render(fmt.Sprint(n))
	}
	cw := m.width - 3
	room := cw - ansi.StringWidth(promptMark) - ansi.StringWidth(right) - 2
	left := sTitle.Render(promptMark)
	if m.filter == "" {
		left += sDim.Render(promptCaret + truncate(promptHint, room-1))
	} else {
		q := m.filter
		// a search longer than the line shows its end, where you type
		for ansi.StringWidth(q) > room-1 && q != "" {
			_, size := utf8.DecodeRuneInString(q)
			q = q[size:]
		}
		left += sText.Render(q) + sDim.Render(promptCaret)
	}
	gap := cw - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 1 {
		gap = 1
	}
	return []string{
		"  " + left + strings.Repeat(" ", gap) + right,
		"  " + sRule.Render(strings.Repeat("─", cw)),
	}
}

func (m Model) header() string {
	n := len(m.visible())
	hot := 0
	for _, s := range m.visible() {
		if s.Status == state.StatusNeedsYou {
			hot++
		}
	}
	left := sTitle.Render("  claude ")
	var right string
	switch {
	case n == 0:
		right = sCount.Render("no sessions ")
	case hot > 0:
		right = sCount.Render(fmt.Sprintf("%d · ", n)) + sHot.Render(session.NeedYou(hot)) + " "
	case n == 1:
		right = sCount.Render("1 session ")
	default:
		right = sCount.Render(fmt.Sprintf("%d sessions ", n))
	}
	fill := m.width - ansi.StringWidth(left) - ansi.StringWidth(right) - 1
	if fill < 0 {
		return truncate(left+right, m.width-1)
	}
	return left + sRule.Render(strings.Repeat("─", fill)) + " " + right
}

// body renders every card into lines, unscrolled.
func (m Model) body() []line {
	var out []line
	w := m.width
	// column 1 is the here-marker gutter, column 2 a space, and the last
	// column a margin; text lives in the cw cells between.
	cw := w - 3
	if cw < 8 {
		cw = 8
	}
	narrow := w < 30
	for i, s := range m.ordered() {
		selected := s.ID == m.selected
		fill := sel(sText, selected)
		lk := look(s.Status)
		icon := lk.icon
		if s.Status == state.StatusWorking {
			icon = spinnerFrames[m.tick%len(spinnerFrames)]
		}
		status := icon + " " + lk.label
		switch {
		case s.Status == state.StatusWorking:
			if t := fmtTokens(s.ContextTokens); t != "" {
				status += " · " + t
			}
		default:
			if a := age(m.now, s.LastActivity); a != "" {
				status += " " + a
			}
		}
		// gutter: a bar on the session that lives in this sidebar's window
		gutter := fill.Render(" ")
		if m.here != "" && s.TmuxPaneID == m.here {
			gutter = sel(sHere, selected).Render(hereMark)
		}
		add := func(text string) {
			out = append(out, line{text: pad(gutter+fill.Render(" ")+text, w, fill), id: s.ID})
		}

		// a quiet card sits back: its title drops to the secondary tone
		title := sel(sText, selected)
		if quiet(s.Status) {
			title = sel(sSub, selected)
		}

		if narrow {
			name := s.Title
			if name == "" {
				name = s.Project
			}
			add(sel(lk.style, selected).Render(icon) + fill.Render(" ") + title.Render(truncate(name, cw-2)))
			out = append(out, line{text: ""})
			continue
		}

		// 1: number, repo · branch (left), status with idle age or context (right)
		num := fmt.Sprintf("%d ", i+1)
		if m.popup {
			// digits type into the search there: no number to press
			num = ""
		}
		sw := ansi.StringWidth(status)
		left := dim(selected).Render(num) + repoBranch(s, cw-len(num)-sw-2, selected)
		gap := cw - ansi.StringWidth(left) - sw
		if gap < 1 {
			gap = 1
		}
		add(left + fill.Render(strings.Repeat(" ", gap)) + sel(lk.style, selected).Render(status))
		// 2..3: title, wrapped
		for _, t := range wrapLines(s.Title, cw, maxTitleLines) {
			add(title.Render(t))
		}
		// live line: what it needs you for, or what it is doing right now
		switch s.Status {
		case state.StatusNeedsYou, state.StatusError:
			if s.Detail != "" {
				add(detailLine(s.Detail, cw, sel(sText, selected), sel(sSub, selected)))
			}
		case state.StatusWorking:
			if s.Detail != "" {
				add(detailLine(s.Detail, cw, sel(sSub, selected), dim(selected)))
			}
		}
		// running subagents: "↳ what it is doing" (left) · last tool (right)
		for i, a := range s.Agents {
			if i == maxAgentLines && len(s.Agents) > maxAgentLines {
				add(dim(selected).Render(fmt.Sprintf("%s+%d more", agentMark, len(s.Agents)-maxAgentLines)))
				break
			}
			add(twoSided(agentMark+agentLabel(a), truncate(toolName(a.Tool), cw/3), sel(sSub, selected), dim(selected), fill, cw))
		}
		out = append(out, line{text: ""})
	}
	if len(out) == 0 {
		out = append(out, line{text: ""}, line{text: sSub.Render("  No sessions yet.")}, line{text: sDim.Render("  Start claude in any pane;")}, line{text: sDim.Render("  it shows up here.")})
		switch {
		case m.filter != "":
			out = out[:2]
			out[1].text = sDim.Render("  Nothing matches " + m.filter)
		case !m.connected && m.sessions == nil && m.width > 0 && m.snaps != nil:
			out = out[:2]
			out[1].text = sDim.Render("  Connecting…")
		}
	} else {
		out = append([]line{{text: ""}}, out...)
	}
	return out
}

type hint struct{ key, label string }

func (m Model) hints() []hint {
	cur, ok := m.current()
	if m.popup {
		hs := []hint{{"enter", "jump"}}
		if ok && cur.Status == state.StatusNeedsYou {
			hs = append(hs, hint{"^y", "accept"})
		}
		return append(hs, hint{"^x", "kill"}, hint{"tab", "next request"}, hint{"esc", "close"})
	}
	hs := []hint{{"enter", "jump"}}
	if ok && cur.Status == state.StatusNeedsYou {
		hs = append(hs, hint{"y", "accept"})
	}
	// help before filter: hints that do not fit drop from the end, and
	// help is the one that leads to all the others.
	hs = append(hs, hint{"x", "kill"}, hint{"?", "help"}, hint{"/", "filter"})
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
		if used+width > w-3 {
			break
		}
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(seg)
		used += width
	}
	return "  " + b.String()
}

// PopupHeight is how many rows the list needs at width w, so a popup can
// be sized to its content.
func PopupHeight(ss []session.Session, w int) int {
	m := Model{width: w, sessions: ss, popup: true}
	return len(m.pickerHead()) + len(m.body()) + 1
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
	{"1-9", "jump to card"},
}

func (m Model) footer() string {
	switch {
	case m.filtering:
		return "  " + sKey.Render("/") + sText.Render(m.filter) + sDim.Render("▏")
	case m.confirmKill:
		cur, _ := m.current()
		return "  " + sErr.Render(truncate("kill "+cur.Title+"? y/n", m.width-3))
	case m.msg != "" && m.now.Before(m.msgUntil):
		return "  " + sMsg.Render(truncate(m.msg, m.width-3))
	case m.helpOpen:
		return renderHints([]hint{{"?", "close help"}}, m.width)
	default:
		return renderHints(m.hints(), m.width)
	}
}

func (m Model) helpBlock() []string {
	// a blank row parts the keys from the list above them
	out := []string{""}
	out = append(out, "  "+sGroup.Render("keys"))
	for _, h := range helpRows {
		out = append(out, fmt.Sprintf("  %s %s", sKey.Render(fmt.Sprintf("%-5s", h.key)), sKeyLbl.Render(truncate(h.label, m.width-9))))
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
	head := []string{m.header()}
	if m.popup {
		head = m.pickerHead()
	}
	foot := m.footer()
	var extra []string
	if m.helpOpen {
		extra = m.helpBlock()
	}
	if m.err != nil {
		extra = append(extra, "  "+sErr.Render(truncate(m.err.Error(), m.width-3)))
	}
	bodyH := m.height - len(head) - 1 - len(extra)
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
	for _, h := range head {
		b.WriteString(h)
		b.WriteString("\n")
	}
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
