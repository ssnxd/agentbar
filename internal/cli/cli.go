// Package cli implements the plumbing subcommands. They are invoked by
// Claude Code hooks (`event`, `statusline`), by tmux panes (`run-agent`),
// and by agents themselves (`agent`, `msg`). Humans use the TUI.
package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/ssnxd/workflow/internal/config"
	"github.com/ssnxd/workflow/internal/db"
	"github.com/ssnxd/workflow/internal/hooks"
	"github.com/ssnxd/workflow/internal/manager"
	"github.com/ssnxd/workflow/internal/msg"
)

func openManager() (*manager.Manager, error) {
	p, cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	store, err := db.Open(p.DBPath)
	if err != nil {
		return nil, err
	}
	return manager.New(p, cfg, store)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "workflow:", err)
	os.Exit(1)
}

// envAgent resolves the calling agent from the environment that run-agent
// sets. Agent-facing commands (spawn, msg, done) require it.
func envAgent(m *manager.Manager) (db.Agent, error) {
	idStr := os.Getenv("WORKFLOW_AGENT_ID")
	if idStr == "" {
		return db.Agent{}, fmt.Errorf("WORKFLOW_AGENT_ID not set — this command only works inside a workflow agent session")
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return db.Agent{}, fmt.Errorf("bad WORKFLOW_AGENT_ID %q", idStr)
	}
	return m.Store.GetAgent(id)
}

// Event handles a Claude Code hook invocation: stdin carries the hook JSON.
func Event(args []string) {
	fs := flag.NewFlagSet("event", flag.ExitOnError)
	statusOverride := fs.String("status", "", "status override from matcher-scoped hooks")
	_ = fs.Parse(args)

	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	if err != nil {
		fatal(err)
	}
	var p hooks.Payload
	if err := json.Unmarshal(raw, &p); err != nil || p.SessionID == "" {
		// Not our JSON; exit 0 so we never break a Claude session.
		return
	}
	m, err := openManager()
	if err != nil {
		fatal(err)
	}
	defer m.Store.Close()

	agent, err := m.Store.GetAgentBySession(p.SessionID)
	if err != nil {
		// A session we don't manage; record nothing.
		return
	}
	status := hooks.MapStatus(p.HookEventName, *statusOverride)
	_ = m.Store.InsertEvent(agent.ID, p.SessionID, p.HookEventName, p.Message)
	if status != "" {
		if err := m.Store.SetAgentStatus(agent.ID, status); err != nil {
			fatal(err)
		}
	}

	// The one send-keys moment: the agent just went idle and has unread
	// mail — nudge it. Idle sessions accept typed input reliably.
	if status == db.StatusIdle {
		inbox := m.Paths.InboxPath(agent.TaskID, agent.Name)
		if msg.Pending(inbox) > 0 {
			fresh, err := m.Store.GetAgent(agent.ID)
			if err == nil && fresh.Status == db.StatusIdle {
				_ = m.Nudge(fresh)
			}
		}
	}
}

// Statusline consumes Claude Code's statusline JSON: updates telemetry in
// the DB and prints the line shown inside the agent's session.
func Statusline() {
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	if err != nil {
		return
	}
	var in hooks.StatusLineInput
	if err := json.Unmarshal(raw, &in); err != nil || in.SessionID == "" {
		return
	}
	m, err := openManager()
	if err != nil {
		return
	}
	defer m.Store.Close()
	agent, err := m.Store.GetAgentBySession(in.SessionID)
	if err == nil {
		_ = m.Store.SetAgentTelemetry(agent.ID, in.Cost.TotalCostUSD,
			in.ContextWindow.UsedPercentage, in.Cost.TotalLinesAdded, in.Cost.TotalLinesRemoved)
	}
	fmt.Printf("%s · $%.2f · ctx %.0f%% · workflow:%s\n",
		in.Model.DisplayName, in.Cost.TotalCostUSD, in.ContextWindow.UsedPercentage, agent.Name)
}

// RunAgent is the tmux pane command. It execs claude, replacing itself, so
// the pane runs the agent directly and pane death == agent exit.
func RunAgent(args []string) {
	fs := flag.NewFlagSet("run-agent", flag.ExitOnError)
	agentID := fs.Int64("agent-id", 0, "agent row id")
	resume := fs.Bool("resume", false, "resume the claude session instead of starting fresh")
	_ = fs.Parse(args)

	m, err := openManager()
	if err != nil {
		fatal(err)
	}
	a, err := m.Store.GetAgent(*agentID)
	if err != nil {
		fatal(err)
	}
	argv, err := m.ClaudeArgv(a, *resume)
	if err != nil {
		fatal(err)
	}
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		fatal(fmt.Errorf("claude not found in PATH: %w", err))
	}
	// Agents call `workflow msg/agent ...` by name; make sure the directory
	// of this very binary is on their PATH regardless of the user's shell
	// setup.
	selfDir := filepath.Dir(m.Exe)
	var env []string
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "PATH=") {
			e = "PATH=" + selfDir + ":" + strings.TrimPrefix(e, "PATH=")
		}
		env = append(env, e)
	}
	env = append(env,
		"WORKFLOW_AGENT_ID="+fmt.Sprint(a.ID),
		"WORKFLOW_AGENT="+a.Name,
		"WORKFLOW_TASK="+fmt.Sprint(a.TaskID),
		"WORKFLOW_ROLE="+a.Role,
	)
	m.Store.Close()
	argv[0] = claudePath
	if err := syscall.Exec(claudePath, argv, env); err != nil {
		fatal(fmt.Errorf("exec claude: %w", err))
	}
}

// NewTask implements `workflow new` — create a task from the command line
// (the TUI form is the primary path; this suits scripting and testing).
func NewTask(args []string) {
	fs := flag.NewFlagSet("new", flag.ExitOnError)
	repo := fs.String("repo", ".", "path to the git repository")
	title := fs.String("title", "", "short task title")
	prompt := fs.String("prompt", "", "task prompt for the orchestrator")
	_ = fs.Parse(args)
	if *title == "" || *prompt == "" {
		fatal(fmt.Errorf("usage: workflow new --repo <path> --title <t> --prompt <p>"))
	}
	m, err := openManager()
	if err != nil {
		fatal(err)
	}
	defer m.Store.Close()
	id, err := m.NewTask(*repo, *title, *prompt)
	if err != nil {
		fatal(err)
	}
	task, err := m.Store.GetTask(id)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("task #%d created · branch %s · tmux session %s\n", id, task.Branch, task.TmuxSessionName)
	fmt.Println("run `workflow` to watch it, or attach directly:")
	fmt.Printf("  tmux -L %s attach -t '%s'\n", config.TmuxSocket, task.TmuxSessionID)
}

// Agent implements `workflow agent spawn|list|done` for use by agents.
func Agent(args []string) {
	if len(args) == 0 {
		fatal(fmt.Errorf("usage: workflow agent spawn|list|done"))
	}
	m, err := openManager()
	if err != nil {
		fatal(err)
	}
	defer m.Store.Close()
	self, err := envAgent(m)
	if err != nil {
		fatal(err)
	}

	switch args[0] {
	case "spawn":
		fs := flag.NewFlagSet("spawn", flag.ExitOnError)
		name := fs.String("name", "", "worker name (short, kebab-case)")
		model := fs.String("model", "", "model alias (default from config)")
		prompt := fs.String("prompt", "", "the worker's full assignment")
		_ = fs.Parse(args[1:])
		if *name == "" || *prompt == "" {
			fatal(fmt.Errorf("spawn requires --name and --prompt"))
		}
		if self.Role != db.RoleOrchestrator {
			fatal(fmt.Errorf("only the orchestrator can spawn workers"))
		}
		worker, err := m.SpawnWorker(self.TaskID, *name, *model, *prompt)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("spawned worker %q (model %s) on branch %s\n", worker.Name, worker.Model, worker.Branch)

	case "list":
		agents, err := m.Store.ListAgents(self.TaskID)
		if err != nil {
			fatal(err)
		}
		for _, a := range agents {
			fmt.Printf("%-20s %-13s %-8s %-7s %s\n", a.Name, a.Role, a.Model, a.Status, a.Branch)
		}

	case "done":
		fs := flag.NewFlagSet("done", flag.ExitOnError)
		summary := fs.String("summary", "", "one-line result summary")
		_ = fs.Parse(args[1:])
		if err := m.Store.SetAgentSummary(self.ID, *summary); err != nil {
			fatal(err)
		}
		if err := m.Store.SetAgentStatus(self.ID, db.StatusDone); err != nil {
			fatal(err)
		}
		fmt.Println("recorded. Now message the orchestrator with anything it needs to merge your branch.")

	default:
		fatal(fmt.Errorf("unknown agent subcommand %q", args[0]))
	}
}

// Msg implements `workflow msg send|read` for use by agents.
func Msg(args []string) {
	if len(args) == 0 {
		fatal(fmt.Errorf("usage: workflow msg send --to <agent> <body> | workflow msg read"))
	}
	m, err := openManager()
	if err != nil {
		fatal(err)
	}
	defer m.Store.Close()
	self, err := envAgent(m)
	if err != nil {
		fatal(err)
	}

	switch args[0] {
	case "send":
		fs := flag.NewFlagSet("send", flag.ExitOnError)
		to := fs.String("to", "", "recipient agent name")
		_ = fs.Parse(args[1:])
		body := ""
		if fs.NArg() > 0 {
			body = fs.Arg(0)
		}
		if *to == "" || body == "" {
			fatal(fmt.Errorf(`usage: workflow msg send --to <agent> "message"`))
		}
		recipient, err := m.Store.GetAgentByName(self.TaskID, *to)
		if err != nil {
			fatal(err)
		}
		inbox := m.Paths.InboxPath(self.TaskID, recipient.Name)
		if err := msg.Send(inbox, self.Name, body); err != nil {
			fatal(err)
		}
		_ = m.Store.InsertMessage(self.TaskID, self.Name, recipient.Name, body)
		// If the recipient is already idle it will not get a Stop hook, so
		// nudge it now.
		if recipient.Status == db.StatusIdle {
			_ = m.Nudge(recipient)
		}
		fmt.Printf("sent to %s\n", recipient.Name)

	case "read":
		inbox := m.Paths.InboxPath(self.TaskID, self.Name)
		msgs, err := msg.Drain(inbox)
		if err != nil {
			fatal(err)
		}
		if len(msgs) == 0 {
			fmt.Println("inbox empty")
			return
		}
		for _, mm := range msgs {
			fmt.Printf("--- from %s at %s ---\n%s\n", mm.From, mm.At.Format("15:04:05"), mm.Body)
		}

	default:
		fatal(fmt.Errorf("unknown msg subcommand %q", args[0]))
	}
}
