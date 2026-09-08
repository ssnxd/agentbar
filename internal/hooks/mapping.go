package hooks

import "github.com/ssnxd/agentbar/internal/state"

// Outcome is what a hook event means for the session's state record.
type Outcome struct {
	Status string
	Detail string
	Tool   string
	Delete bool // SessionEnd: remove the record
	Ignore bool // an event we record nothing for
}

// Map turns a hook event into a state change. statusOverride comes from the
// --status flag that matcher-scoped Notification hooks pass explicitly,
// because the notification subtype lives in the matcher, not reliably in
// the payload.
func Map(p Payload, statusOverride string) Outcome {
	o := Outcome{Tool: p.ToolName}
	if p.ToolName != "" {
		o.Detail = Summarize(p.ToolName, p.ToolInput)
	}
	if statusOverride != "" {
		o.Status = statusOverride
		if o.Detail == "" {
			o.Detail = p.Message
		}
		return o
	}
	switch p.HookEventName {
	case "SessionStart":
		// A fresh, resumed, or cleared session sits at its prompt. Only a
		// compaction restart happens mid-turn.
		if p.Source == "compact" {
			o.Status = state.StatusWorking
		} else {
			o.Status = state.StatusWaiting
		}
	case "UserPromptSubmit", "PreToolUse":
		o.Status = state.StatusWorking
	case "PermissionRequest":
		o.Status = state.StatusNeedsYou
	case "Stop":
		o.Status = state.StatusWaiting
	case "StopFailure":
		o.Status = state.StatusError
		o.Detail = p.Message
	case "SessionEnd":
		o.Delete = true
	default:
		o.Ignore = true
	}
	return o
}
