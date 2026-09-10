package hooks

import "github.com/ssnxd/agentbar/internal/state"

// Outcome is what a hook event means for the session's state record.
type Outcome struct {
	Status string
	Detail string
	Tool   string
	Delete bool         // SessionEnd: remove the record
	Ignore bool         // an event we record nothing for
	Agent  *AgentChange // set when the event belongs to a subagent
}

// AgentChange is what a hook event means for one subagent's record. Done
// removes the record; otherwise it is created or updated in place.
type AgentChange struct {
	ID     string
	Type   string
	Tool   string
	Detail string
	Done   bool
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
	if p.AgentID != "" {
		o.Agent = &AgentChange{ID: p.AgentID, Type: p.AgentType, Tool: o.Tool, Detail: o.Detail}
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
	case "UserPromptSubmit":
		o.Status = state.StatusWorking
	case "PreToolUse":
		// Inside a subagent the tool call belongs to the agent's record;
		// the session is already working.
		if o.Agent == nil {
			o.Status = state.StatusWorking
		}
	case "PermissionRequest":
		o.Status = state.StatusNeedsYou
		if o.Agent != nil && o.Agent.Type != "" {
			o.Detail = o.Agent.Type + " · " + o.Detail
		}
	case "SubagentStart":
		if o.Agent == nil {
			o.Ignore = true
		}
	case "SubagentStop":
		if o.Agent == nil {
			o.Ignore = true
		} else {
			o.Agent.Done = true
		}
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
