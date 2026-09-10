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
	pub := &daemon.Publisher{}
	err := daemon.Serve(context.Background(), daemon.SocketPath(),
		func() daemon.Snapshot {
			s := daemon.Build(r)
			pub.Publish(r, s)
			return s
		}, daemon.Interval, daemon.IdleExit)
	if err != nil {
		fmt.Fprintln(os.Stderr, "agentbar daemon:", err)
		os.Exit(1)
	}
}
