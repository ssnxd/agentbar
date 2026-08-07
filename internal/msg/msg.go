// Package msg implements file-based inboxes: one JSON file per agent per
// task. Files, not send-keys, carry message payloads — typing into a busy
// interactive TUI is the least reliable channel there is. The manager only
// ever *nudges* an idle agent to read its inbox.
package msg

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type Message struct {
	From string    `json:"from"`
	Body string    `json:"body"`
	At   time.Time `json:"at"`
}

// withLock serializes inbox mutations across processes via flock on a
// sidecar lock file.
func withLock(inboxPath string, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(inboxPath), 0o755); err != nil {
		return err
	}
	lf, err := os.OpenFile(inboxPath+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer lf.Close()
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lf.Fd()), syscall.LOCK_UN)
	return fn()
}

func read(inboxPath string) ([]Message, error) {
	raw, err := os.ReadFile(inboxPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var msgs []Message
	if len(raw) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(raw, &msgs); err != nil {
		return nil, err
	}
	return msgs, nil
}

func write(inboxPath string, msgs []Message) error {
	out, err := json.MarshalIndent(msgs, "", "  ")
	if err != nil {
		return err
	}
	tmp := inboxPath + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, inboxPath)
}

// Send appends a message to the recipient's inbox.
func Send(inboxPath, from, body string) error {
	return withLock(inboxPath, func() error {
		msgs, err := read(inboxPath)
		if err != nil {
			return err
		}
		msgs = append(msgs, Message{From: from, Body: body, At: time.Now().UTC()})
		return write(inboxPath, msgs)
	})
}

// Drain returns all pending messages and empties the inbox.
func Drain(inboxPath string) ([]Message, error) {
	var msgs []Message
	err := withLock(inboxPath, func() error {
		var err error
		msgs, err = read(inboxPath)
		if err != nil {
			return err
		}
		return write(inboxPath, []Message{})
	})
	return msgs, err
}

// Pending reports how many messages wait in an inbox (0 on any read problem —
// this feeds a nudge, never an error path).
func Pending(inboxPath string) int {
	msgs, err := read(inboxPath)
	if err != nil {
		return 0
	}
	return len(msgs)
}
