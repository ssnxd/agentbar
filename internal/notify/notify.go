// Package notify sends best-effort desktop notifications. Failures are
// silent — a missing notifier must never affect the manager.
package notify

import (
	"fmt"
	"os/exec"
	"runtime"
)

// Send shows a desktop notification (macOS: osascript; Linux: notify-send).
func Send(title, body string) {
	switch runtime.GOOS {
	case "darwin":
		script := fmt.Sprintf("display notification %q with title %q sound name \"Glass\"", body, title)
		go func() { _ = exec.Command("osascript", "-e", script).Run() }()
	case "linux":
		go func() { _ = exec.Command("notify-send", title, body).Run() }()
	}
}
