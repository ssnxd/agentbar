package session

import (
	"testing"
	"time"

	"github.com/ssnxd/agentbar/internal/registry"
	"github.com/ssnxd/agentbar/internal/state"
	"github.com/ssnxd/agentbar/internal/transcript"
)

func TestResolvePriority(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	started := now.Add(-time.Hour)
	e := registry.Entry{SessionID: "s", StartedAt: started}
	fresh := state.Record{SessionID: "s", Status: state.StatusNeedsYou, Detail: "Bash touch x", UpdatedAt: now.Add(-time.Minute)}
	// A record older than the process start comes from a previous process
	// that reused the session id (claude --resume). It must not win.
	stale := state.Record{SessionID: "s", Status: state.StatusWorking, UpdatedAt: started.Add(-time.Minute)}
	waitingReg := registry.Entry{SessionID: "s", StartedAt: started, Status: "waiting", WaitingFor: "permission prompt"}

	cases := []struct {
		name       string
		e          registry.Entry
		rec        state.Record
		has        bool
		ti         transcript.Info
		wantStatus string
		wantDetail string
		wantSource string
	}{
		{"hook wins over everything", e, fresh, true, transcript.Info{Known: true, TurnOpen: false}, state.StatusNeedsYou, "Bash touch x", SourceHook},
		{"hook wins over registry waiting", waitingReg, fresh, true, transcript.Info{}, state.StatusNeedsYou, "Bash touch x", SourceHook},
		{"stale hook ignored, registry waiting", waitingReg, stale, true, transcript.Info{}, state.StatusNeedsYou, "permission prompt", SourceRegistry},
		{"registry busy is not trusted", registry.Entry{SessionID: "s", StartedAt: started, Status: "busy"}, state.Record{}, false, transcript.Info{Known: true, TurnOpen: false, LastActivity: now.Add(-time.Minute)}, state.StatusWaiting, "", SourceTranscript},
		{"transcript closed turn", e, state.Record{}, false, transcript.Info{Known: true, TurnOpen: false, LastActivity: now.Add(-time.Minute)}, state.StatusWaiting, "", SourceTranscript},
		{"transcript open recent", e, state.Record{}, false, transcript.Info{Known: true, TurnOpen: true, LastActivity: now.Add(-5 * time.Second)}, state.StatusWorking, "", SourceTranscript},
		{"transcript open silent", e, state.Record{}, false, transcript.Info{Known: true, TurnOpen: true, LastActivity: now.Add(-2 * time.Minute)}, state.StatusNeedsYou, "probably a prompt (no hook data)", SourceTranscript},
		{"nothing", e, state.Record{}, false, transcript.Info{}, state.StatusUnknown, "", SourceNone},
	}
	for _, c := range cases {
		st, det, src := Resolve(c.e, c.rec, c.has, c.ti, now)
		if st != c.wantStatus || src != c.wantSource || det != c.wantDetail {
			t.Errorf("%s: got %s/%q/%s want %s/%q/%s", c.name, st, det, src, c.wantStatus, c.wantDetail, c.wantSource)
		}
	}
}

func TestBuild(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	started := now.Add(-time.Hour)
	d := Deps{
		Registry: []registry.Entry{
			{SessionID: "a", PID: 1, CWD: "/u/code/app", Name: "app-1a", StartedAt: started, TmuxSession: "w", TmuxWindowID: "@1", TmuxPaneID: "%1"},
			{SessionID: "b", PID: 2, CWD: "/u/other/app", Name: "app-2b", StartedAt: started},
			{SessionID: "c", PID: 3, CWD: "/u/code/site", Name: "site-3c", StartedAt: started},
		},
		States: map[string]state.Record{
			"a": {SessionID: "a", Status: state.StatusNeedsYou, Detail: "Bash ls", UpdatedAt: now.Add(-time.Minute)},
		},
		Transcript: func(cwd, id string) transcript.Info {
			switch id {
			case "a":
				return transcript.Info{Known: true, Title: "Fix auth", Model: "claude-fable-5-1", Branch: "fix/auth", ContextTokens: 1234, LastActivity: now.Add(-2 * time.Minute)}
			case "b":
				return transcript.Info{Known: true, FirstPrompt: "add rate limiter", TurnOpen: false, LastActivity: now.Add(-3 * time.Minute)}
			}
			return transcript.Info{}
		},
		Agents: map[string][]state.Agent{
			"a": {
				{SessionID: "a", AgentID: "x1", Type: "Explore", Tool: "Grep", Detail: "Grep hooks", StartedAt: now.Add(-2 * time.Minute), UpdatedAt: now.Add(-time.Minute)},
				{SessionID: "a", AgentID: "x2", Type: "general-purpose", StartedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-time.Minute)},
			},
		},
		AgentMeta: func(cwd, sid, aid string) transcript.Meta {
			if sid == "a" && aid == "x1" {
				return transcript.Meta{Description: "Map hook payloads", Model: "sonnet"}
			}
			return transcript.Meta{}
		},
		Now: now,
	}
	ss := Build(d)
	if len(ss) != 3 {
		t.Fatalf("got %d sessions", len(ss))
	}
	a, b, c := ss[0], ss[1], ss[2]

	if len(a.Agents) != 2 || len(b.Agents) != 0 {
		t.Fatalf("agents: a=%+v b=%+v", a.Agents, b.Agents)
	}
	x1, x2 := a.Agents[0], a.Agents[1]
	if x1.ID != "x1" || x1.Type != "Explore" || x1.Description != "Map hook payloads" || x1.Model != "sonnet" || x1.Tool != "Grep" || x1.Detail != "Grep hooks" || !x1.StartedAt.Equal(now.Add(-2*time.Minute)) {
		t.Errorf("x1: %+v", x1)
	}
	if x2.ID != "x2" || x2.Type != "general-purpose" || x2.Description != "" {
		t.Errorf("x2 (no meta yet): %+v", x2)
	}

	if a.Project != "code/app" || b.Project != "other/app" || c.Project != "site" {
		t.Errorf("projects: %q %q %q", a.Project, b.Project, c.Project)
	}
	if a.Title != "Fix auth" || b.Title != "add rate limiter" || c.Title != "site-3c" {
		t.Errorf("titles: %q %q %q", a.Title, b.Title, c.Title)
	}
	if a.Status != state.StatusNeedsYou || a.Detail != "Bash ls" || a.Source != SourceHook {
		t.Errorf("a: %+v", a)
	}
	if b.Status != state.StatusWaiting || b.Source != SourceTranscript {
		t.Errorf("b: %+v", b)
	}
	if c.Status != state.StatusUnknown {
		t.Errorf("c: %+v", c)
	}
	if !a.InTmux || a.TmuxPaneID != "%1" || b.InTmux {
		t.Error("InTmux wrong")
	}
	if a.Model != "claude-fable-5-1" || a.Branch != "fix/auth" || a.ContextTokens != 1234 || a.PID != 1 {
		t.Errorf("enrichment: %+v", a)
	}
	// LastActivity is the newest of transcript, hook, start.
	if !a.LastActivity.Equal(now.Add(-time.Minute)) {
		t.Errorf("a.LastActivity = %v", a.LastActivity)
	}
	if !c.LastActivity.Equal(started) {
		t.Errorf("c.LastActivity = %v", c.LastActivity)
	}
}

func TestProjectBase(t *testing.T) {
	cases := map[string]string{
		"/u/code/miivo": "miivo",
		"/u/code/miivo/.claude/worktrees/fix+google-seal": "miivo/fix+google-seal",
		"/u/code/miivo/.claude/worktrees/feat/agentbar":   "agentbar",
		"/u/code/worktrees/x":                             "x",
		"/":                                               "/",
	}
	for in, want := range cases {
		if got := projectBase(in); got != want {
			t.Errorf("%s: got %q want %q", in, got, want)
		}
	}
}

func TestBuildEmpty(t *testing.T) {
	if ss := Build(Deps{Transcript: func(string, string) transcript.Info { return transcript.Info{} }}); len(ss) != 0 {
		t.Errorf("got %+v", ss)
	}
}
