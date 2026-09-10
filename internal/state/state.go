// Package state stores one small JSON record per Claude session, written by
// the hook receiver and read by the sidebar. Files live in
// ~/.local/share/agentbar/state/<session_id>.json and are written
// atomically so a reader never sees a torn record.
package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Status values shown in the sidebar. Exact strings; they also travel on the
// --status flag of matcher-scoped Notification hooks.
const (
	StatusNeedsYou = "needs-you"
	StatusWorking  = "working"
	StatusWaiting  = "waiting"
	StatusError    = "error"
	StatusUnknown  = "unknown"
)

// Record is the last thing agentbar learned about a session from a hook.
type Record struct {
	SessionID string    `json:"session_id"`
	CWD       string    `json:"cwd"`
	Status    string    `json:"status"`
	Detail    string    `json:"detail,omitempty"`
	Tool      string    `json:"tool,omitempty"`
	Event     string    `json:"event"`
	UpdatedAt time.Time `json:"updated_at"`
}

func path(dir, id string) string { return filepath.Join(dir, safe(id)+".json") }

// safe keeps ids filesystem-friendly. Session ids are UUIDs, but the hook
// payload is external input and must not be able to escape the state dir.
func safe(id string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == 0 {
			return '_'
		}
		return r
	}, id)
}

// Write stores r atomically (temp file + rename), creating dir if needed.
func Write(dir string, r Record) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return writeAtomic(dir, path(dir, r.SessionID), b)
}

// writeAtomic writes b to dst via a temp file in dir and a rename, creating
// dir if needed.
func writeAtomic(dir, dst string, b []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), dst)
}

// Read returns the record for id, or false if missing or unreadable.
func Read(dir, id string) (Record, bool) {
	b, err := os.ReadFile(path(dir, id))
	if err != nil {
		return Record{}, false
	}
	var r Record
	if json.Unmarshal(b, &r) != nil || r.SessionID == "" {
		return Record{}, false
	}
	return r, true
}

// ReadAll returns every readable record keyed by session id. Temp files and
// garbage are skipped silently: this is a best-effort widget.
func ReadAll(dir string) map[string]Record {
	out := map[string]Record{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") || strings.HasPrefix(name, ".") {
			continue
		}
		if r, ok := Read(dir, strings.TrimSuffix(name, ".json")); ok {
			out[r.SessionID] = r
		}
	}
	return out
}

// Delete removes the record for id. A missing record is not an error.
func Delete(dir, id string) error {
	err := os.Remove(path(dir, id))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Sweep removes records for sessions not in live whose UpdatedAt is older
// than olderThan. Returns the number removed.
func Sweep(dir string, live map[string]bool, olderThan time.Duration, now time.Time) int {
	n := 0
	for id, r := range ReadAll(dir) {
		if live[id] || now.Sub(r.UpdatedAt) < olderThan {
			continue
		}
		if Delete(dir, id) == nil {
			n++
		}
	}
	return n
}
