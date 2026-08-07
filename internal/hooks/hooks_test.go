package hooks

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ssnxd/workflow/internal/db"
)

func TestMapStatus(t *testing.T) {
	cases := []struct {
		event, override, want string
	}{
		{"UserPromptSubmit", "", db.StatusWorking},
		{"PreToolUse", "", db.StatusWorking},
		{"PermissionRequest", "", db.StatusNeedsYou},
		{"Stop", "", db.StatusIdle},
		{"StopFailure", "", db.StatusError},
		{"SessionEnd", "", db.StatusEnded},
		{"Notification", "needs_you", db.StatusNeedsYou},
		{"Notification", "idle", db.StatusIdle},
		{"Notification", "", ""},           // unmatched notification: record only
		{"SomeFutureHook", "", ""},         // unknown events must not change status
		{"Stop", "needs_you", "needs_you"}, // override wins
	}
	for _, c := range cases {
		if got := MapStatus(c.event, c.override); got != c.want {
			t.Errorf("MapStatus(%q, %q) = %q, want %q", c.event, c.override, got, c.want)
		}
	}
}

func TestSettingsJSON(t *testing.T) {
	out, err := SettingsJSON("/usr/local/bin/workflow", db.RoleOrchestrator, []string{"Bash(npm test*)"})
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("settings JSON does not parse: %v", err)
	}
	s := string(out)
	for _, want := range []string{
		"/usr/local/bin/workflow event",
		"--status needs_you",
		"--status idle",
		"statusLine",
		"Bash(workflow *)",
		"Bash(npm test*)",
		"Bash(git merge*)", // orchestrator extra
		"Stop", "StopFailure", "SessionEnd", "PermissionRequest",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("settings JSON missing %q", want)
		}
	}

	worker, err := SettingsJSON("/x/workflow", db.RoleWorker, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(worker), "git merge") {
		t.Error("worker settings must not include orchestrator merge grants")
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}

func TestPayloadParse(t *testing.T) {
	raw := `{"session_id":"abc","transcript_path":"/t.jsonl","cwd":"/w","hook_event_name":"Stop"}`
	var p Payload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	if p.SessionID != "abc" || p.HookEventName != "Stop" {
		t.Errorf("bad parse: %+v", p)
	}
}
