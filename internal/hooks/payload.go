// Package hooks understands Claude Code hook payloads, maps them to sidebar
// statuses, and installs agentbar's hook entries into settings.json.
package hooks

import (
	"encoding/json"
	"io"
	"path/filepath"
	"sort"
	"strings"
)

// Payload is the subset of hook stdin JSON that agentbar uses.
type Payload struct {
	SessionID        string         `json:"session_id"`
	CWD              string         `json:"cwd"`
	HookEventName    string         `json:"hook_event_name"`
	Message          string         `json:"message"`
	ToolName         string         `json:"tool_name"`
	ToolInput        map[string]any `json:"tool_input"`
	NotificationType string         `json:"notification_type"`
	PermissionMode   string         `json:"permission_mode"`
}

// Parse reads one hook payload. Input is capped at 1MB: tool_input can carry
// whole file contents and we only need a summary.
func Parse(r io.Reader) (Payload, error) {
	var p Payload
	dec := json.NewDecoder(io.LimitReader(r, 1<<20))
	return p, dec.Decode(&p)
}

const maxDetail = 80

// Summarize renders "Tool <what>" for the sidebar's detail line: the command
// for Bash, the file basename for file tools, otherwise the first string
// value. Whitespace is collapsed and the result capped at maxDetail runes.
func Summarize(tool string, input map[string]any) string {
	what := ""
	switch tool {
	case "Bash":
		what, _ = input["command"].(string)
	case "Edit", "Write", "Read", "MultiEdit", "NotebookEdit":
		if fp, ok := input["file_path"].(string); ok {
			what = filepath.Base(fp)
		}
	}
	if what == "" && input != nil {
		keys := make([]string, 0, len(input))
		for k := range input {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if s, ok := input[k].(string); ok && s != "" {
				what = s
				break
			}
		}
	}
	what = strings.Join(strings.Fields(what), " ")
	if r := []rune(what); len(r) > maxDetail {
		what = string(r[:maxDetail-1]) + "…"
	}
	if what == "" {
		return tool
	}
	return tool + " " + what
}
