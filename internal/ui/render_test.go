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
		{ID: "1", Project: "miivo-api", Title: "fix auth middleware", Status: state.StatusNeedsYou, Detail: "Bash touch /tmp/agentbar-e2e", TmuxTarget: "work:2.1", TmuxPaneID: "%1", InTmux: true, Model: "claude-fable-5-1", Branch: "fix/auth", LastActivity: now.Add(-12 * time.Minute)},
		{ID: "2", Project: "miivo-api", Title: "add rate limiter", Status: state.StatusWorking, TmuxTarget: "work:3.1", TmuxPaneID: "%2", InTmux: true, Model: "claude-opus-5", Branch: "main", LastActivity: now.Add(-3 * time.Minute)},
		{ID: "3", Project: "dotfiles", Title: "ghostty theme", Status: state.StatusWaiting, Model: "claude-sonnet-5", Branch: "main", LastActivity: now.Add(-40 * time.Minute)},
	}
}

func TestRenderGrouped(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	m := Model{width: 42, height: 20, now: now, sessions: sample(now), selected: "1"}
	out := ansi.Strip(Render(m))
	for _, want := range []string{
		"3 · 1 needs you",
		"miivo-api",
		"● needs you  fix auth middleware",
		"work:2.1 · fable · fix/auth",
		"12m",
		"Bash touch /tmp/agentbar-e2e",
		"◐ working    add rate limiter",
		"dotfiles",
		"○ waiting    ghostty theme",
		"not in tmux · sonnet · main",
		"enter jump",
		"y accept",
		"x kill",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	lines := strings.Split(out, "\n")
	if len(lines) != 20 {
		t.Errorf("want exactly 20 lines for height 20, got %d:\n%s", len(lines), out)
	}
	for _, l := range lines {
		if ansi.StringWidth(l) > 42 {
			t.Errorf("line too wide (%d): %q", ansi.StringWidth(l), l)
		}
	}
	// miivo-api has the needs-you row so it sorts before dotfiles
	if strings.Index(out, "miivo-api") > strings.Index(out, "dotfiles") {
		t.Error("group order wrong")
	}
}

func TestRenderNoAcceptHintWhenNotHot(t *testing.T) {
	now := time.Now()
	m := Model{width: 42, height: 20, now: now, sessions: sample(now), selected: "2"}
	out := ansi.Strip(Render(m))
	if strings.Contains(out, "y accept") {
		t.Error("accept hint must only show on a needs-you row")
	}
}

func TestRenderNarrowDropsDetailLines(t *testing.T) {
	now := time.Now()
	m := Model{width: 24, height: 12, now: now, sessions: sample(now), selected: "1"}
	out := ansi.Strip(Render(m))
	if strings.Contains(out, "work:2.1") {
		t.Error("narrow layout must drop line 2")
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

func TestGroupsOrdering(t *testing.T) {
	now := time.Now()
	ss := []session.Session{
		{ID: "w", Project: "b", Status: state.StatusWaiting},
		{ID: "n", Project: "a", Status: state.StatusNeedsYou, LastActivity: now.Add(-time.Hour)},
		{ID: "k", Project: "a", Status: state.StatusWorking},
		{ID: "n2", Project: "a", Status: state.StatusNeedsYou, LastActivity: now},
	}
	gs := Groups(ss)
	if gs[0].Name != "a" || gs[1].Name != "b" {
		t.Fatalf("group order: %+v", gs)
	}
	ids := []string{gs[0].Rows[0].ID, gs[0].Rows[1].ID, gs[0].Rows[2].ID}
	if ids[0] != "n2" || ids[1] != "n" || ids[2] != "k" {
		t.Errorf("row order: %v", ids)
	}
}

func TestSelectionAndNavigation(t *testing.T) {
	now := time.Now()
	m := Model{width: 42, height: 20, now: now, sessions: sample(now)}
	m.ensureSelection()
	if m.selected != "1" {
		t.Fatalf("first row should be selected, got %q", m.selected)
	}
	m.move(1)
	if m.selected != "2" {
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
	m.sessions = m.sessions[1:] // selected session disappears
	m.ensureSelection()
	// without the needs-you row, dotfiles (waiting) outranks miivo-api (working)
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
	// selected rows at lines 28,29 (id of length 10); heading at 27
	sc := clampScroll(lines, strings.Repeat("x", 10), 0, 10)
	if sc != 20 {
		t.Errorf("scroll to end: %d", sc)
	}
	sc = clampScroll(lines, "x", 20, 10) // first row: lines 1,2 heading 0
	if sc != 0 {
		t.Errorf("scroll to start: %d", sc)
	}
}

func TestHelpers(t *testing.T) {
	if shortModel("claude-fable-5-1") != "fable" || shortModel("claude-haiku-4-5-20251001") != "haiku" || shortModel("gpt") != "gpt" {
		t.Error("shortModel")
	}
	now := time.Now()
	if age(now, now.Add(-10*time.Second)) != "now" || age(now, now.Add(-5*time.Minute)) != "5m" || age(now, now.Add(-3*time.Hour)) != "3h" || age(now, now.Add(-50*time.Hour)) != "2d" || age(now, time.Time{}) != "" {
		t.Error("age")
	}
	if truncate("hello world", 5) != "hell…" || truncate("hi", 5) != "hi" || truncate("x", 0) != "" {
		t.Errorf("truncate: %q", truncate("hello world", 5))
	}
}
