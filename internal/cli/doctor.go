package cli

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"

	"github.com/ssnxd/workflow/internal/config"
	"github.com/ssnxd/workflow/internal/tmux"
)

var tmuxVerRe = regexp.MustCompile(`tmux (\d+)\.(\d+)`)

// CheckTmuxVersion parses `tmux -V` output and errors below 3.2 (needed for
// new-session -e and modern -P -F behavior).
func CheckTmuxVersion(v string) error {
	m := tmuxVerRe.FindStringSubmatch(v)
	if m == nil {
		return fmt.Errorf("cannot parse tmux version from %q", v)
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	if major < 3 || (major == 3 && minor < 2) {
		return fmt.Errorf("tmux %d.%d is too old; workflow needs >= 3.2", major, minor)
	}
	return nil
}

// Doctor checks the environment and prints a report.
func Doctor() {
	ok := true
	check := func(name string, err error) {
		if err != nil {
			ok = false
			fmt.Printf("  ✗ %s: %v\n", name, err)
		} else {
			fmt.Printf("  ✓ %s\n", name)
		}
	}

	ver, err := tmux.ServerVersion()
	if err != nil {
		check("tmux installed", err)
	} else {
		check(fmt.Sprintf("tmux installed (%s)", ver), CheckTmuxVersion(ver))
	}

	_, err = exec.LookPath("claude")
	check("claude in PATH", err)

	_, err = exec.LookPath("git")
	check("git in PATH", err)

	p, _, err := config.Load()
	check("data dir "+p.DataDir, err)

	if tmux.InsideTmux() {
		fmt.Println("  · running inside tmux: attach will nest onto the dedicated workflow server (this is fine)")
	}
	if !ok {
		os.Exit(1)
	}
}
