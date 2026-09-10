package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Agent is the last thing agentbar learned about one running subagent. Each
// agent has its own file under <dir>/agents/ so that hooks from parallel
// subagents never race on a shared record.
type Agent struct {
	SessionID string    `json:"session_id"`
	AgentID   string    `json:"agent_id"`
	Type      string    `json:"type"`
	Tool      string    `json:"tool,omitempty"`
	Detail    string    `json:"detail,omitempty"`
	StartedAt time.Time `json:"started_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

const agentSep = "__"

func agentsDir(dir string) string { return filepath.Join(dir, "agents") }

func agentPath(dir, sid, aid string) string {
	return filepath.Join(agentsDir(dir), safe(sid)+agentSep+safe(aid)+".json")
}

// WriteAgent stores a atomically, creating the agents dir if needed.
func WriteAgent(dir string, a Agent) error {
	b, err := json.Marshal(a)
	if err != nil {
		return err
	}
	return writeAtomic(agentsDir(dir), agentPath(dir, a.SessionID, a.AgentID), b)
}

// ReadAgent returns the record for (sid, aid), or false if missing.
func ReadAgent(dir, sid, aid string) (Agent, bool) {
	return readAgentFile(agentPath(dir, sid, aid))
}

func readAgentFile(p string) (Agent, bool) {
	b, err := os.ReadFile(p)
	if err != nil {
		return Agent{}, false
	}
	var a Agent
	if json.Unmarshal(b, &a) != nil || a.SessionID == "" || a.AgentID == "" {
		return Agent{}, false
	}
	return a, true
}

// ReadAgents returns every readable agent record grouped by session id,
// each group ordered by StartedAt then AgentID.
func ReadAgents(dir string) map[string][]Agent {
	out := map[string][]Agent{}
	entries, err := os.ReadDir(agentsDir(dir))
	if err != nil {
		return out
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") || strings.HasPrefix(name, ".") {
			continue
		}
		if a, ok := readAgentFile(filepath.Join(agentsDir(dir), name)); ok {
			out[a.SessionID] = append(out[a.SessionID], a)
		}
	}
	for _, as := range out {
		sort.Slice(as, func(i, j int) bool {
			if !as[i].StartedAt.Equal(as[j].StartedAt) {
				return as[i].StartedAt.Before(as[j].StartedAt)
			}
			return as[i].AgentID < as[j].AgentID
		})
	}
	return out
}

// DeleteAgent removes one agent record. A missing record is not an error.
func DeleteAgent(dir, sid, aid string) error {
	err := os.Remove(agentPath(dir, sid, aid))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// DeleteSessionAgents removes every agent record of session sid.
func DeleteSessionAgents(dir, sid string) error {
	var first error
	for _, a := range ReadAgents(dir)[sid] {
		if err := DeleteAgent(dir, sid, a.AgentID); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// SweepAgents removes agent records whose session is not in live, and
// records not updated for olderThan (a SubagentStop that never arrived).
// Returns the number removed.
func SweepAgents(dir string, live map[string]bool, olderThan time.Duration, now time.Time) int {
	n := 0
	for sid, as := range ReadAgents(dir) {
		for _, a := range as {
			if live[sid] && now.Sub(a.UpdatedAt) < olderThan {
				continue
			}
			if DeleteAgent(dir, sid, a.AgentID) == nil {
				n++
			}
		}
	}
	return n
}
