// Package tmuxctl drives the user's tmux server: finding the tagged sidebar
// pane, opening and moving it, jumping to session panes, and sending the
// accept keystroke. Everything goes through Runner so tests use a fake.
package tmuxctl

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// Runner executes one tmux command and returns trimmed stdout.
type Runner interface {
	Run(args ...string) (string, error)
}

// Exec is the real tmux. The process environment is passed through: both
// `toggle` (via run-shell) and the sidebar itself run inside the user's
// default server, and switch-client needs the client context TMUX provides.
type Exec struct{}

func (Exec) Run(args ...string) (string, error) {
	cmd := exec.Command("tmux", args...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("tmux %s: %s", args[0], msg)
	}
	return strings.TrimSpace(out.String()), nil
}

// Pane is one row of a server-wide list-panes.
type Pane struct {
	SessionName  string
	WindowID     string // "@1"
	WindowIndex  string // "2" (respects base-index)
	PaneID       string // "%5"
	PaneIndex    string // "1" (respects pane-base-index)
	Command      string // pane_current_command
	Active       bool   // active pane in its window
	WindowActive bool   // window is current in its session
	Sidebar      bool   // pane option @agentbar is set
}

const paneFormat = "#{session_name}|#{window_id}|#{window_index}|#{pane_id}|#{pane_index}|#{pane_current_command}|#{pane_active}|#{window_active}|#{@agentbar}"

// ListPanes lists every pane on the server.
func ListPanes(r Runner) ([]Pane, error) {
	out, err := r.Run("list-panes", "-a", "-F", paneFormat)
	if err != nil {
		return nil, err
	}
	var ps []Pane
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, "|")
		if len(f) != 9 {
			continue
		}
		ps = append(ps, Pane{
			SessionName: f[0], WindowID: f[1], WindowIndex: f[2], PaneID: f[3], PaneIndex: f[4],
			Command: f[5], Active: f[6] == "1", WindowActive: f[7] == "1", Sidebar: f[8] != "",
		})
	}
	return ps, nil
}

// FindSidebar returns the tagged sidebar pane, or nil.
func FindSidebar(ps []Pane) *Pane {
	for i := range ps {
		if ps[i].Sidebar {
			return &ps[i]
		}
	}
	return nil
}

// FindPane returns the pane with id, or nil.
func FindPane(ps []Pane, id string) *Pane {
	for i := range ps {
		if ps[i].PaneID == id {
			return &ps[i]
		}
	}
	return nil
}

// TargetLabel renders "session:window.pane" the way the user addresses it.
func TargetLabel(p Pane) string {
	return fmt.Sprintf("%s:%s.%s", p.SessionName, p.WindowIndex, p.PaneIndex)
}

// Claude Code 2.1.x sets its process title to its version number, so
// pane_current_command shows "2.1.263" rather than "claude". Older builds
// and wrappers show "claude" or "node".
var claudeCmdRe = regexp.MustCompile(`^(claude|node|\d+\.\d+\.\d+)$`)

// LooksLikeClaude reports whether a pane's current command is plausibly a
// Claude Code process. Used as a guard before sending keys.
func LooksLikeClaude(cmd string) bool { return claudeCmdRe.MatchString(cmd) }

// Current resolves the pane the user is in. hint is a pane id passed on the
// command line (run-shell expands #{pane_id}); it wins over TMUX_PANE.
func Current(r Runner, hint string) (paneID, windowID string, err error) {
	args := []string{"display-message", "-p"}
	if hint != "" {
		args = append(args, "-t", hint)
	}
	args = append(args, "#{pane_id} #{window_id}")
	out, err := r.Run(args...)
	if err != nil {
		return "", "", err
	}
	f := strings.Fields(out)
	if len(f) != 2 {
		return "", "", fmt.Errorf("unexpected display-message output %q", out)
	}
	return f[0], f[1], nil
}

// Opts control where and how big the sidebar is.
type Opts struct {
	Side  string // "left" | "right"
	Width int
	Cmd   string // shell command run in the sidebar pane (open only)
}

const (
	DefaultSide  = "left"
	DefaultWidth = 42
)

// LoadOpts reads @agentbar-side and @agentbar-width from tmux's global
// options, falling back to defaults for anything missing or malformed.
func LoadOpts(r Runner) Opts {
	o := Opts{Side: DefaultSide, Width: DefaultWidth}
	if v, err := r.Run("show-options", "-gqv", "@agentbar-side"); err == nil && (v == "left" || v == "right") {
		o.Side = v
	}
	if v, err := r.Run("show-options", "-gqv", "@agentbar-width"); err == nil {
		if n, err := strconv.Atoi(v); err == nil && n >= 20 && n <= 200 {
			o.Width = n
		}
	}
	return o
}

// splitFlags returns the split/join flags for the configured side.
func splitFlags(o Opts) []string {
	flags := []string{"-h"}
	if o.Side != "right" {
		flags = append(flags, "-b")
	}
	return append(flags, "-l", strconv.Itoa(o.Width))
}

// SelectPane focuses a pane (within its window).
func SelectPane(r Runner, pane string) error {
	_, err := r.Run("select-pane", "-t", pane)
	return err
}

// SendEnter presses Enter in a pane: accepts the highlighted option of a
// Claude Code permission prompt.
func SendEnter(r Runner, pane string) error {
	_, err := r.Run("send-keys", "-t", pane, "Enter")
	return err
}
