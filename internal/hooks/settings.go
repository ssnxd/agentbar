package hooks

import (
	"bytes"
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"github.com/ssnxd/agentbar/internal/state"
)

// hook config JSON shapes (subset of the Claude Code settings schema).

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

const hookTimeout = 5

// Entries returns the hook groups agentbar installs, keyed by event. Every
// command starts with "<exe> event", which is how Merge and Remove tell
// agentbar's entries from the user's own.
func Entries(exe string) map[string][]hookGroup {
	ev := func(async bool) []hookGroup {
		return []hookGroup{{Hooks: []hookCmd{{Type: "command", Command: exe + " event", Async: async, Timeout: hookTimeout}}}}
	}
	return map[string][]hookGroup{
		"SessionStart":      ev(false),
		"UserPromptSubmit":  ev(false),
		"PreToolUse":        ev(true), // high frequency: async, observe-only
		"SubagentStart":     ev(true), // observe-only
		"SubagentStop":      ev(true),
		"PermissionRequest": ev(false),
		"Stop":              ev(false),
		"StopFailure":       ev(false),
		"SessionEnd":        ev(false),
		"Notification": {
			{
				Matcher: "permission_prompt|elicitation_dialog|agent_needs_input",
				Hooks:   []hookCmd{{Type: "command", Command: exe + " event --status " + state.StatusNeedsYou, Timeout: hookTimeout}},
			},
			{
				Matcher: "idle_prompt",
				Hooks:   []hookCmd{{Type: "command", Command: exe + " event --status " + state.StatusWaiting, Timeout: hookTimeout}},
			},
		},
	}
}

var eventCmdRe = regexp.MustCompile(`"command"\s*:\s*"(\S+/agentbar) event`)

// InstalledExe returns the binary path of any agentbar hooks already in the
// settings, or "" if none are installed. Used to migrate hooks when the
// binary moves.
func InstalledExe(settings []byte) string {
	m := eventCmdRe.FindSubmatch(settings)
	if m == nil {
		return ""
	}
	return string(m[1])
}

// Missing returns the events, in sorted order, for which settings lacks any
// of agentbar's hook groups for exe. Empty means the install is complete;
// an older install reports the events added since.
func Missing(settings []byte, exe string) []string {
	var root map[string]any
	_ = json.Unmarshal(settings, &root)
	hooks, _ := root["hooks"].(map[string]any)
	var out []string
	for event, groups := range Entries(exe) {
		existing, _ := hooks[event].([]any)
		for _, g := range groups {
			want := toAny(g)
			found := false
			for _, e := range existing {
				if jsonEqual(e, want) {
					found = true
					break
				}
			}
			if !found {
				out = append(out, event)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

func isOurs(group any, exe string) bool {
	g, _ := group.(map[string]any)
	hs, _ := g["hooks"].([]any)
	for _, h := range hs {
		hm, _ := h.(map[string]any)
		if c, _ := hm["command"].(string); strings.HasPrefix(c, exe+" event") {
			return true
		}
	}
	return false
}

// toAny round-trips a typed value through JSON so it compares equal to what
// json.Unmarshal produces for the same content.
func toAny(v any) any {
	b, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(b, &out)
	return out
}

// Merge adds agentbar's hook groups to settings JSON, preserving everything
// else byte-for-byte in meaning (keys are re-marshalled). Groups that are
// already present are not duplicated. Returns the new JSON and whether
// anything changed; when nothing changed the input is returned unmodified.
func Merge(settings []byte, exe string) ([]byte, bool, error) {
	var root map[string]any
	if len(bytes.TrimSpace(settings)) == 0 {
		root = map[string]any{}
	} else if err := json.Unmarshal(settings, &root); err != nil {
		return nil, false, err
	}
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	changed := false
	for event, groups := range Entries(exe) {
		existing, _ := hooks[event].([]any)
		for _, g := range groups {
			want := toAny(g)
			dup := false
			for _, e := range existing {
				if jsonEqual(e, want) {
					dup = true
					break
				}
			}
			if !dup {
				existing = append(existing, want)
				changed = true
			}
		}
		hooks[event] = existing
	}
	if !changed {
		return settings, false, nil
	}
	root["hooks"] = hooks
	out, err := marshal(root)
	return out, true, err
}

// Remove strips every hook group that belongs to exe. Events left with no
// groups are deleted; a hooks object left empty is deleted too.
func Remove(settings []byte, exe string) ([]byte, bool, error) {
	var root map[string]any
	if err := json.Unmarshal(settings, &root); err != nil {
		return nil, false, err
	}
	hooks, _ := root["hooks"].(map[string]any)
	changed := false
	for event, v := range hooks {
		groups, _ := v.([]any)
		kept := make([]any, 0, len(groups))
		for _, g := range groups {
			if isOurs(g, exe) {
				changed = true
				continue
			}
			kept = append(kept, g)
		}
		if len(kept) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = kept
		}
	}
	if !changed {
		return settings, false, nil
	}
	if len(hooks) == 0 {
		delete(root, "hooks")
	}
	out, err := marshal(root)
	return out, true, err
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	err := enc.Encode(v)
	return buf.Bytes(), err
}
