// Package tmux wraps the dedicated workflow tmux server. All agent processes
// live under this server; workflow itself never parents them. Targets are
// tmux IDs ($n session, @n window, %n pane) captured at creation, never names.
package tmux

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

type Client struct {
	Socket   string // tmux -L socket name
	ConfPath string // managed server config, passed with -f on every call
}

func New(socket, confPath string) *Client {
	return &Client{Socket: socket, ConfPath: confPath}
}

// ErrNoServer marks "the workflow tmux server is not running", which callers
// treat as "zero live sessions", not as a failure.
type ErrNoServer struct{ Stderr string }

func (e ErrNoServer) Error() string { return "tmux server not running" }

func (c *Client) cmd(args ...string) *exec.Cmd {
	full := append([]string{"-L", c.Socket, "-f", c.ConfPath}, args...)
	return exec.Command("tmux", full...)
}

func (c *Client) run(args ...string) (string, error) {
	cmd := c.cmd(args...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	if err != nil {
		stderr := strings.TrimSpace(errb.String())
		if strings.Contains(stderr, "no server running") ||
			strings.Contains(stderr, "error connecting to") {
			return "", ErrNoServer{Stderr: stderr}
		}
		return "", fmt.Errorf("tmux %s: %w: %s", args[0], err, stderr)
	}
	return strings.TrimSpace(out.String()), nil
}

var nameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// SanitizeName makes a string safe for tmux session/window names. Dots and
// colons break tmux's target parser; whitespace breaks everyone's sanity.
func SanitizeName(s string) string {
	s = nameSanitizer.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "unnamed"
	}
	if len(s) > 40 {
		s = s[:40]
	}
	return s
}

// NewSession creates a detached session running command in dir, with env
// entries (KEY=VAL) applied to the initial pane. Returns session, window and
// pane IDs.
func (c *Client) NewSession(name, dir string, env []string, command []string) (sessionID, windowID, paneID string, err error) {
	args := []string{"new-session", "-d", "-s", SanitizeName(name), "-c", dir,
		"-P", "-F", "#{session_id}|#{window_id}|#{pane_id}"}
	for _, e := range env {
		args = append(args, "-e", e)
	}
	args = append(args, "--")
	args = append(args, command...)
	out, err := c.run(args...)
	if err != nil {
		return "", "", "", err
	}
	parts := strings.Split(out, "|")
	if len(parts) != 3 {
		return "", "", "", fmt.Errorf("unexpected new-session output %q", out)
	}
	return parts[0], parts[1], parts[2], nil
}

// NewWindow creates a window in session (by ID) running command in dir.
func (c *Client) NewWindow(sessionID, name, dir string, env []string, command []string) (windowID, paneID string, err error) {
	args := []string{"new-window", "-d", "-t", sessionID + ":", "-n", SanitizeName(name),
		"-c", dir, "-P", "-F", "#{window_id}|#{pane_id}"}
	for _, e := range env {
		args = append(args, "-e", e)
	}
	args = append(args, "--")
	args = append(args, command...)
	out, err := c.run(args...)
	if err != nil {
		return "", "", err
	}
	parts := strings.Split(out, "|")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("unexpected new-window output %q", out)
	}
	return parts[0], parts[1], nil
}

// SendText types text into a pane and submits it. The discipline, learned
// from every prior tool: literal send (-l, -- guard), cancel copy-mode first,
// Enter as a separate keystroke after a short delay.
func (c *Client) SendText(paneID, text string) error {
	if err := c.exitCopyMode(paneID); err != nil {
		return err
	}
	if strings.Contains(text, "\n") {
		if err := c.pasteMultiline(paneID, text); err != nil {
			return err
		}
	} else {
		if _, err := c.run("send-keys", "-t", paneID, "-l", "--", text); err != nil {
			return err
		}
	}
	time.Sleep(150 * time.Millisecond)
	_, err := c.run("send-keys", "-t", paneID, "Enter")
	return err
}

// pasteMultiline routes through a tmux buffer with bracketed paste (-p) so a
// TUI like Claude Code treats embedded newlines as text, not as submissions.
func (c *Client) pasteMultiline(paneID, text string) error {
	load := c.cmd("load-buffer", "-b", "wfmsg", "-")
	load.Stdin = strings.NewReader(text)
	var errb bytes.Buffer
	load.Stderr = &errb
	if err := load.Run(); err != nil {
		return fmt.Errorf("load-buffer: %w: %s", err, errb.String())
	}
	_, err := c.run("paste-buffer", "-p", "-d", "-b", "wfmsg", "-t", paneID)
	return err
}

func (c *Client) exitCopyMode(paneID string) error {
	out, err := c.run("display-message", "-p", "-t", paneID, "#{pane_in_mode}")
	if err != nil {
		return err
	}
	if out == "1" {
		_, err = c.run("send-keys", "-t", paneID, "-X", "cancel")
	}
	return err
}

// SendEnter presses Enter in a pane (used to confirm dialogs).
func (c *Client) SendEnter(paneID string) error {
	_, err := c.run("send-keys", "-t", paneID, "Enter")
	return err
}

// CapturePane returns the visible pane content with SGR colors (-e).
func (c *Client) CapturePane(paneID string) (string, error) {
	cmd := c.cmd("capture-pane", "-p", "-e", "-t", paneID)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("capture-pane: %w: %s", err, errb.String())
	}
	return strings.TrimRight(out.String(), "\n"), nil
}

// PaneInfo is one row of the server-wide poll.
type PaneInfo struct {
	SessionID      string
	SessionName    string
	WindowID       string
	PaneID         string
	CurrentCommand string
	Dead           bool
	DeadStatus     string
	PanesInWindow  int
}

// ListPanes polls the entire server in one call. ErrNoServer means zero
// panes, which callers must treat as "all agents down", not an app error.
func (c *Client) ListPanes() ([]PaneInfo, error) {
	out, err := c.run("list-panes", "-a", "-F",
		"#{session_id}|#{session_name}|#{window_id}|#{pane_id}|#{pane_current_command}|#{pane_dead}|#{pane_dead_status}|#{window_panes}")
	if err != nil {
		return nil, err
	}
	var infos []PaneInfo
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		p := strings.Split(line, "|")
		if len(p) != 8 {
			continue
		}
		infos = append(infos, PaneInfo{
			SessionID:      p[0],
			SessionName:    p[1],
			WindowID:       p[2],
			PaneID:         p[3],
			CurrentCommand: p[4],
			Dead:           p[5] == "1",
			DeadStatus:     p[6],
			PanesInWindow:  atoiSafe(p[7]),
		})
	}
	return infos, nil
}

func atoiSafe(s string) int {
	n := 0
	fmt.Sscanf(s, "%d", &n)
	return n
}

func (c *Client) KillSession(sessionID string) error {
	_, err := c.run("kill-session", "-t", sessionID)
	return err
}

func (c *Client) KillWindow(windowID string) error {
	_, err := c.run("kill-window", "-t", windowID)
	return err
}

func (c *Client) SelectWindow(windowID string) error {
	_, err := c.run("select-window", "-t", windowID)
	return err
}

// RespawnPane restarts a dead pane with a new command (used for recovery).
func (c *Client) RespawnPane(paneID, dir string, command []string) error {
	args := []string{"respawn-pane", "-k", "-c", dir, "-t", paneID, "--"}
	args = append(args, command...)
	_, err := c.run(args...)
	return err
}

// AttachCmd builds the command handed to tea.ExecProcess. TMUX/TMUX_PANE are
// stripped so this works as a nested client when workflow itself runs inside
// the user's own tmux; our server binds Ctrl-Q on the root table, so there is
// no prefix to collide with the outer tmux.
func (c *Client) AttachCmd(sessionID, windowID string) *exec.Cmd {
	cmd := c.cmd("attach-session", "-t", sessionID, ";",
		"select-window", "-t", windowID)
	env := os.Environ()
	clean := env[:0]
	for _, e := range env {
		if strings.HasPrefix(e, "TMUX=") || strings.HasPrefix(e, "TMUX_PANE=") {
			continue
		}
		clean = append(clean, e)
	}
	cmd.Env = clean
	return cmd
}

// InsideTmux reports whether workflow itself runs inside any tmux client.
func InsideTmux() bool { return os.Getenv("TMUX") != "" }

// ServerVersion returns `tmux -V` output, e.g. "tmux 3.7b".
func ServerVersion() (string, error) {
	out, err := exec.Command("tmux", "-V").Output()
	return strings.TrimSpace(string(out)), err
}
