package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/ssnxd/agentbar/internal/daemon"
	"github.com/ssnxd/agentbar/internal/session"
	"github.com/ssnxd/agentbar/internal/state"
	"github.com/ssnxd/agentbar/internal/tmuxctl"
)

func sample(now time.Time) []session.Session {
	return []session.Session{
		{ID: "1", Project: "miivo-api", Title: "Fix auth middleware token refresh for the mobile client", Status: state.StatusNeedsYou, Detail: "Bash touch /tmp/agentbar-e2e", TmuxSession: "work", TmuxTarget: "work:2.2", TmuxPaneID: "%1", InTmux: true, Model: "claude-fable-5-1", Branch: "fix/auth", StartedAt: now.Add(-3*time.Hour - 12*time.Minute), LastActivity: now.Add(-12 * time.Minute)},
		{ID: "2", Project: "miivo-api", Title: "add rate limiter", Status: state.StatusWorking, Detail: "Bash go test ./...", TmuxSession: "work", TmuxTarget: "work:1.2", TmuxPaneID: "%2", InTmux: true, Model: "claude-opus-5", Branch: "main", StartedAt: now.Add(-45 * time.Minute), LastActivity: now.Add(-3 * time.Minute)},
		{ID: "3", Project: "dotfiles", Title: "ghostty theme", Status: state.StatusWaiting, Model: "claude-sonnet-5", Branch: "main", StartedAt: now.Add(-26 * time.Hour), LastActivity: now.Add(-40 * time.Minute)},
	}
}

func TestRenderCards(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	m := Model{width: 42, height: 24, now: now, sessions: sample(now), selected: "1"}
	out := ansi.Strip(Render(m))
	for _, want := range []string{
		"3 · 1 needs you",
		"miivo-api · fix/auth", "● needs you 12m", // card 1 line 1: repo · branch, status with idle age
		"Fix auth middleware token refresh for", // title wrapped, first line
		"the mobile client",                     // second line
		"Bash touch /tmp/agentbar-e2e",          // the prompt it needs you for
		"miivo-api · main", "add rate limiter", "◐ working",
		"Bash go test ./...", // what the working session is doing right now
		"dotfiles · main", "ghostty theme", "○ waiting 40m",
		"enter jump", "y accept", "x kill",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	for _, gone := range []string{"3h 12m", "1d 2h", "◐ working 3m"} {
		if strings.Contains(out, gone) {
			t.Errorf("unexpected %q in:\n%s", gone, out)
		}
	}
	lines := strings.Split(out, "\n")
	if len(lines) != 24 {
		t.Errorf("want exactly 24 lines for height 24, got %d:\n%s", len(lines), out)
	}
	for _, l := range lines {
		if ansi.StringWidth(l) > 42 {
			t.Errorf("line too wide (%d): %q", ansi.StringWidth(l), l)
		}
	}
	// tmux order: window 1 before window 2; non-tmux last
	if !(strings.Index(out, "add rate limiter") < strings.Index(out, "Fix auth") && strings.Index(out, "Fix auth") < strings.Index(out, "ghostty")) {
		t.Errorf("order wrong:\n%s", out)
	}
	// right alignment: status ends at the right margin, on the repo line
	for _, l := range lines {
		l = strings.TrimRight(l, " ")
		if strings.Contains(l, "● needs you") && !(strings.HasSuffix(l, "● needs you 12m") && strings.Contains(l, "miivo-api")) {
			t.Errorf("status not right-aligned on the repo line: %q", l)
		}
	}
}

func TestRenderAgents(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	ss := sample(now)
	ss[1].Agents = []session.Agent{
		{ID: "a1", Type: "Explore", Description: "Map hook payloads", Tool: "Grep", Detail: "Grep hooks", StartedAt: now.Add(-2 * time.Minute)},
		{ID: "a2", Type: "general-purpose", Tool: "Bash", Detail: "Bash go test ./...", StartedAt: now.Add(-time.Minute)},
	}
	m := Model{width: 42, height: 30, now: now, sessions: ss, selected: "2"}
	out := ansi.Strip(Render(m))
	lines := strings.Split(out, "\n")
	find := func(sub string) string {
		for _, l := range lines {
			if strings.Contains(l, sub) {
				return strings.TrimRight(l, " ")
			}
		}
		t.Fatalf("missing %q in:\n%s", sub, out)
		return ""
	}
	// description when known, else the agent type; last tool on the right
	l1 := find("Map hook payloads")
	if !strings.Contains(l1, "↳") || !strings.HasSuffix(l1, "Grep") {
		t.Errorf("agent line 1: %q", l1)
	}
	l2 := find("general-purpose")
	if !strings.HasSuffix(l2, "Bash") {
		t.Errorf("agent line 2: %q", l2)
	}
	// agents sit under their own card: after "add rate limiter", before "Fix auth"
	if !(strings.Index(out, "add rate limiter") < strings.Index(out, "Map hook payloads") && strings.Index(out, "Map hook payloads") < strings.Index(out, "Fix auth")) {
		t.Errorf("agent lines misplaced:\n%s", out)
	}
	for _, l := range lines {
		if ansi.StringWidth(l) > 42 {
			t.Errorf("line too wide (%d): %q", ansi.StringWidth(l), l)
		}
	}
	// selection covers the agent lines too
	body := m.body()
	for _, b := range body {
		if strings.Contains(ansi.Strip(b.text), "Map hook payloads") && b.id != "2" {
			t.Errorf("agent line not attributed to its session: %+v", b)
		}
	}
}

func TestRenderAgentsCapped(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	ss := sample(now)
	for i := 0; i < 6; i++ {
		ss[1].Agents = append(ss[1].Agents, session.Agent{ID: string(rune('a' + i)), Type: "Explore", Description: "task " + string(rune('a'+i)), Tool: "Read"})
	}
	m := Model{width: 42, height: 40, now: now, sessions: ss, selected: "2"}
	out := ansi.Strip(Render(m))
	if n := strings.Count(out, "↳"); n != maxAgentLines+1 {
		t.Errorf("want %d agent rows (incl. the +more row), got %d:\n%s", maxAgentLines+1, n, out)
	}
	if !strings.Contains(out, "+2 more") {
		t.Errorf("missing overflow row:\n%s", out)
	}
}

func TestHereMarkerAndDefaultSelection(t *testing.T) {
	now := time.Now()
	// this viewer is pane %s1 in window @2; the session pane there is %1
	m := Model{width: 42, height: 24, now: now, sessions: sample(now), ownPane: "%s1"}
	m.panes = map[string]tmuxctl.Pane{
		"%s1": {PaneID: "%s1", WindowID: "@2", Sidebar: true},
		"%1":  {PaneID: "%1", WindowID: "@2", Active: true},
		"%2":  {PaneID: "%2", WindowID: "@1", Active: true},
	}
	m.here = m.herePane()
	if m.here != "%1" {
		t.Fatalf("here = %q", m.here)
	}
	m.ensureSelection()
	if m.selected != "1" {
		t.Errorf("initial selection should be the session in this window, got %q", m.selected)
	}
	out := ansi.Strip(Render(m))
	marked := 0
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, hereMark) {
			marked++
			if !(strings.Contains(l, "miivo-api") || strings.Contains(l, "Fix auth") || strings.Contains(l, "mobile client") || strings.Contains(l, "fix/auth") || strings.Contains(l, "Bash touch")) {
				t.Errorf("marker on the wrong card: %q", l)
			}
		}
	}
	// repo/status, two title lines, the prompt detail
	if marked != 4 {
		t.Errorf("expected 4 marked lines for the current card, got %d:\n%s", marked, out)
	}
	// a viewer with no session in its window marks nothing
	m2 := Model{width: 42, height: 24, now: now, sessions: sample(now), ownPane: "%s9"}
	m2.panes = map[string]tmuxctl.Pane{"%s9": {PaneID: "%s9", WindowID: "@9", Sidebar: true}, "%77": {PaneID: "%77", WindowID: "@9", Active: true}}
	m2.here = m2.herePane()
	if strings.Contains(ansi.Strip(Render(m2)), hereMark) {
		t.Error("no session in this window: no marker")
	}
}

// The first snapshot a fresh viewer receives was built before its own pane
// existed, so "here" is unknown and the cursor lands on the first row. Once
// a snapshot knows the pane, the cursor must move home, unless the user has
// already moved it.
func TestCursorHomesOnceOwnPaneIsKnown(t *testing.T) {
	now := time.Now()
	m := Model{width: 42, height: 24, now: now, ownPane: "%s1"}
	early := map[string]tmuxctl.Pane{
		"%1": {PaneID: "%1", WindowID: "@2", Active: true},
		"%2": {PaneID: "%2", WindowID: "@1", Active: true},
	}
	mm, _ := m.Update(snapshotMsg(daemon.Snapshot{Sessions: sample(now), Panes: early}))
	m = mm.(Model)
	if m.selected != "2" {
		t.Fatalf("own pane unknown: expected first row, got %q", m.selected)
	}
	later := map[string]tmuxctl.Pane{
		"%s1": {PaneID: "%s1", WindowID: "@2", Sidebar: true},
		"%1":  {PaneID: "%1", WindowID: "@2", Active: true},
		"%2":  {PaneID: "%2", WindowID: "@1", Active: true},
	}
	mm, _ = m.Update(snapshotMsg(daemon.Snapshot{Sessions: sample(now), Panes: later}))
	m = mm.(Model)
	if m.selected != "1" {
		t.Errorf("cursor should move to the session in this window, got %q", m.selected)
	}
	// the user moves: later snapshots leave the cursor alone
	mm, _ = m.key(tea.KeyPressMsg{Code: 'j', Text: "j"})
	m = mm.(Model)
	if m.selected != "3" {
		t.Fatalf("after j: %q", m.selected)
	}
	mm, _ = m.Update(snapshotMsg(daemon.Snapshot{Sessions: sample(now), Panes: later}))
	m = mm.(Model)
	if m.selected != "3" {
		t.Errorf("snapshot must not override a cursor the user moved, got %q", m.selected)
	}
}

// Every row keeps a one-cell margin on the right and starts its text in
// column three: column one is the here-marker gutter, column two is space.
func TestRenderPadding(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	m := Model{width: 42, height: 24, now: now, sessions: sample(now), selected: "1", here: "%1"}
	out := ansi.Strip(Render(m))
	for _, l := range strings.Split(out, "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if ansi.StringWidth(strings.TrimRight(l, " ")) > 41 {
			t.Errorf("text touches the right edge: %q", l)
		}
		r := []rune(l)
		if len(r) < 3 || (r[0] != ' ' && r[0] != '▌') || r[1] != ' ' || r[2] == ' ' {
			t.Errorf("text must start in column three after gutter and space: %q", l)
		}
	}
	if !strings.Contains(out, "▌ 2 miivo-api") {
		t.Errorf("here marker should sit apart from the text:\n%s", out)
	}
}

func TestRenderContextOnWorkingCard(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	ss := sample(now)
	ss[0].ContextTokens = 900000 // needs-you: not shown
	ss[1].ContextTokens = 142345 // working: shown
	m := Model{width: 42, height: 24, now: now, sessions: ss, selected: "2"}
	out := ansi.Strip(Render(m))
	if !strings.Contains(out, "◐ working · 142k") {
		t.Errorf("working card should carry context size:\n%s", out)
	}
	if strings.Contains(out, "900k") {
		t.Errorf("idle cards should not carry context size:\n%s", out)
	}
	for in, want := range map[int64]string{0: "", 950: "950", 1500: "2k", 142345: "142k", 1234567: "1.2M"} {
		if got := fmtTokens(in); got != want {
			t.Errorf("fmtTokens(%d) = %q want %q", in, got, want)
		}
	}
}

func TestRenderIndexAndDigitJump(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	m := Model{width: 42, height: 24, now: now, sessions: sample(now), selected: "2"}
	out := ansi.Strip(Render(m))
	// cards are numbered in display order: window 1, window 2, then non-tmux
	for _, want := range []string{"1 miivo-api · main", "2 miivo-api · fix/auth", "3 dotfiles · main"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	mm, cmd := m.key(tea.KeyPressMsg{Code: '2', Text: "2"})
	m = mm.(Model)
	if m.selected != "1" || cmd == nil {
		t.Errorf("digit 2 should select the second card and jump: selected=%q cmd=%v", m.selected, cmd != nil)
	}
	mm, cmd = m.key(tea.KeyPressMsg{Code: '9', Text: "9"})
	m = mm.(Model)
	if m.selected != "1" || cmd != nil {
		t.Errorf("digit past the end does nothing: selected=%q cmd=%v", m.selected, cmd != nil)
	}
}

func TestRenderNoAcceptHintWhenNotHot(t *testing.T) {
	now := time.Now()
	m := Model{width: 42, height: 24, now: now, sessions: sample(now), selected: "2"}
	out := ansi.Strip(Render(m))
	if strings.Contains(out, "y accept") {
		t.Error("accept hint must only show on a needs-you row")
	}
}

func TestRenderNarrow(t *testing.T) {
	now := time.Now()
	m := Model{width: 24, height: 12, now: now, sessions: sample(now), selected: "1"}
	out := ansi.Strip(Render(m))
	if strings.Contains(out, "fix/auth") {
		t.Error("narrow layout drops the branch line")
	}
	for _, l := range strings.Split(out, "\n") {
		if ansi.StringWidth(l) > 24 {
			t.Errorf("line too wide: %q", l)
		}
	}
}

func TestRenderEmptyAndFilter(t *testing.T) {
	m := Model{width: 42, height: 10, now: time.Now()}
	if out := ansi.Strip(Render(m)); !strings.Contains(out, "No sessions yet") || !strings.Contains(out, "Start claude in any pane") || !strings.Contains(out, "no sessions") {
		t.Errorf("empty:\n%s", out)
	}
	m.sessions = sample(time.Now())
	m.filter = "dotfiles"
	out := ansi.Strip(Render(m))
	if strings.Contains(out, "miivo-api") || !strings.Contains(out, "ghostty theme") {
		t.Errorf("filter:\n%s", out)
	}
	m.filter = "zzz"
	if out := ansi.Strip(Render(m)); !strings.Contains(out, "Nothing matches zzz") {
		t.Errorf("no match:\n%s", out)
	}
}

func TestOrdered(t *testing.T) {
	ss := []session.Session{
		{ID: "out", InTmux: false},
		{ID: "b10", InTmux: true, TmuxSession: "a", TmuxTarget: "a:10.2"},
		{ID: "b2", InTmux: true, TmuxSession: "a", TmuxTarget: "a:2.2"},
		{ID: "z1", InTmux: true, TmuxSession: "z", TmuxTarget: "z:1.1"},
	}
	got := Ordered(ss)
	ids := []string{got[0].ID, got[1].ID, got[2].ID, got[3].ID}
	if strings.Join(ids, ",") != "b2,b10,z1,out" {
		t.Errorf("order: %v", ids)
	}
	// status changes must not reorder
	ss[1].Status = state.StatusNeedsYou
	got = Ordered(ss)
	if got[0].ID != "b2" {
		t.Error("status must not affect order")
	}
}

func TestSelectionAndNavigation(t *testing.T) {
	now := time.Now()
	m := Model{width: 42, height: 24, now: now, sessions: sample(now)}
	m.ensureSelection()
	if m.selected != "2" {
		t.Fatalf("first displayed row (window 1) should be selected, got %q", m.selected)
	}
	m.move(1)
	if m.selected != "1" {
		t.Errorf("move down: %q", m.selected)
	}
	m.move(5)
	if m.selected != "3" {
		t.Errorf("clamp at end: %q", m.selected)
	}
	m.nextHot()
	if m.selected != "1" {
		t.Errorf("nextHot wraps to the needs-you row: %q", m.selected)
	}
	m.sessions = m.sessions[2:] // only dotfiles left
	m.ensureSelection()
	if m.selected != "3" {
		t.Errorf("selection should fall back to first displayed row: %q", m.selected)
	}
}

func TestClampScroll(t *testing.T) {
	var lines []line
	for i := 0; i < 30; i++ {
		id := ""
		if i%3 != 0 {
			id = strings.Repeat("x", i/3+1)
		}
		lines = append(lines, line{id: id})
	}
	sc := clampScroll(lines, strings.Repeat("x", 10), 0, 10)
	if sc != 20 {
		t.Errorf("scroll to end: %d", sc)
	}
	sc = clampScroll(lines, "x", 20, 10)
	if sc != 1 {
		t.Errorf("scroll to start: %d", sc)
	}
}

func TestHelpers(t *testing.T) {
	now := time.Now()
	cases := map[time.Duration]string{
		10 * time.Second:              "",
		5 * time.Minute:               "5m",
		3*time.Hour + 7*time.Minute:   "3h",
		26*time.Hour + 30*time.Minute: "1d",
	}
	for d, want := range cases {
		if got := age(now, now.Add(-d)); got != want {
			t.Errorf("age(%v) = %q want %q", d, got, want)
		}
	}
	if age(now, time.Time{}) != "" {
		t.Error("zero time must be empty")
	}
	if truncate("hello world", 5) != "hell…" || truncate("hi", 5) != "hi" || truncate("x", 0) != "" {
		t.Errorf("truncate: %q", truncate("hello world", 5))
	}
	got := wrapLines("one two three four five six seven eight nine ten", 12, 2)
	if len(got) != 2 || ansi.StringWidth(got[0]) > 12 || !strings.HasSuffix(got[1], "…") {
		t.Errorf("wrapLines: %q", got)
	}
	if got := wrapLines("short", 12, 2); len(got) != 1 || got[0] != "short" {
		t.Errorf("wrapLines short: %q", got)
	}
	if got := wrapLines("averyveryverylongsingleword", 10, 2); len(got) != 1 || ansi.StringWidth(got[0]) > 10 {
		t.Errorf("wrapLines long word: %q", got)
	}
}

func TestDetailNamesTheTool(t *testing.T) {
	for _, c := range []struct{ in, head, rest string }{
		{"Bash go test ./...", "Bash", "go test ./..."},
		{"Read", "Read", ""},
		{"mcp__claude-in-chrome__tabs_close_mcp", "claude-in-chrome · tabs close mcp", ""},
		{"mcp__plugin_context7_context7__query-docs lipgloss", "context7_context7 · query-docs", "lipgloss"},
		{"Claude needs your permission", "", "Claude needs your permission"},
		{"Explore · Bash ls", "", "Explore · Bash ls"},
	} {
		if head, rest := splitDetail(c.in); head != c.head || rest != c.rest {
			t.Errorf("splitDetail(%q) = %q, %q; want %q, %q", c.in, head, rest, c.head, c.rest)
		}
	}
	if got := toolName("mcp__claude-in-chrome__navigate"); got != "navigate" {
		t.Errorf("toolName = %q", got)
	}
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	ss := sample(now)
	ss[1].Detail = "mcp__claude-in-chrome__tabs_close_mcp"
	out := ansi.Strip(Render(Model{width: 42, height: 24, now: now, sessions: ss, selected: "1"}))
	if strings.Contains(out, "mcp__") || !strings.Contains(out, "claude-in-chrome · tabs close mcp") {
		t.Errorf("raw MCP name in:\n%s", out)
	}
}

func TestHeaderCountAndHelpHint(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	one := ansi.Strip(Render(Model{width: 42, height: 24, now: now, sessions: sample(now)[2:], selected: "3"}))
	if !strings.Contains(one, "1 session ") || strings.Contains(one, "1 sessions") {
		t.Errorf("want singular count in:\n%s", one)
	}
	// on a needs-you card the accept hint joins; help must still fit
	hot := ansi.Strip(Render(Model{width: 42, height: 24, now: now, sessions: sample(now), selected: "1"}))
	if !strings.Contains(hot, "y accept") || !strings.Contains(hot, "? help") {
		t.Errorf("want accept and help hints in:\n%s", hot)
	}
}

func TestNarrowFallsBackToProject(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	ss := []session.Session{{ID: "1", Project: "scratch", Status: state.StatusWaiting}}
	out := ansi.Strip(Render(Model{width: 28, height: 10, now: now, sessions: ss, selected: "1"}))
	if !strings.Contains(out, "○ scratch") {
		t.Errorf("want project as the name in:\n%s", out)
	}
}

func popupModel(now time.Time) Model {
	m := Model{width: 42, height: 24, now: now, popup: true, client: "/dev/ttys001", opened: "%2", runner: &fakeRunner{}}
	mm, _ := m.Update(snapshotMsg(daemon.Snapshot{Sessions: sample(now), Panes: map[string]tmuxctl.Pane{
		"%1": {PaneID: "%1", WindowID: "@2"}, "%2": {PaneID: "%2", WindowID: "@1", Command: "claude"},
	}}))
	return mm.(Model)
}

type fakeRunner struct{ calls []string }

func (f *fakeRunner) Run(args ...string) (string, error) {
	f.calls = append(f.calls, strings.Join(args, " "))
	if args[0] == "list-panes" {
		return "work|@2|2|%1|2|claude|1|1||177\nwork|@1|1|%2|2|claude|1|0||177\n", nil
	}
	return "", nil
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

// A popup opens on the oldest request, not on the session you are in: you
// opened it to answer something.
func TestPopupOpensOnOldestRequest(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	m := popupModel(now)
	if m.selected != "1" {
		t.Errorf("cursor should be on the needs-you session, got %q", m.selected)
	}
	if m.here != "%2" {
		t.Errorf("here is the pane the popup was opened from, got %q", m.here)
	}
	// nothing asked: it opens on the session you are in
	ss := sample(now)
	ss[0].Status = state.StatusWaiting
	q := Model{width: 42, height: 24, now: now, popup: true, opened: "%2"}
	mm, _ := q.Update(snapshotMsg(daemon.Snapshot{Sessions: ss}))
	if got := mm.(Model).selected; got != "2" {
		t.Errorf("no request: cursor on the session here, got %q", got)
	}
}

func TestPopupClosesAndJumps(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	m := popupModel(now)
	if _, cmd := m.key(tea.KeyPressMsg{Code: tea.KeyEscape}); !isQuit(cmd) {
		t.Error("esc closes the popup")
	}
	// esc with a search clears the search first
	m.filter = "auth"
	mm, cmd := m.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	if isQuit(cmd) || mm.(Model).filter != "" {
		t.Error("esc clears the search before it closes")
	}

	// enter: the client goes to the pane, then the popup closes
	m = popupModel(now)
	_, cmd = m.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	msg := cmd().(actionMsg)
	if !msg.quit || msg.err != nil {
		t.Fatalf("jump should finish the popup: %+v", msg)
	}
	calls := strings.Join(m.runner.(*fakeRunner).calls, "\n")
	if !strings.Contains(calls, "switch-client -c /dev/ttys001 -t %1") {
		t.Errorf("jump must name the client:\n%s", calls)
	}
	if _, cmd := m.Update(msg); !isQuit(cmd) {
		t.Error("the popup closes after the jump")
	}
	// a sidebar never quits on jump
	s := Model{width: 42, height: 24, now: now, sessions: sample(now), selected: "1", runner: &fakeRunner{}}
	_, cmd = s.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd().(actionMsg).quit {
		t.Error("sidebar stays open")
	}
}

func typed(m Model, text string) Model {
	for _, r := range text {
		mm, _ := m.key(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = mm.(Model)
	}
	return m
}

// In the popup every printable key is search: the letters that are
// actions in a sidebar must only type.
func TestPickerTypingFilters(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	m := popupModel(now)
	m = typed(m, "qyx1")
	if m.filter != "qyx1" || m.confirmKill {
		t.Fatalf("q, y, x and digits type: filter %q kill %v", m.filter, m.confirmKill)
	}
	mm, _ := m.key(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	m = mm.(Model)
	if m.filter != "" {
		t.Fatalf("ctrl+u clears: %q", m.filter)
	}

	// the cursor follows the best match and survives later snapshots
	m = typed(m, "ghostty")
	if m.selected != "3" {
		t.Errorf("cursor on the only match, got %q", m.selected)
	}
	mm, _ = m.Update(snapshotMsg(daemon.Snapshot{Sessions: sample(now)}))
	if got := mm.(Model).selected; got != "3" {
		t.Errorf("a snapshot must not pull the cursor back to the request, got %q", got)
	}
	out := ansi.Strip(Render(m))
	if !strings.Contains(out, "❯ ghostty▏") || !strings.Contains(out, "1/3") || strings.Contains(out, "add rate limiter") {
		t.Errorf("search line and filtered list:\n%s", out)
	}

	// words match in any order, across fields
	m = typed(popupModel(now), "limiter miivo")
	if rows := m.ordered(); len(rows) != 1 || rows[0].ID != "2" {
		t.Errorf("two words, both must match: %+v", rows)
	}
	mm, _ = m.key(tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl})
	if got := mm.(Model).filter; got != "limiter " {
		t.Errorf("ctrl+w deletes a word: %q", got)
	}
	m = typed(popupModel(now), "zzz")
	if out := ansi.Strip(Render(m)); !strings.Contains(out, "Nothing matches zzz") || !strings.Contains(out, "0/3") {
		t.Errorf("no match:\n%s", out)
	}
}

func TestPickerMovesAndActs(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	m := popupModel(now) // order: 2, 1 (needs you, selected), 3
	for _, k := range []tea.KeyPressMsg{{Code: tea.KeyDown}, {Code: 'n', Mod: tea.ModCtrl}} {
		mm, _ := popupModel(now).key(k)
		if got := mm.(Model).selected; got != "3" {
			t.Errorf("%s moves down, got %q", k.String(), got)
		}
	}
	for _, k := range []tea.KeyPressMsg{{Code: tea.KeyUp}, {Code: 'p', Mod: tea.ModCtrl}} {
		mm, _ := popupModel(now).key(k)
		if got := mm.(Model).selected; got != "2" {
			t.Errorf("%s moves up, got %q", k.String(), got)
		}
	}
	// ctrl+y accepts; it was the only request, so the popup is done
	m.panes = map[string]tmuxctl.Pane{"%1": {PaneID: "%1", Command: "claude"}}
	_, cmd := m.key(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	msg := cmd().(actionMsg)
	if msg.err != nil || !msg.quit {
		t.Errorf("accept: %+v", msg)
	}
	if calls := strings.Join(m.runner.(*fakeRunner).calls, "\n"); !strings.Contains(calls, "send-keys -t %1 Enter") {
		t.Errorf("accept presses enter in the pane:\n%s", calls)
	}
	// ctrl+x asks first
	mm, _ := popupModel(now).key(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	if !mm.(Model).confirmKill {
		t.Error("ctrl+x asks before it kills")
	}
}

func TestPopupRender(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	m := popupModel(now)
	m.width = 64
	out := ansi.Strip(Render(m))
	lines := strings.Split(out, "\n")
	if !strings.HasPrefix(lines[0], "  ❯ ▏search sessions") || !strings.HasSuffix(lines[0], "3 · 1 needs you") {
		t.Errorf("search line: %q", lines[0])
	}
	if strings.Trim(lines[1], " ") != strings.Repeat("─", 61) {
		t.Errorf("rule under the search: %q", lines[1])
	}
	for _, l := range lines {
		if l = strings.TrimRight(l, " "); ansi.StringWidth(l) > 63 {
			t.Errorf("text in the right margin (%d): %q", ansi.StringWidth(l), l)
		}
	}
	// no card numbers: digits are search here
	if strings.Contains(out, "1 miivo-api") || !strings.Contains(out, "  miivo-api · fix/auth") {
		t.Errorf("cards carry no number in the popup:\n%s", out)
	}
	for _, want := range []string{"enter jump", "^y accept", "^x kill", "esc close"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing hint %q in:\n%s", want, out)
		}
	}
	// sized to content: every row of the body is a row of the popup
	h := PopupHeight(sample(now), 64)
	m.height = h
	if got := strings.Count(Render(m), "\n") + 1; got != h {
		t.Errorf("PopupHeight %d but the render has %d rows", h, got)
	}
	if !strings.Contains(ansi.Strip(Render(m)), "ghostty theme") {
		t.Errorf("last card must fit:\n%s", ansi.Strip(Render(m)))
	}
}

func TestSetAccent(t *testing.T) {
	title, here := sTitle, sHere
	defer func() { sTitle, sHere = title, here }()
	for _, bad := range []string{"", "green", "#fff", "#{@accent}", "#a6e3a1; rm"} {
		if setAccent(bad) {
			t.Errorf("%q is not a colour", bad)
		}
	}
	if !setAccent(" #a6e3a1\n") {
		t.Fatal("a hex colour sets the accent")
	}
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	out := Render(Model{width: 42, height: 24, now: now, sessions: sample(now), selected: "1", here: "%1"})
	// 166;227;161 is #a6e3a1: on the title and on the here bar
	if n := strings.Count(out, "38;2;166;227;161"); n < 2 {
		t.Errorf("accent used %d times:\n%q", n, strings.Split(out, "\n")[0])
	}
	if strings.Contains(out, "38;2;203;166;247") {
		t.Error("the default accent must be gone")
	}
}
