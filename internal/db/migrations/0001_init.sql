-- +goose Up
CREATE TABLE projects (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL,
    path       TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE tasks (
    id                INTEGER PRIMARY KEY,
    project_id        INTEGER NOT NULL REFERENCES projects(id),
    title             TEXT NOT NULL,
    prompt            TEXT NOT NULL,
    status            TEXT NOT NULL DEFAULT 'active', -- active | archived
    branch            TEXT NOT NULL,
    tmux_session_id   TEXT NOT NULL DEFAULT '',
    tmux_session_name TEXT NOT NULL DEFAULT '',
    created_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    archived_at       TEXT
);

CREATE TABLE agents (
    id                INTEGER PRIMARY KEY,
    task_id           INTEGER NOT NULL REFERENCES tasks(id),
    name              TEXT NOT NULL,
    role              TEXT NOT NULL, -- orchestrator | worker
    model             TEXT NOT NULL,
    claude_session_id TEXT NOT NULL UNIQUE,
    tmux_window_id    TEXT NOT NULL DEFAULT '',
    tmux_pane_id      TEXT NOT NULL DEFAULT '',
    worktree_path     TEXT NOT NULL DEFAULT '',
    branch            TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'starting',
    -- starting | working | idle | needs_you | error | done | ended | dead
    summary           TEXT NOT NULL DEFAULT '',
    cost_usd          REAL NOT NULL DEFAULT 0,
    context_pct       REAL NOT NULL DEFAULT 0,
    lines_added       INTEGER NOT NULL DEFAULT 0,
    lines_removed     INTEGER NOT NULL DEFAULT 0,
    created_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE (task_id, name)
);

CREATE TABLE events (
    id         INTEGER PRIMARY KEY,
    agent_id   INTEGER REFERENCES agents(id),
    session_id TEXT NOT NULL,
    event      TEXT NOT NULL,
    detail     TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE messages (
    id         INTEGER PRIMARY KEY,
    task_id    INTEGER NOT NULL REFERENCES tasks(id),
    from_agent TEXT NOT NULL,
    to_agent   TEXT NOT NULL,
    body       TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE INDEX idx_agents_task ON agents(task_id);
CREATE INDEX idx_events_agent ON events(agent_id, created_at);

-- +goose Down
DROP TABLE messages;
DROP TABLE events;
DROP TABLE agents;
DROP TABLE tasks;
DROP TABLE projects;
