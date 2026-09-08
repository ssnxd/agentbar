package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/ssnxd/agentbar/internal/hooks"
	"github.com/ssnxd/agentbar/internal/paths"
)

// Install merges agentbar's hooks into ~/.claude/settings.json (or removes
// them with --uninstall). The file is backed up first. Hooks pointing at an
// older binary location are migrated to this one.
func Install(args []string) {
	uninstall := slices.Contains(args, "--uninstall")
	exe := exePath()
	p := paths.SettingsPath()
	cur, err := os.ReadFile(p)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		fatal(err)
	}

	var out []byte
	changed := false
	old := hooks.InstalledExe(cur)
	switch {
	case uninstall && old == "":
		out = cur
	case uninstall:
		out, changed, err = hooks.Remove(cur, old)
	default:
		work := cur
		if old != "" && old != exe {
			work, _, err = hooks.Remove(cur, old)
			if err != nil {
				fatal(err)
			}
			fmt.Printf("migrating hooks from %s\n", old)
			changed = true
		}
		var merged bool
		out, merged, err = hooks.Merge(work, exe)
		changed = changed || merged
	}
	if err != nil {
		fatal(err)
	}

	if changed {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			fatal(err)
		}
		if len(cur) > 0 {
			backup := p + ".bak." + time.Now().Format("20060102-150405")
			if err := os.WriteFile(backup, cur, 0o600); err != nil {
				fatal(err)
			}
			fmt.Println("backup:", backup)
		}
		if err := os.WriteFile(p, out, 0o600); err != nil {
			fatal(err)
		}
		fmt.Println("updated:", p)
	} else {
		fmt.Println("no change:", p)
	}
	if uninstall {
		return
	}
	fmt.Printf(`
hooks call: %s event
state dir:  %s

Add to your tmux config, then reload it (prefix r, or tmux source-file):
  run-shell /path/to/agentbar/agentbar.tmux
or bind the key directly:
  bind a run-shell "%s toggle #{pane_id}"
`, exe, paths.StateDir(), exe)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "agentbar:", err)
	os.Exit(1)
}
