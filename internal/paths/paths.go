// Package paths centralises every filesystem location agentbar reads or
// writes. Env overrides exist for tests and for users with unusual layouts.
package paths

import (
	"os"
	"path/filepath"
)

func home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return h
}

// DataDir is ~/.local/share/agentbar (AGENTBAR_STATE_DIR overrides).
func DataDir() string {
	if d := os.Getenv("AGENTBAR_STATE_DIR"); d != "" {
		return d
	}
	return filepath.Join(home(), ".local", "share", "agentbar")
}

// StateDir holds one JSON record per session, written by the hook receiver.
func StateDir() string { return filepath.Join(DataDir(), "state") }

// LogPath is where the hook receiver logs problems (it never prints).
func LogPath() string { return filepath.Join(DataDir(), "agentbar.log") }

// ClaudeDir is ~/.claude (AGENTBAR_CLAUDE_DIR overrides).
func ClaudeDir() string {
	if d := os.Getenv("AGENTBAR_CLAUDE_DIR"); d != "" {
		return d
	}
	return filepath.Join(home(), ".claude")
}

// SettingsPath is the user's global Claude Code settings file.
func SettingsPath() string { return filepath.Join(ClaudeDir(), "settings.json") }

// SessionsDir is Claude Code's own registry of running sessions.
func SessionsDir() string { return filepath.Join(ClaudeDir(), "sessions") }

// ProjectsDir holds transcripts, one subdirectory per munged cwd.
func ProjectsDir() string { return filepath.Join(ClaudeDir(), "projects") }
