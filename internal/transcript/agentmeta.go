package transcript

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Meta is what Claude Code records about a subagent next to its transcript,
// in <session>/subagents/agent-<id>.meta.json.
type Meta struct {
	Type        string `json:"agentType"`
	Description string `json:"description"`
	Model       string `json:"model"`
}

// AgentMeta reads the meta file of agent agentID belonging to the session
// whose transcript is at transcriptPath. Missing or unreadable yields a zero
// Meta: the file is written when the agent starts, but a hook can arrive
// first.
func AgentMeta(transcriptPath, agentID string) Meta {
	var m Meta
	if transcriptPath == "" || agentID == "" {
		return m
	}
	base := strings.TrimSuffix(transcriptPath, ".jsonl")
	p := filepath.Join(base, "subagents", "agent-"+agentID+".meta.json")
	b, err := os.ReadFile(p)
	if err != nil {
		return m
	}
	_ = json.Unmarshal(b, &m)
	return m
}
