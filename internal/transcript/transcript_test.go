package transcript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const sample = `{"type":"user","message":{"role":"user","content":"fix   the auth\n bug"},"gitBranch":"fix/auth","permissionMode":"default"}
{"type":"assistant","message":{"model":"claude-fable-5-1","usage":{"input_tokens":10,"cache_read_input_tokens":900,"cache_creation_input_tokens":90}}}
{"type":"system","subtype":"turn_duration","isMeta":false}
{"aiTitle":"Fix auth middleware","type":"ai-title"}
`

func TestTail(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	_ = os.WriteFile(p, []byte(sample), 0o644)
	i := Tail(p)
	if !i.Known {
		t.Fatal("not known")
	}
	if i.Title != "Fix auth middleware" || i.FirstPrompt != "fix the auth bug" || i.Model != "claude-fable-5-1" || i.Branch != "fix/auth" || i.ContextTokens != 1000 || i.PermissionMode != "default" {
		t.Errorf("%+v", i)
	}
	if i.TurnOpen {
		t.Error("turn_duration closes the turn")
	}
	if i.LastActivity.IsZero() {
		t.Error("LastActivity should be the file mtime")
	}
}

func TestTailReopensTurnAndInvalidatesCache(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	_ = os.WriteFile(p, []byte(sample), 0o644)
	if Tail(p).TurnOpen {
		t.Fatal("closed turn expected")
	}
	open := sample + `{"type":"assistant","message":{"model":"m"}}` + "\n"
	_ = os.WriteFile(p, []byte(open), 0o644)
	if !Tail(p).TurnOpen {
		t.Error("assistant line after turn_duration reopens the turn; cache must notice the size change")
	}
}

func TestTailLastActivityIgnoresBookkeepingLines(t *testing.T) {
	body := `{"type":"assistant","timestamp":"2026-09-08T08:27:00.000Z","message":{"model":"m"}}
{"type":"system","subtype":"turn_duration","timestamp":"2026-09-08T08:27:29.447Z"}
{"type":"system","subtype":"away_summary","timestamp":"2026-09-08T08:30:38.294Z"}
`
	p := filepath.Join(t.TempDir(), "s.jsonl")
	_ = os.WriteFile(p, []byte(body), 0o644)
	i := Tail(p)
	want := time.Date(2026, 9, 8, 8, 27, 29, 447000000, time.UTC)
	if !i.LastActivity.Equal(want) {
		t.Errorf("LastActivity = %v, want %v (turn_duration, not away_summary)", i.LastActivity, want)
	}
	if i.TurnOpen {
		t.Error("away_summary must not reopen the turn")
	}
}

func TestTailIgnoresUserToolResultsForFirstPrompt(t *testing.T) {
	// A user line whose content is an array (tool_result) is not a prompt.
	body := `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}
{"type":"user","message":{"role":"user","content":"real prompt"}}
`
	p := filepath.Join(t.TempDir(), "s.jsonl")
	_ = os.WriteFile(p, []byte(body), 0o644)
	if got := Tail(p).FirstPrompt; got != "real prompt" {
		t.Errorf("got %q", got)
	}
}

func TestTailOnlyReadsLastChunk(t *testing.T) {
	p := filepath.Join(t.TempDir(), "big.jsonl")
	var b strings.Builder
	b.WriteString(`{"aiTitle":"old title"}` + "\n")
	for i := 0; i < 3000; i++ {
		b.WriteString(`{"type":"assistant","message":{"model":"m","usage":{"input_tokens":1}}}` + "\n")
	}
	b.WriteString(`{"aiTitle":"new title"}` + "\n")
	_ = os.WriteFile(p, []byte(b.String()), 0o644)
	if got := Tail(p).Title; got != "new title" {
		t.Errorf("got %q", got)
	}
}

func TestTailMissing(t *testing.T) {
	if Tail("/nonexistent/x.jsonl").Known {
		t.Error("missing file must not be Known")
	}
}

func TestNewestPrefersSessionFile(t *testing.T) {
	projects := t.TempDir()
	cwd := "/Users/x/code/app"
	dir := filepath.Join(projects, MungeProjectDir(cwd))
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "other.jsonl"), []byte("{}\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "sid.jsonl"), []byte("{}\n"), 0o644)
	// make "other" newer so the fallback would pick it
	future := time.Now().Add(time.Hour)
	_ = os.Chtimes(filepath.Join(dir, "other.jsonl"), future, future)

	if got := Newest(projects, cwd, "sid"); filepath.Base(got) != "sid.jsonl" {
		t.Errorf("got %q", got)
	}
	if got := Newest(projects, cwd, "missing"); filepath.Base(got) != "other.jsonl" {
		t.Errorf("fallback to newest: got %q", got)
	}
	if got := Newest(projects, "/nope", ""); got != "" {
		t.Error("missing dir must yield empty")
	}
}

func TestNewestFindsSessionUnderAnotherProjectDir(t *testing.T) {
	projects := t.TempDir()
	origin := filepath.Join(projects, MungeProjectDir("/u/code/app"))
	_ = os.MkdirAll(origin, 0o755)
	_ = os.WriteFile(filepath.Join(origin, "moved.jsonl"), []byte("{}\n"), 0o644)
	// the session later cd'd into a worktree; its cwd's project dir is empty
	wt := "/u/code/app/.claude/worktrees/feat"
	_ = os.MkdirAll(filepath.Join(projects, MungeProjectDir(wt)), 0o755)
	got := Newest(projects, wt, "moved")
	if filepath.Base(got) != "moved.jsonl" || filepath.Dir(got) != origin {
		t.Errorf("got %q", got)
	}
}

func TestAgentMeta(t *testing.T) {
	dir := t.TempDir()
	tp := filepath.Join(dir, "sid.jsonl")
	_ = os.WriteFile(tp, []byte("{}\n"), 0o644)
	sub := filepath.Join(dir, "sid", "subagents")
	_ = os.MkdirAll(sub, 0o755)
	_ = os.WriteFile(filepath.Join(sub, "agent-abc.meta.json"), []byte(`{"agentType":"general-purpose","description":"Implement Task 2 (Twilio service)","toolUseId":"toolu_1","spawnDepth":1,"requestShape":"background","model":"sonnet"}`), 0o644)
	m := AgentMeta(tp, "abc")
	if m.Description != "Implement Task 2 (Twilio service)" || m.Model != "sonnet" {
		t.Errorf("got %+v", m)
	}
	if m := AgentMeta(tp, "missing"); m.Description != "" {
		t.Errorf("missing meta must be empty, got %+v", m)
	}
	if m := AgentMeta("", "abc"); m.Description != "" {
		t.Errorf("no transcript must be empty, got %+v", m)
	}
}

func TestMungeProjectDir(t *testing.T) {
	if got := MungeProjectDir("/Users/x/code/my.app"); got != "-Users-x-code-my-app" {
		t.Errorf("munge = %q", got)
	}
}
