package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ssnxd/agentbar/internal/state"
)

// feed runs Event with payload on stdin.
func feed(t *testing.T, payload string, args ...string) {
	t.Helper()
	f := filepath.Join(t.TempDir(), "in.json")
	if err := os.WriteFile(f, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	in, err := os.Open(f)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	old := os.Stdin
	os.Stdin = in
	defer func() { os.Stdin = old }()
	Event(args)
}

func TestEventSubagentLifecycle(t *testing.T) {
	data := t.TempDir()
	t.Setenv("AGENTBAR_STATE_DIR", data)
	dir := filepath.Join(data, "state")

	feed(t, `{"session_id":"s1","cwd":"/p","hook_event_name":"UserPromptSubmit"}`)
	feed(t, `{"session_id":"s1","cwd":"/p","hook_event_name":"SubagentStart","agent_id":"a1","agent_type":"Explore"}`)
	a, ok := state.ReadAgent(dir, "s1", "a1")
	if !ok || a.Type != "Explore" || a.StartedAt.IsZero() {
		t.Fatalf("after start: %+v ok=%v", a, ok)
	}
	started := a.StartedAt

	feed(t, `{"session_id":"s1","cwd":"/p","hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"go test"},"agent_id":"a1","agent_type":"Explore"}`)
	a, _ = state.ReadAgent(dir, "s1", "a1")
	if a.Detail != "Bash go test" || a.Tool != "Bash" || !a.StartedAt.Equal(started) {
		t.Errorf("after tool: %+v", a)
	}
	// the agent's tool call must not become the session's detail
	if r, _ := state.Read(dir, "s1"); r.Event != "UserPromptSubmit" || r.Detail != "" {
		t.Errorf("session touched by agent tool call: %+v", r)
	}

	// a tool call from an agent we never saw start still creates a record
	feed(t, `{"session_id":"s1","cwd":"/p","hook_event_name":"PreToolUse","tool_name":"Read","tool_input":{"file_path":"/x/y.go"},"agent_id":"a2","agent_type":"Plan"}`)
	if a2, ok := state.ReadAgent(dir, "s1", "a2"); !ok || a2.Type != "Plan" || a2.StartedAt.IsZero() {
		t.Errorf("unseen agent: %+v ok=%v", a2, ok)
	}

	feed(t, `{"session_id":"s1","cwd":"/p","hook_event_name":"SubagentStop","agent_id":"a1","agent_type":"Explore"}`)
	if _, ok := state.ReadAgent(dir, "s1", "a1"); ok {
		t.Error("agent record survives SubagentStop")
	}

	feed(t, `{"session_id":"s1","cwd":"/p","hook_event_name":"SessionEnd"}`)
	if _, ok := state.Read(dir, "s1"); ok {
		t.Error("session record survives SessionEnd")
	}
	if agents := state.ReadAgents(dir); len(agents["s1"]) != 0 {
		t.Errorf("agents survive SessionEnd: %+v", agents)
	}
}
