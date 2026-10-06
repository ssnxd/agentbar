package state

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// A seen mark is when the user last looked at a session that had finished.
// Each is its own file under <dir>/seen/, written by the daemon, so it
// never races a hook on the session's record.

func seenDir(dir string) string { return filepath.Join(dir, "seen") }

func seenPath(dir, id string) string { return filepath.Join(seenDir(dir), safe(id)) }

// MarkSeen records that the session was seen at t.
func MarkSeen(dir, id string, t time.Time) error {
	return writeAtomic(seenDir(dir), seenPath(dir, id), []byte(t.Format(time.RFC3339Nano)))
}

// ReadSeen returns every readable seen mark keyed by session id.
func ReadSeen(dir string) map[string]time.Time {
	out := map[string]time.Time{}
	entries, err := os.ReadDir(seenDir(dir))
	if err != nil {
		return out
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(seenDir(dir), name))
		if err != nil {
			continue
		}
		if t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(b))); err == nil {
			out[name] = t
		}
	}
	return out
}

// DeleteSeen removes the mark for id. A missing mark is not an error.
func DeleteSeen(dir, id string) error {
	err := os.Remove(seenPath(dir, id))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// SweepSeen removes the marks of sessions not in live. Returns the number
// removed.
func SweepSeen(dir string, live map[string]bool) int {
	n := 0
	for id := range ReadSeen(dir) {
		if !live[id] && DeleteSeen(dir, id) == nil {
			n++
		}
	}
	return n
}
