package db

import (
	"database/sql"
	"errors"
	"fmt"
)

var ErrNotFound = errors.New("not found")

// --- projects ---

func (s *Store) UpsertProject(name, path string) (Project, error) {
	_, err := s.db.Exec(
		`INSERT INTO projects (name, path) VALUES (?, ?)
		 ON CONFLICT(path) DO UPDATE SET name = excluded.name`, name, path)
	if err != nil {
		return Project{}, err
	}
	var p Project
	err = s.db.QueryRow(`SELECT id, name, path FROM projects WHERE path = ?`, path).
		Scan(&p.ID, &p.Name, &p.Path)
	return p, err
}

func (s *Store) ListProjects() ([]Project, error) {
	rows, err := s.db.Query(`SELECT id, name, path FROM projects ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Name, &p.Path); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// --- tasks ---

func (s *Store) CreateTask(projectID int64, title, prompt, branch string) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO tasks (project_id, title, prompt, branch) VALUES (?, ?, ?, ?)`,
		projectID, title, prompt, branch)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) SetTaskSession(taskID int64, sessionID, sessionName string) error {
	_, err := s.db.Exec(
		`UPDATE tasks SET tmux_session_id = ?, tmux_session_name = ? WHERE id = ?`,
		sessionID, sessionName, taskID)
	return err
}

func (s *Store) SetTaskBranch(taskID int64, branch string) error {
	_, err := s.db.Exec(`UPDATE tasks SET branch = ? WHERE id = ?`, branch, taskID)
	return err
}

func (s *Store) SetAgentTmux(id int64, windowID, paneID string) error {
	_, err := s.db.Exec(
		`UPDATE agents SET tmux_window_id = ?, tmux_pane_id = ?, `+touch+` WHERE id = ?`,
		windowID, paneID, id)
	return err
}

func (s *Store) ArchiveTask(taskID int64) error {
	_, err := s.db.Exec(
		`UPDATE tasks SET status = 'archived',
		 archived_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ?`, taskID)
	return err
}

const taskCols = `t.id, t.project_id, t.title, t.prompt, t.status, t.branch,
	t.tmux_session_id, t.tmux_session_name, t.created_at, p.name, p.path`

func scanTask(scan func(...any) error) (Task, error) {
	var t Task
	err := scan(&t.ID, &t.ProjectID, &t.Title, &t.Prompt, &t.Status, &t.Branch,
		&t.TmuxSessionID, &t.TmuxSessionName, &t.CreatedAt, &t.ProjectName, &t.ProjectPath)
	return t, err
}

func (s *Store) GetTask(id int64) (Task, error) {
	row := s.db.QueryRow(
		`SELECT `+taskCols+` FROM tasks t JOIN projects p ON p.id = t.project_id
		 WHERE t.id = ?`, id)
	t, err := scanTask(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return t, fmt.Errorf("task %d: %w", id, ErrNotFound)
	}
	return t, err
}

func (s *Store) ListTasks(includeArchived bool) ([]Task, error) {
	q := `SELECT ` + taskCols + ` FROM tasks t JOIN projects p ON p.id = t.project_id`
	if !includeArchived {
		q += ` WHERE t.status = 'active'`
	}
	q += ` ORDER BY t.id DESC`
	rows, err := s.db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		t, err := scanTask(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// --- agents ---

func (s *Store) CreateAgent(a Agent) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO agents (task_id, name, role, model, claude_session_id,
		   tmux_window_id, tmux_pane_id, worktree_path, branch, status)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.TaskID, a.Name, a.Role, a.Model, a.ClaudeSessionID,
		a.TmuxWindowID, a.TmuxPaneID, a.WorktreePath, a.Branch, StatusStarting)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

const agentCols = `id, task_id, name, role, model, claude_session_id,
	tmux_window_id, tmux_pane_id, worktree_path, branch, status, summary,
	cost_usd, context_pct, lines_added, lines_removed, updated_at`

func scanAgent(scan func(...any) error) (Agent, error) {
	var a Agent
	err := scan(&a.ID, &a.TaskID, &a.Name, &a.Role, &a.Model, &a.ClaudeSessionID,
		&a.TmuxWindowID, &a.TmuxPaneID, &a.WorktreePath, &a.Branch, &a.Status,
		&a.Summary, &a.CostUSD, &a.ContextPct, &a.LinesAdded, &a.LinesRemoved,
		&a.UpdatedAt)
	return a, err
}

func (s *Store) agentBy(where string, arg any) (Agent, error) {
	row := s.db.QueryRow(`SELECT `+agentCols+` FROM agents WHERE `+where, arg)
	a, err := scanAgent(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return a, fmt.Errorf("agent (%s=%v): %w", where, arg, ErrNotFound)
	}
	return a, err
}

func (s *Store) GetAgent(id int64) (Agent, error) { return s.agentBy("id = ?", id) }

func (s *Store) GetAgentBySession(claudeSessionID string) (Agent, error) {
	return s.agentBy("claude_session_id = ?", claudeSessionID)
}

func (s *Store) GetAgentByName(taskID int64, name string) (Agent, error) {
	row := s.db.QueryRow(
		`SELECT `+agentCols+` FROM agents WHERE task_id = ? AND name = ?`, taskID, name)
	a, err := scanAgent(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return a, fmt.Errorf("agent %q in task %d: %w", name, taskID, ErrNotFound)
	}
	return a, err
}

func (s *Store) ListAgents(taskID int64) ([]Agent, error) {
	rows, err := s.db.Query(
		`SELECT `+agentCols+` FROM agents WHERE task_id = ? ORDER BY id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Agent
	for rows.Next() {
		a, err := scanAgent(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) ListAllActiveAgents() ([]Agent, error) {
	rows, err := s.db.Query(
		`SELECT ` + agentCols + ` FROM agents
		 WHERE task_id IN (SELECT id FROM tasks WHERE status = 'active') ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Agent
	for rows.Next() {
		a, err := scanAgent(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

const touch = `updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')`

// SetAgentStatus applies a status transition. done/ended are sticky against
// transient signals (a Stop hook after `agent done` must not flip the agent
// back to idle). dead is deliberately NOT sticky: a hook firing is live
// evidence the session runs, and must win over a stale poll verdict; if the
// pane really is gone, the next reconcile tick re-marks it dead.
func (s *Store) SetAgentStatus(id int64, status string) error {
	_, err := s.db.Exec(
		`UPDATE agents SET status = ?, `+touch+`
		 WHERE id = ?
		   AND NOT (status IN ('done','ended') AND ? IN ('idle','working'))`,
		status, id, status)
	return err
}

func (s *Store) SetAgentSummary(id int64, summary string) error {
	_, err := s.db.Exec(
		`UPDATE agents SET summary = ?, `+touch+` WHERE id = ?`, summary, id)
	return err
}

func (s *Store) SetAgentTelemetry(id int64, cost, ctxPct float64, added, removed int64) error {
	_, err := s.db.Exec(
		`UPDATE agents SET cost_usd = ?, context_pct = ?, lines_added = ?, lines_removed = ?, `+
			touch+` WHERE id = ?`, cost, ctxPct, added, removed, id)
	return err
}

// --- events / messages ---

func (s *Store) InsertEvent(agentID int64, sessionID, event, detail string) error {
	var aid any
	if agentID > 0 {
		aid = agentID
	}
	_, err := s.db.Exec(
		`INSERT INTO events (agent_id, session_id, event, detail) VALUES (?, ?, ?, ?)`,
		aid, sessionID, event, detail)
	return err
}

func (s *Store) InsertMessage(taskID int64, from, to, body string) error {
	_, err := s.db.Exec(
		`INSERT INTO messages (task_id, from_agent, to_agent, body) VALUES (?, ?, ?, ?)`,
		taskID, from, to, body)
	return err
}
