// Package cli implements agentbar's subcommands.
package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/ssnxd/agentbar/internal/hooks"
	"github.com/ssnxd/agentbar/internal/paths"
	"github.com/ssnxd/agentbar/internal/state"
)

// Event is the hook receiver. It must never fail loudly: any problem is
// logged and the process exits 0 so Claude Code is never blocked and never
// shows an error for a sidebar hiccup.
func Event(args []string) {
	_ = os.MkdirAll(paths.DataDir(), 0o755)
	fs := flag.NewFlagSet("event", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	status := fs.String("status", "", "status override (from Notification matchers)")
	if err := fs.Parse(args); err != nil {
		logf("event: bad args %v: %v", args, err)
		return
	}
	p, err := hooks.Parse(os.Stdin)
	if err != nil {
		logf("event: parse: %v", err)
		return
	}
	if p.SessionID == "" {
		logf("event: %s without session_id", p.HookEventName)
		return
	}
	o := hooks.Map(p, *status)
	dir := paths.StateDir()
	now := time.Now()
	if o.Ignore {
		return
	}
	if o.Delete {
		if err := state.Delete(dir, p.SessionID); err != nil {
			logf("event: delete %s: %v", p.SessionID, err)
		}
		if err := state.DeleteSessionAgents(dir, p.SessionID); err != nil {
			logf("event: delete agents of %s: %v", p.SessionID, err)
		}
		return
	}
	if o.Agent != nil {
		applyAgent(dir, p.SessionID, *o.Agent, now)
	}
	if o.Status == "" {
		return
	}
	err = state.Write(dir, state.Record{
		SessionID: p.SessionID,
		CWD:       p.CWD,
		Status:    o.Status,
		Detail:    o.Detail,
		Tool:      o.Tool,
		Event:     p.HookEventName,
		UpdatedAt: now,
	})
	if err != nil {
		logf("event: write %s: %v", p.SessionID, err)
	}
}

// applyAgent creates, updates, or removes one subagent record. Hooks are
// async and can arrive out of order, so an update from an agent we never saw
// start creates the record, and every event carries the type itself.
func applyAgent(dir, sid string, c hooks.AgentChange, now time.Time) {
	if c.Done {
		if err := state.DeleteAgent(dir, sid, c.ID); err != nil {
			logf("event: delete agent %s/%s: %v", sid, c.ID, err)
		}
		return
	}
	a, ok := state.ReadAgent(dir, sid, c.ID)
	if !ok {
		a = state.Agent{SessionID: sid, AgentID: c.ID, StartedAt: now}
	}
	if c.Type != "" {
		a.Type = c.Type
	}
	if c.Tool != "" {
		a.Tool, a.Detail = c.Tool, c.Detail
	}
	a.UpdatedAt = now
	if err := state.WriteAgent(dir, a); err != nil {
		logf("event: write agent %s/%s: %v", sid, c.ID, err)
	}
}

// logf appends one line to the agentbar log. Logging failures are ignored:
// there is nowhere else to report them from a hook.
func logf(format string, a ...any) {
	f, err := os.OpenFile(paths.LogPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, time.Now().Format(time.RFC3339)+" "+format+"\n", a...)
}
