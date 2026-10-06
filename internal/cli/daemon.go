package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/ssnxd/agentbar/internal/daemon"
	"github.com/ssnxd/agentbar/internal/tmuxctl"
)

// Daemon runs the snapshot daemon in the foreground (normally started
// detached by openAll or by a viewer that finds no daemon).
func Daemon() {
	r := tmuxctl.Exec{}
	pub, seen, notify := &daemon.Publisher{}, &daemon.Seen{}, &daemon.Notifier{}
	err := daemon.Serve(context.Background(), daemon.SocketPath(),
		func() daemon.Snapshot {
			s := daemon.Build(r)
			v := daemon.Look(r, s.Panes)
			// first what you have seen, so that the status line and the
			// notifications speak of the same state
			seen.Apply(&s, v)
			pub.Publish(r, s)
			notify.Observe(r, s, v)
			return s
		}, daemon.Interval, daemon.IdleExit,
		// the status line shows what the daemon publishes: stay while it does
		func() bool { return daemon.StatusReads(r) })
	pub.Clear(r)
	if err != nil {
		fmt.Fprintln(os.Stderr, "agentbar daemon:", err)
		os.Exit(1)
	}
}
