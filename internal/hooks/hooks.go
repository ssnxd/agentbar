// Package hooks generates the per-agent Claude Code settings (hooks,
// permissions, statusline) and maps incoming hook events to agent statuses.
//
// Status ground truth comes from Claude Code itself via hooks — not from
// scraping the tmux pane. Every hook pipes its stdin JSON to `workflow event`,
// which writes to SQLite. This works whether or not the TUI is running.
package hooks

import (
	"encoding/json"

	"github.com/ssnxd/workflow/internal/db"
)

// Payload is the subset of hook stdin JSON that workflow uses.
type Payload struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	CWD            string `json:"cwd"`
	HookEventName  string `json:"hook_event_name"`
	Message        string `json:"message,omitempty"`
	ToolName       string `json:"tool_name,omitempty"`
}

// MapStatus converts a hook event into an agent status. An empty return means
// "record the event, do not change status". statusOverride comes from the
// --status flag that matcher-scoped Notification hooks pass explicitly,
// because the notification subtype is expressed in the matcher, not reliably
// in the payload.
func MapStatus(hookEvent, statusOverride string) string {
	if statusOverride != "" {
		return statusOverride
	}
	switch hookEvent {
	case "UserPromptSubmit", "PreToolUse", "PostToolUse":
		return db.StatusWorking
	case "PermissionRequest":
		return db.StatusNeedsYou
	case "Stop":
		return db.StatusIdle
	case "StopFailure":
		return db.StatusError
	case "SessionEnd":
		return db.StatusEnded
	case "SessionStart":
		return db.StatusWorking
	default:
		return ""
	}
}

// hook config JSON shapes (subset of Claude Code settings schema).

type hookCmd struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Async   bool   `json:"async,omitempty"`
	Timeout int    `json:"timeout,omitempty"`
}

type hookGroup struct {
	Matcher string    `json:"matcher,omitempty"`
	Hooks   []hookCmd `json:"hooks"`
}

type settings struct {
	Hooks       map[string][]hookGroup `json:"hooks"`
	StatusLine  hookCmd                `json:"statusLine"`
	Permissions struct {
		Allow []string `json:"allow,omitempty"`
	} `json:"permissions"`
}

// baseAllow is granted to every agent: its own plumbing plus the git
// subcommands needed to work on and commit to its own branch. Anything else
// prompts, which surfaces as needs_you in the TUI.
var baseAllow = []string{
	"Bash(workflow *)",
	"Bash(git status*)", "Bash(git diff*)", "Bash(git log*)", "Bash(git show*)",
	"Bash(git add*)", "Bash(git commit*)", "Bash(git restore*)", "Bash(git stash*)",
}

// orchAllow extends baseAllow for the orchestrator, which reviews and merges
// worker branches inside its own worktree.
var orchAllow = []string{
	"Bash(git merge*)", "Bash(git branch*)", "Bash(git fetch*)", "Bash(git switch*)",
}

// SettingsJSON builds the settings file content for one agent. exe is the
// absolute path to the workflow binary (hooks must not depend on PATH).
func SettingsJSON(exe, role string, extraAllow []string) ([]byte, error) {
	ev := func(status string, async bool) []hookGroup {
		cmd := exe + " event"
		if status != "" {
			cmd += " --status " + status
		}
		return []hookGroup{{Hooks: []hookCmd{{Type: "command", Command: cmd, Async: async, Timeout: 10}}}}
	}

	var s settings
	s.Hooks = map[string][]hookGroup{
		"SessionStart":      ev("", false),
		"UserPromptSubmit":  ev("", false),
		"PreToolUse":        ev("", true), // high frequency: async, observe-only
		"PermissionRequest": ev("", false),
		"Stop":              ev("", false),
		"StopFailure":       ev("", false),
		"SessionEnd":        ev("", false),
		"Notification": {
			{
				Matcher: "permission_prompt|elicitation_dialog|agent_needs_input",
				Hooks:   []hookCmd{{Type: "command", Command: exe + " event --status needs_you", Timeout: 10}},
			},
			{
				Matcher: "idle_prompt",
				Hooks:   []hookCmd{{Type: "command", Command: exe + " event --status idle", Timeout: 10}},
			},
		},
	}
	s.StatusLine = hookCmd{Type: "command", Command: exe + " statusline"}
	s.Permissions.Allow = append(append([]string{}, baseAllow...), extraAllow...)
	if role == db.RoleOrchestrator {
		s.Permissions.Allow = append(s.Permissions.Allow, orchAllow...)
	}
	return json.MarshalIndent(s, "", "  ")
}

// StatusLineInput is the subset of Claude Code's statusline JSON we consume.
type StatusLineInput struct {
	SessionID string `json:"session_id"`
	Model     struct {
		DisplayName string `json:"display_name"`
	} `json:"model"`
	Cost struct {
		TotalCostUSD      float64 `json:"total_cost_usd"`
		TotalLinesAdded   int64   `json:"total_lines_added"`
		TotalLinesRemoved int64   `json:"total_lines_removed"`
	} `json:"cost"`
	ContextWindow struct {
		UsedPercentage float64 `json:"used_percentage"`
	} `json:"context_window"`
}
