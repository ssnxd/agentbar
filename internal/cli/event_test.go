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

// The request names what is asked; the notification that follows it must
// not replace that with "Claude needs your permission".
func TestEventNotificationKeepsTheRequest(t *testing.T) {
	data := t.TempDir()
	t.Setenv("AGENTBAR_STATE_DIR", data)
	dir := filepath.Join(data, "state")

	feed(t, `{"session_id":"s1","cwd":"/p","hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"touch /tmp/x"}}`)
	feed(t, `{"session_id":"s1","cwd":"/p","hook_event_name":"Notification","message":"Claude needs your permission to use Bash"}`, "--status", state.StatusNeedsYou)
	if r, _ := state.Read(dir, "s1"); r.Status != state.StatusNeedsYou || r.Detail != "Bash touch /tmp/x" || r.Tool != "Bash" {
		t.Errorf("request lost: %+v", r)
	}

	// a notification with no request before it still says what it can
	feed(t, `{"session_id":"s2","cwd":"/p","hook_event_name":"UserPromptSubmit"}`)
	feed(t, `{"session_id":"s2","cwd":"/p","hook_event_name":"Notification","message":"Claude needs your input"}`, "--status", state.StatusNeedsYou)
	if r, _ := state.Read(dir, "s2"); r.Detail != "Claude needs your input" {
		t.Errorf("notification alone: %+v", r)
	}

	// once the session moves on, an old request does not come back
	feed(t, `{"session_id":"s1","cwd":"/p","hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"touch /tmp/x"}}`)
	feed(t, `{"session_id":"s1","cwd":"/p","hook_event_name":"Notification","message":"Claude needs your input"}`, "--status", state.StatusNeedsYou)
	if r, _ := state.Read(dir, "s1"); r.Detail != "Claude needs your input" {
		t.Errorf("stale request shown: %+v", r)
	}
}

func TestEventStopIsDoneUntilSeen(t *testing.T) {
	data := t.TempDir()
	t.Setenv("AGENTBAR_STATE_DIR", data)
	dir := filepath.Join(data, "state")

	feed(t, `{"session_id":"s1","cwd":"/p","hook_event_name":"UserPromptSubmit"}`)
	feed(t, `{"session_id":"s1","cwd":"/p","hook_event_name":"Stop"}`)
	done, _ := state.Read(dir, "s1")
	if done.Status != state.StatusDone {
		t.Fatalf("a turn that ended is done: %+v", done)
	}
	// a minute idle at the prompt is not you seeing it
	feed(t, `{"session_id":"s1","cwd":"/p","hook_event_name":"Notification","message":"Claude is waiting for your input"}`, "--status", state.StatusWaiting)
	if r, _ := state.Read(dir, "s1"); r.Status != state.StatusDone || !r.UpdatedAt.Equal(done.UpdatedAt) {
		t.Errorf("the idle notification must leave done as it was: %+v", r)
	}
	// the next prompt moves it on
	feed(t, `{"session_id":"s1","cwd":"/p","hook_event_name":"UserPromptSubmit"}`)
	if r, _ := state.Read(dir, "s1"); r.Status != state.StatusWorking {
		t.Errorf("a new prompt is working: %+v", r)
	}
	// with nothing finished, the idle notification still says waiting
	feed(t, `{"session_id":"s2","cwd":"/p","hook_event_name":"SessionStart","source":"startup"}`)
	feed(t, `{"session_id":"s2","cwd":"/p","hook_event_name":"Notification"}`, "--status", state.StatusWaiting)
	if r, _ := state.Read(dir, "s2"); r.Status != state.StatusWaiting {
		t.Errorf("idle at a fresh prompt is waiting: %+v", r)
	}
}
