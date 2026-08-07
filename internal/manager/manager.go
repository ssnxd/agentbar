// Package manager owns task and agent lifecycle: creating tasks, spawning
// agents into tmux windows, launching claude, reconciling DB state against
// the tmux server, recovery, and archival.
//
// Division of labor: Claude sessions (orchestrator) decide *what* to spawn;
// this package decides *how* — worktrees, branches, windows, settings.
package manager

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/ssnxd/workflow/internal/config"
	"github.com/ssnxd/workflow/internal/db"
	"github.com/ssnxd/workflow/internal/gitx"
	"github.com/ssnxd/workflow/internal/hooks"
	"github.com/ssnxd/workflow/internal/tmux"
)

type Manager struct {
	Paths config.Paths
	Cfg   config.Config
	Store *db.Store
	Tmux  *tmux.Client
	Exe   string // absolute path to the workflow binary

	nudgeMu   sync.Mutex
	lastNudge map[int64]time.Time // agent id → last inbox nudge
}

func New(p config.Paths, cfg config.Config, store *db.Store) (*Manager, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return &Manager{
		Paths:     p,
		Cfg:       cfg,
		Store:     store,
		Tmux:      tmux.New(config.TmuxSocket, p.TmuxConf),
		Exe:       exe,
		lastNudge: make(map[int64]time.Time),
	}, nil
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = slugRe.ReplaceAllString(strings.ToLower(s), "-")
	s = strings.Trim(s, "-")
	if len(s) > 24 {
		s = s[:24]
	}
	if s == "" {
		s = "task"
	}
	return s
}

// NewTask creates the task, its branch, the tmux session, and the
// orchestrator agent in window 0. Returns the task ID.
func (m *Manager) NewTask(repoPath, title, prompt string) (int64, error) {
	repoPath, err := filepath.Abs(repoPath)
	if err != nil {
		return 0, err
	}
	if !gitx.IsRepo(repoPath) {
		return 0, fmt.Errorf("%s is not a git repository", repoPath)
	}
	proj, err := m.Store.UpsertProject(filepath.Base(repoPath), repoPath)
	if err != nil {
		return 0, err
	}
	taskID, err := m.Store.CreateTask(proj.ID, title, prompt, "")
	if err != nil {
		return 0, err
	}
	slug := slugify(title)
	branch := fmt.Sprintf("wf/%d-%s", taskID, slug)
	if err := m.Store.SetTaskBranch(taskID, branch); err != nil {
		return 0, err
	}

	// The orchestrator gets its own worktree, checked out on the task
	// branch. It never edits code there; it reviews and merges.
	orchWT := m.Paths.WorktreePath(taskID, "orchestrator")
	if err := gitx.AddWorktree(repoPath, orchWT, branch, "HEAD"); err != nil {
		return 0, err
	}

	orchPrompt := fmt.Sprintf(
		"Invoke the workflow-orch skill and follow it. Your task:\n\n%s", prompt)
	agentID, err := m.prepareAgent(taskID, "orchestrator", db.RoleOrchestrator,
		m.Cfg.OrchModel, orchWT, branch, orchPrompt)
	if err != nil {
		return 0, err
	}

	sessName := fmt.Sprintf("wf-%d-%s", taskID, slug)
	sessID, winID, paneID, err := m.Tmux.NewSession(sessName, orchWT, nil,
		m.runAgentCmd(agentID, false))
	if err != nil {
		return 0, err
	}
	m.autoTrust(paneID)
	if err := m.Store.SetTaskSession(taskID, sessID, tmux.SanitizeName(sessName)); err != nil {
		return 0, err
	}
	if err := m.Store.SetAgentTmux(agentID, winID, paneID); err != nil {
		return 0, err
	}
	return taskID, nil
}

// SpawnWorker creates a worker agent: branch off the task branch, worktree,
// tmux window, claude session. Called by `workflow agent spawn` from inside
// the orchestrator's session.
func (m *Manager) SpawnWorker(taskID int64, name, model, prompt string) (db.Agent, error) {
	task, err := m.Store.GetTask(taskID)
	if err != nil {
		return db.Agent{}, err
	}
	name = tmux.SanitizeName(name)
	if name == "orchestrator" {
		return db.Agent{}, fmt.Errorf("agent name %q is reserved", name)
	}
	if cap := m.Cfg.MaxConcurrentWorkers; cap > 0 {
		live, err := m.Store.CountLiveWorkers(taskID)
		if err != nil {
			return db.Agent{}, err
		}
		if live >= cap {
			return db.Agent{}, fmt.Errorf(
				"concurrency cap reached (%d live workers, max %d) — wait for a worker to finish or raise max_concurrent_workers", live, cap)
		}
	}
	if model == "" {
		model = m.Cfg.DefaultWorkerModel
	}
	// "--", not "/": git refs cannot nest under an existing branch name
	// (refs/heads/wf/1-x blocks refs/heads/wf/1-x/worker).
	branch := fmt.Sprintf("%s--%s", task.Branch, name)
	wt := m.Paths.WorktreePath(taskID, name)
	if err := gitx.AddWorktree(task.ProjectPath, wt, branch, task.Branch); err != nil {
		return db.Agent{}, err
	}
	if err := gitx.CopyGlobs(task.ProjectPath, wt, m.Cfg.WorktreeCopy); err != nil {
		return db.Agent{}, fmt.Errorf("seed worktree: %w", err)
	}

	workerPrompt := fmt.Sprintf(
		"Invoke the workflow-comms skill to learn the protocol you work under. Your assignment:\n\n%s", prompt)
	agentID, err := m.prepareAgent(taskID, name, db.RoleWorker, model, wt, branch, workerPrompt)
	if err != nil {
		return db.Agent{}, err
	}
	winID, paneID, err := m.Tmux.NewWindow(task.TmuxSessionID, name, wt, nil,
		m.runAgentCmd(agentID, false))
	if err != nil {
		return db.Agent{}, err
	}
	m.autoTrust(paneID)
	if err := m.Store.SetAgentTmux(agentID, winID, paneID); err != nil {
		return db.Agent{}, err
	}
	return m.Store.GetAgent(agentID)
}

// prepareAgent writes the agent's on-disk material (settings, identity,
// prompt) and its DB row. The tmux pane command is a tiny argv —
// `workflow run-agent --agent-id N` — because tmux runs window commands
// through a shell; everything with quoting hazards lives in files.
func (m *Manager) prepareAgent(taskID int64, name, role, model, worktree, branch, prompt string) (int64, error) {
	sessionID := uuid.NewString()
	agentID, err := m.Store.CreateAgent(db.Agent{
		TaskID:          taskID,
		Name:            name,
		Role:            role,
		Model:           model,
		ClaudeSessionID: sessionID,
		WorktreePath:    worktree,
		Branch:          branch,
	})
	if err != nil {
		return 0, err
	}

	dir := m.agentDir(taskID, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	settings, err := hooks.SettingsJSON(m.Exe, role, m.Cfg.ExtraAllow)
	if err != nil {
		return 0, err
	}
	identity := m.identityPrompt(taskID, name, role)
	files := map[string]string{
		"settings.json": string(settings),
		"identity.txt":  identity,
		"prompt.txt":    prompt,
	}
	for fn, content := range files {
		if err := os.WriteFile(filepath.Join(dir, fn), []byte(content), 0o644); err != nil {
			return 0, err
		}
	}
	return agentID, nil
}

func (m *Manager) agentDir(taskID int64, name string) string {
	return filepath.Join(m.Paths.TaskDir(taskID), "agents", name)
}

func (m *Manager) identityPrompt(taskID int64, name, role string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are agent %q (role: %s) in workflow task %d.\n", name, role, taskID)
	fmt.Fprintf(&b, "You run in a managed tmux window; a human can attach and talk to you at any time.\n")
	fmt.Fprintf(&b, "Your inbox: %s\n", m.Paths.InboxPath(taskID, name))
	fmt.Fprintf(&b, "Message other agents with `workflow msg send --to <name> \"...\"`; read yours with `workflow msg read`.\n")
	if role == db.RoleWorker {
		fmt.Fprintf(&b, "The orchestrator is named \"orchestrator\". Work only in your own worktree and branch. ")
		fmt.Fprintf(&b, "When finished and committed, run `workflow agent done --summary \"...\"` and message the orchestrator.\n")
	} else {
		fmt.Fprintf(&b, "Spawn workers with `workflow agent spawn`; see `workflow agent list` for their status.\n")
	}
	return b.String()
}

// autoTrust watches a freshly spawned pane for Claude Code's folder-trust
// dialog and accepts it. The worktree is a checkout of the user's own repo,
// so trust is the correct default; without this, every spawned agent would
// sit blocked until a human attached. Returns once the dialog is handled,
// the session is past it, or the window times out.
func (m *Manager) autoTrust(paneID string) {
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		out, err := m.Tmux.CapturePane(paneID)
		if err != nil {
			return
		}
		low := strings.ToLower(out)
		if strings.Contains(low, "do you trust") || strings.Contains(low, "trust this folder") ||
			strings.Contains(low, "i trust this folder") {
			_ = m.Tmux.SendEnter(paneID)
			return
		}
		// Already at the normal prompt or busy: no dialog is coming.
		if strings.Contains(low, "? for shortcuts") || strings.Contains(low, "esc to interrupt") {
			return
		}
		time.Sleep(400 * time.Millisecond)
	}
}

// runAgentCmd is the pane command: quoting-safe indirection through our own
// binary, which execs claude.
func (m *Manager) runAgentCmd(agentID int64, resume bool) []string {
	cmd := []string{m.Exe, "run-agent", "--agent-id", fmt.Sprint(agentID)}
	if resume {
		cmd = append(cmd, "--resume")
	}
	return cmd
}

// ClaudeArgv builds the final claude command for an agent. Used by
// `workflow run-agent` inside the pane.
func (m *Manager) ClaudeArgv(a db.Agent, resume bool) ([]string, error) {
	dir := m.agentDir(a.TaskID, a.Name)
	// Seed the worktree's project-local settings from the agent's canonical
	// copy. Claude Code watches settings files and hot-reloads hooks, so
	// editing either file reaches the RUNNING session — something an inline
	// --settings argument can never do. Re-seeding here also self-heals
	// after worktree recreation during recovery.
	canonical := filepath.Join(dir, "settings.json")
	if raw, err := os.ReadFile(canonical); err == nil {
		local := filepath.Join(a.WorktreePath, ".claude", "settings.local.json")
		if err := os.MkdirAll(filepath.Dir(local), 0o755); err == nil {
			_ = os.WriteFile(local, raw, 0o644)
		}
	}

	mode := m.Cfg.PermissionMode
	if mode == "" {
		mode = "bypassPermissions"
	}
	argv := []string{
		"claude",
		"--model", a.Model,
		"--permission-mode", mode,
		"--add-dir", m.Paths.ClaudeAddDir,
	}
	if resume {
		return append(argv, "--resume", a.ClaudeSessionID), nil
	}
	identity, err := os.ReadFile(filepath.Join(dir, "identity.txt"))
	if err != nil {
		return nil, err
	}
	prompt, err := os.ReadFile(filepath.Join(dir, "prompt.txt"))
	if err != nil {
		return nil, err
	}
	argv = append(argv,
		"--session-id", a.ClaudeSessionID,
		"--append-system-prompt", string(identity),
		string(prompt),
	)
	return argv, nil
}
