package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/ssnxd/agentbar/internal/session"
	"github.com/ssnxd/agentbar/internal/state"
)

func sample(now time.Time) []session.Session {
	return []session.Session{
		{ID: "1", Project: "miivo-api", Title: "Fix auth middleware token refresh for the mobile client", Status: state.StatusNeedsYou, Detail: "Bash touch /tmp/agentbar-e2e", TmuxSession: "work", TmuxTarget: "work:2.2", TmuxPaneID: "%1", InTmux: true, Model: "claude-fable-5-1", Branch: "fix/auth", StartedAt: now.Add(-3*time.Hour - 12*time.Minute), LastActivity: now.Add(-12 * time.Minute)},
		{ID: "2", Project: "miivo-api", Title: "add rate limiter", Status: state.StatusWorking, TmuxSession: "work", TmuxTarget: "work:1.2", TmuxPaneID: "%2", InTmux: true, Model: "claude-opus-5", Branch: "main", StartedAt: now.Add(-45 * time.Minute), LastActivity: now.Add(-3 * time.Minute)},
		{ID: "3", Project: "dotfiles", Title: "ghostty theme", Status: state.StatusWaiting, Model: "claude-sonnet-5", Branch: "main", StartedAt: now.Add(-26 * time.Hour), LastActivity: now.Add(-40 * time.Minute)},
	}
}

func TestRenderCards(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	m := Model{width: 42, height: 24, now: now, sessions: sample(now), selected: "1"}
	out := ansi.Strip(Render(m))
	for _, want := range []string{
		"3 · 1 needs you",
		"miivo-api", "3h 12m", // card 1 line 1
		"Fix auth middleware token refresh for", // title wrapped, first line
		"the mobile client",                     // second line
		"fix/auth", "● needs you",
		"Bash touch /tmp/agentbar-e2e",
		"45m", "add rate limiter", "◐ working",
		"dotfiles", "1d 2h", "ghostty theme", "○ waiting",
		"enter jump", "y accept", "x kill",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
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
	// right alignment: uptime ends at the right edge
	for _, l := range lines {
		if strings.Contains(l, "3h 12m") && !strings.HasSuffix(l, "3h 12m") {
			t.Errorf("uptime not right-aligned: %q", l)
		}
		if strings.Contains(l, "● needs you") && !strings.HasSuffix(l, "● needs you") {
			t.Errorf("status not right-aligned: %q", l)
		}
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
	if out := ansi.Strip(Render(m)); !strings.Contains(out, "no claude sessions running") || !strings.Contains(out, "no sessions") {
		t.Errorf("empty:\n%s", out)
	}
	m.sessions = sample(time.Now())
	m.filter = "dotfiles"
	out := ansi.Strip(Render(m))
	if strings.Contains(out, "miivo-api") || !strings.Contains(out, "ghostty theme") {
		t.Errorf("filter:\n%s", out)
	}
	m.filter = "zzz"
	if out := ansi.Strip(Render(m)); !strings.Contains(out, "nothing matches zzz") {
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
		10 * time.Second:              "0m",
		5 * time.Minute:               "5m",
		3*time.Hour + 7*time.Minute:   "3h 07m",
		26*time.Hour + 30*time.Minute: "1d 2h",
	}
	for d, want := range cases {
		if got := uptime(now, now.Add(-d)); got != want {
			t.Errorf("uptime(%v) = %q want %q", d, got, want)
		}
	}
	if uptime(now, time.Time{}) != "" {
		t.Error("zero start must be empty")
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
