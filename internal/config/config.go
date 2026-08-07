// Package config owns filesystem layout and user configuration for workflow.
//
// Layout under the data dir (~/.local/share/workflow):
//
//	workflow.db            SQLite store
//	tmux.conf              managed tmux server config (rewritten each start)
//	claude/.claude/skills/ skills injected into agents via --add-dir
//	worktrees/<task>/<agent>/
//	tasks/<task>/inboxes/<agent>.json
//	config.json            user-editable settings
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/ssnxd/workflow/assets"
)

// TmuxSocket is the dedicated tmux server socket name (tmux -L workflow).
const TmuxSocket = "workflow"

// Config is the user-editable configuration, stored as config.json.
type Config struct {
	// OrchModel is the model used for orchestrator sessions.
	OrchModel string `json:"orch_model"`
	// DefaultWorkerModel is used when the orchestrator does not pick one.
	DefaultWorkerModel string `json:"default_worker_model"`
	// WorktreeCopy lists file globs (relative to the repo root) copied into
	// each new worktree; untracked essentials like .env do not follow a
	// worktree checkout on their own.
	WorktreeCopy []string `json:"worktree_copy"`
	// PermissionMode is passed to every spawned agent. "bypassPermissions"
	// (yolo — agents never prompt) or "acceptEdits" (edits auto-approved,
	// unlisted commands surface as needs-you in the TUI).
	PermissionMode string `json:"permission_mode"`
	// ExtraAllow adds permission rules to every spawned agent (only
	// meaningful with permission_mode acceptEdits).
	ExtraAllow []string `json:"extra_allow,omitempty"`
	// RepoRoots are scanned (a few levels deep) by the new-task repo picker.
	RepoRoots []string `json:"repo_roots"`
	// PreLand runs (sh -c, in the repo) before a local land; non-zero exit
	// aborts the merge. Put your test/lint gate here.
	PreLand string `json:"pre_land,omitempty"`
	// MaxTaskBudgetUSD warns and asks the orchestrator to wrap up when a
	// task's total agent spend exceeds it. 0 disables.
	MaxTaskBudgetUSD float64 `json:"max_task_budget_usd,omitempty"`
	// MaxConcurrentWorkers caps live workers per task. 0 disables.
	MaxConcurrentWorkers int `json:"max_concurrent_workers,omitempty"`
}

func defaultConfig() Config {
	return Config{
		OrchModel:          "fable",
		DefaultWorkerModel: "sonnet",
		WorktreeCopy:       []string{".env", ".env.*"},
		PermissionMode:     "bypassPermissions",
		RepoRoots:          []string{"~/code"},
	}
}

// Paths resolves every location the app touches.
type Paths struct {
	DataDir      string
	DBPath       string
	TmuxConf     string
	ClaudeAddDir string // passed to claude --add-dir; contains .claude/skills
	WorktreesDir string
	TasksDir     string
	ConfigPath   string
}

func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	data := filepath.Join(home, ".local", "share", "workflow")
	return Paths{
		DataDir:      data,
		DBPath:       filepath.Join(data, "workflow.db"),
		TmuxConf:     filepath.Join(data, "tmux.conf"),
		ClaudeAddDir: filepath.Join(data, "claude"),
		WorktreesDir: filepath.Join(data, "worktrees"),
		TasksDir:     filepath.Join(data, "tasks"),
		ConfigPath:   filepath.Join(data, "config.json"),
	}, nil
}

// Load ensures the data dir exists, materializes embedded assets, and reads
// (or creates) config.json.
func Load() (Paths, Config, error) {
	p, err := DefaultPaths()
	if err != nil {
		return Paths{}, Config{}, err
	}
	for _, dir := range []string{p.DataDir, p.WorktreesDir, p.TasksDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Paths{}, Config{}, err
		}
	}
	if err := materialize(p); err != nil {
		return Paths{}, Config{}, fmt.Errorf("materialize assets: %w", err)
	}
	cfg, err := loadConfig(p.ConfigPath)
	if err != nil {
		return Paths{}, Config{}, err
	}
	return p, cfg, nil
}

func loadConfig(path string) (Config, error) {
	cfg := defaultConfig()
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		out, _ := json.MarshalIndent(cfg, "", "  ")
		return cfg, os.WriteFile(path, append(out, '\n'), 0o644)
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}

// materialize writes embedded assets into the data dir, overwriting so the
// on-disk copies always match the running binary.
func materialize(p Paths) error {
	conf, err := assets.FS.ReadFile("tmux.conf")
	if err != nil {
		return err
	}
	if err := os.WriteFile(p.TmuxConf, conf, 0o644); err != nil {
		return err
	}
	skillsRoot := filepath.Join(p.ClaudeAddDir, ".claude", "skills")
	return fs.WalkDir(assets.FS, "skills", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel("skills", path)
		dst := filepath.Join(skillsRoot, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		raw, err := assets.FS.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, raw, 0o644)
	})
}

// TaskDir returns the per-task state dir (inboxes live here).
func (p Paths) TaskDir(taskID int64) string {
	return filepath.Join(p.TasksDir, fmt.Sprint(taskID))
}

// InboxPath returns an agent's inbox file.
func (p Paths) InboxPath(taskID int64, agent string) string {
	return filepath.Join(p.TaskDir(taskID), "inboxes", agent+".json")
}

// WorktreePath returns an agent's worktree location.
func (p Paths) WorktreePath(taskID int64, agent string) string {
	return filepath.Join(p.WorktreesDir, fmt.Sprint(taskID), agent)
}
