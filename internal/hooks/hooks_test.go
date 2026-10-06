package hooks

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ssnxd/agentbar/internal/state"
)

func TestMapTable(t *testing.T) {
	cases := []struct {
		event, override, tool  string
		source                 string
		input                  map[string]any
		wantStatus, wantDetail string
		wantDelete             bool
	}{
		{event: "SessionStart", source: "startup", wantStatus: state.StatusWaiting},
		{event: "SessionStart", source: "resume", wantStatus: state.StatusWaiting},
		{event: "SessionStart", source: "compact", wantStatus: state.StatusWorking},
		{event: "UserPromptSubmit", wantStatus: state.StatusWorking},
		{event: "PreToolUse", tool: "Bash", input: map[string]any{"command": "ls"}, wantStatus: state.StatusWorking, wantDetail: "Bash ls"},
		{event: "PermissionRequest", tool: "Bash", input: map[string]any{"command": "git push origin main"}, wantStatus: state.StatusNeedsYou, wantDetail: "Bash git push origin main"},
		{event: "Notification", override: state.StatusNeedsYou, wantStatus: state.StatusNeedsYou},
		{event: "Notification", override: state.StatusWaiting, wantStatus: state.StatusWaiting},
		{event: "Stop", wantStatus: state.StatusDone},
		{event: "StopFailure", wantStatus: state.StatusError},
		{event: "SessionEnd", wantDelete: true},
	}
	for _, c := range cases {
		o := Map(Payload{HookEventName: c.event, ToolName: c.tool, ToolInput: c.input, Source: c.source}, c.override)
		if o.Ignore || o.Delete != c.wantDelete || o.Status != c.wantStatus || (c.wantDetail != "" && o.Detail != c.wantDetail) {
			t.Errorf("%s/%s: got %+v", c.event, c.override, o)
		}
	}
	if o := Map(Payload{HookEventName: "PostToolUse"}, ""); !o.Ignore {
		t.Error("unknown events must be ignored, not mapped")
	}
	if o := Map(Payload{HookEventName: "Notification", Message: "Claude needs your permission"}, state.StatusNeedsYou); o.Detail != "Claude needs your permission" {
		t.Errorf("notification message should become detail: %+v", o)
	}
	if o := Map(Payload{HookEventName: "StopFailure", Message: "rate limited"}, ""); o.Detail != "rate limited" {
		t.Errorf("stop failure message should become detail: %+v", o)
	}
}

func TestMapSubagentEvents(t *testing.T) {
	in := map[string]any{"command": "ls"}
	o := Map(Payload{HookEventName: "SubagentStart", AgentID: "a1", AgentType: "Explore"}, "")
	if o.Ignore || o.Status != "" || o.Agent == nil || o.Agent.ID != "a1" || o.Agent.Type != "Explore" || o.Agent.Done {
		t.Errorf("SubagentStart: %+v", o)
	}
	o = Map(Payload{HookEventName: "SubagentStop", AgentID: "a1", AgentType: "Explore"}, "")
	if o.Ignore || o.Status != "" || o.Agent == nil || o.Agent.ID != "a1" || !o.Agent.Done {
		t.Errorf("SubagentStop: %+v", o)
	}
	// A tool call inside a subagent updates the agent, not the session.
	o = Map(Payload{HookEventName: "PreToolUse", ToolName: "Bash", ToolInput: in, AgentID: "a1", AgentType: "Explore"}, "")
	if o.Ignore || o.Status != "" || o.Agent == nil || o.Agent.Tool != "Bash" || o.Agent.Detail != "Bash ls" || o.Agent.Type != "Explore" {
		t.Errorf("PreToolUse in agent: %+v", o)
	}
	// A permission prompt inside a subagent needs the user just the same,
	// and names the agent.
	o = Map(Payload{HookEventName: "PermissionRequest", ToolName: "Bash", ToolInput: in, AgentID: "a1", AgentType: "Explore"}, "")
	if o.Status != state.StatusNeedsYou || o.Detail != "Explore · Bash ls" || o.Agent == nil || o.Agent.Detail != "Bash ls" {
		t.Errorf("PermissionRequest in agent: %+v", o)
	}
	// Main-thread events carry no agent change.
	if o := Map(Payload{HookEventName: "PreToolUse", ToolName: "Bash", ToolInput: in}, ""); o.Agent != nil {
		t.Errorf("main thread: %+v", o)
	}
}

func TestSummarize(t *testing.T) {
	long := strings.Repeat("x", 200)
	if got := Summarize("Bash", map[string]any{"command": long}); len([]rune(got)) > 80+len("Bash ") {
		t.Errorf("not truncated: %d", len([]rune(got)))
	}
	if got := Summarize("Bash", map[string]any{"command": "git   status\n  -sb"}); got != "Bash git status -sb" {
		t.Errorf("whitespace: %q", got)
	}
	if got := Summarize("Edit", map[string]any{"file_path": "/a/b/c.go"}); got != "Edit c.go" {
		t.Errorf("edit: %q", got)
	}
	if got := Summarize("WebFetch", map[string]any{"url": "https://x"}); got != "WebFetch https://x" {
		t.Errorf("fallback: %q", got)
	}
	if got := Summarize("Foo", nil); got != "Foo" {
		t.Errorf("nil input: %q", got)
	}
}

func TestParsePayload(t *testing.T) {
	in := `{"session_id":"s1","cwd":"/p","hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"ls"},"permission_mode":"default"}`
	p, err := Parse(strings.NewReader(in))
	if err != nil || p.SessionID != "s1" || p.ToolName != "Bash" || p.ToolInput["command"] != "ls" || p.PermissionMode != "default" {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err := Parse(strings.NewReader("")); err == nil {
		t.Error("empty stdin must be an error")
	}
	sub := `{"session_id":"s1","hook_event_name":"SubagentStart","agent_id":"abc","agent_type":"Explore"}`
	if p, err := Parse(strings.NewReader(sub)); err != nil || p.AgentID != "abc" || p.AgentType != "Explore" {
		t.Errorf("subagent fields: %+v %v", p, err)
	}
}

const userSettings = `{
  "model": "claude-fable-5-1[1m]",
  "hooks": {
    "Stop": [{"hooks": [{"type": "command", "command": "osascript -e 'display notification'", "timeout": 10}]}]
  },
  "theme": "auto"
}`

func TestMergeIsIdempotentAndPreservesForeignHooks(t *testing.T) {
	const exe = "/usr/local/bin/agentbar"
	out, changed, err := Merge([]byte(userSettings), exe)
	if err != nil || !changed {
		t.Fatal(err, changed)
	}
	var s map[string]any
	if err := json.Unmarshal(out, &s); err != nil {
		t.Fatal(err)
	}
	if s["model"] != "claude-fable-5-1[1m]" || s["theme"] != "auto" {
		t.Error("foreign top-level keys lost")
	}
	hooks := s["hooks"].(map[string]any)
	stop := hooks["Stop"].([]any)
	if len(stop) != 2 {
		t.Fatalf("Stop should have osascript + agentbar, got %d", len(stop))
	}
	for _, ev := range []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PermissionRequest", "Notification", "Stop", "StopFailure", "SessionEnd", "SubagentStart", "SubagentStop"} {
		if _, ok := hooks[ev]; !ok {
			t.Errorf("missing %s", ev)
		}
	}
	if len(hooks["Notification"].([]any)) != 2 {
		t.Error("Notification needs two matcher groups")
	}
	if strings.Contains(string(out), `&`) || strings.Contains(string(out), `>`) {
		t.Error("HTML escaping must be off")
	}

	again, changed2, err := Merge(out, exe)
	if err != nil || changed2 || string(again) != string(out) {
		t.Error("second merge must be a no-op")
	}

	removed, changed3, err := Remove(again, exe)
	if err != nil || !changed3 {
		t.Fatal("remove should change", err)
	}
	var r map[string]any
	_ = json.Unmarshal(removed, &r)
	rh := r["hooks"].(map[string]any)
	if len(rh["Stop"].([]any)) != 1 {
		t.Error("osascript hook must survive removal")
	}
	if _, ok := rh["SessionStart"]; ok {
		t.Error("agentbar-only events must be removed entirely")
	}
	if r["model"] != "claude-fable-5-1[1m]" {
		t.Error("foreign keys lost on remove")
	}
	if _, c, _ := Remove(removed, exe); c {
		t.Error("removing twice must be a no-op")
	}
}

func TestMergeIntoEmptySettings(t *testing.T) {
	out, changed, err := Merge(nil, "/x/agentbar")
	if err != nil || !changed {
		t.Fatal(err, changed)
	}
	var s map[string]any
	if err := json.Unmarshal(out, &s); err != nil {
		t.Fatal(err)
	}
	if _, ok := s["hooks"].(map[string]any)["Stop"]; !ok {
		t.Error("hooks missing")
	}
}

func TestMissing(t *testing.T) {
	const exe = "/x/agentbar"
	full, _, _ := Merge([]byte(userSettings), exe)
	if got := Missing(full, exe); len(got) != 0 {
		t.Errorf("complete install reports missing %v", got)
	}
	// an install from before subagent support lacks the two agent events
	old := strings.ReplaceAll(string(full), `"SubagentStart"`, `"SubagentStartX"`)
	old = strings.ReplaceAll(old, `"SubagentStop"`, `"SubagentStopX"`)
	got := Missing([]byte(old), exe)
	if len(got) != 2 || got[0] != "SubagentStart" || got[1] != "SubagentStop" {
		t.Errorf("got %v", got)
	}
	if got := Missing([]byte(userSettings), exe); len(got) != len(Entries(exe)) {
		t.Errorf("nothing installed: got %v", got)
	}
}

func TestInstalledExe(t *testing.T) {
	out, _, _ := Merge([]byte(userSettings), "/old/path/agentbar")
	if got := InstalledExe(out); got != "/old/path/agentbar" {
		t.Errorf("got %q", got)
	}
	if got := InstalledExe([]byte(userSettings)); got != "" {
		t.Errorf("expected none, got %q", got)
	}
}
