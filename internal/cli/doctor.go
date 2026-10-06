package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ssnxd/agentbar/internal/daemon"
	"github.com/ssnxd/agentbar/internal/hooks"
	"github.com/ssnxd/agentbar/internal/paths"
	"github.com/ssnxd/agentbar/internal/registry"
	"github.com/ssnxd/agentbar/internal/tmuxctl"
)

var tmuxVerRe = regexp.MustCompile(`(\d+)\.(\d+)`)

// TmuxVersionOK reports whether a `tmux -V` string is 3.2 or newer.
func TmuxVersionOK(v string) bool {
	m := tmuxVerRe.FindStringSubmatch(v)
	if m == nil {
		return false
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	return major > 3 || (major == 3 && minor >= 2)
}

// Doctor checks the environment and exits 1 if anything is wrong.
func Doctor() {
	failed := false
	check := func(ok bool, what, hint string) {
		mark := "ok  "
		if !ok {
			mark = "FAIL"
			failed = true
		}
		fmt.Printf("%s %s", mark, what)
		if !ok && hint != "" {
			fmt.Printf("\n     %s", hint)
		}
		fmt.Println()
	}

	if out, err := exec.Command("tmux", "-V").Output(); err != nil {
		check(false, "tmux", "install tmux >= 3.2")
	} else {
		v := strings.TrimSpace(string(out))
		check(TmuxVersionOK(v), v, "agentbar needs tmux >= 3.2 (pane options, join-pane -l)")
	}

	exe := exePath()
	st, err := os.Stat(exe)
	check(err == nil && filepath.IsAbs(exe) && st.Mode()&0o111 != 0, "binary "+exe, "")

	settings, err := os.ReadFile(paths.SettingsPath())
	switch installed := hooks.InstalledExe(settings); {
	case err != nil:
		check(false, "hooks in "+paths.SettingsPath(), "run: agentbar install")
	case installed == "":
		check(false, "hooks not installed", "run: agentbar install")
	case installed != exe:
		check(false, "hooks point at "+installed, "run: agentbar install  (migrates to "+exe+")")
	default:
		missing := hooks.Missing(settings, exe)
		check(len(missing) == 0, "hooks installed", "missing "+strings.Join(missing, ", ")+"; run: agentbar install")
	}

	check(os.MkdirAll(paths.StateDir(), 0o755) == nil, "state dir "+paths.StateDir(), "")

	if _, err := os.Stat(paths.SessionsDir()); err != nil {
		check(false, "claude registry "+paths.SessionsDir(), "start a claude session; Claude Code >= 2.1.26x writes this")
	} else {
		n := len(registry.Load(paths.SessionsDir(), registry.Alive))
		check(true, fmt.Sprintf("claude registry: %d live session(s)", n), "")
	}

	if os.Getenv("TMUX") != "" {
		fmt.Println("ok   inside tmux")
		r := tmuxctl.Exec{}
		if daemon.StatusReads(r) {
			check(daemon.Running(daemon.SocketPath()), "status line reads agentbar; daemon running", "run: agentbar tmux-init")
		} else {
			fmt.Println("note status line has no indicator; add #{E:@agentbar_pill} to status-right")
		}
		// a desktop notification travels through tmux to the terminal
		if v, _ := r.Run("show-options", "-gqv", daemon.NotifyOption); v == "on" || v == "1" {
			pt, _ := r.Run("show-options", "-gqv", "allow-passthrough")
			check(pt == "on" || pt == "all", "notifications on; allow-passthrough "+pt, "add to tmux.conf: set -g allow-passthrough on")
		}
	} else {
		fmt.Println("note not inside tmux (fine for install/doctor)")
	}

	if failed {
		os.Exit(1)
	}
}
