// Package daemon builds the sidebar snapshot once, centrally, and streams
// it to every viewer over a Unix socket. One daemon, N thin viewers: the
// per-window sidebar panes never poll tmux or read transcripts themselves.
package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/ssnxd/agentbar/internal/paths"
	"github.com/ssnxd/agentbar/internal/registry"
	"github.com/ssnxd/agentbar/internal/session"
	"github.com/ssnxd/agentbar/internal/state"
	"github.com/ssnxd/agentbar/internal/tmuxctl"
	"github.com/ssnxd/agentbar/internal/transcript"
)

// Snapshot is one consistent view of the world, sent as a JSON line.
type Snapshot struct {
	At       time.Time               `json:"at"`
	Sessions []session.Session       `json:"sessions"`
	Panes    map[string]tmuxctl.Pane `json:"panes"`
	Err      string                  `json:"err,omitempty"`
}

const (
	Interval = time.Second
	IdleExit = 60 * time.Second
	sweepAge = time.Hour
)

func SocketPath() string { return filepath.Join(paths.DataDir(), "daemon.sock") }
func PIDPath() string    { return filepath.Join(paths.DataDir(), "daemon.pid") }

// Build assembles a snapshot from every source. It is what the old UI
// refresh did, moved here so it runs once per tick for all viewers.
func Build(r tmuxctl.Runner) Snapshot {
	now := time.Now()
	reg := registry.Load(paths.SessionsDir(), registry.Alive)
	live := map[string]bool{}
	var interactive []registry.Entry
	for _, e := range reg {
		live[e.SessionID] = true
		if e.Kind == "" || e.Kind == "interactive" {
			interactive = append(interactive, e)
		}
	}
	states := state.ReadAll(paths.StateDir())
	agents := state.ReadAgents(paths.StateDir())
	state.Sweep(paths.StateDir(), live, sweepAge, now)
	state.SweepAgents(paths.StateDir(), live, sweepAge, now)
	state.SweepSeen(paths.StateDir(), live)

	panes := map[string]tmuxctl.Pane{}
	ps, perr := tmuxctl.ListPanes(r)
	for _, p := range ps {
		panes[p.PaneID] = p
	}
	ss := session.Build(session.Deps{
		Registry: interactive,
		States:   states,
		Agents:   agents,
		Transcript: func(cwd, id string) transcript.Info {
			p := transcript.Newest(paths.ProjectsDir(), cwd, id)
			if p == "" {
				return transcript.Info{}
			}
			return transcript.Tail(p)
		},
		AgentMeta: func(cwd, id, agentID string) transcript.Meta {
			return transcript.AgentMeta(transcript.Newest(paths.ProjectsDir(), cwd, id), agentID)
		},
		Now: now,
	})
	for i := range ss {
		if p, ok := panes[ss[i].TmuxPaneID]; ok {
			ss[i].TmuxTarget = tmuxctl.TargetLabel(p)
		} else {
			ss[i].InTmux = false
		}
	}
	snap := Snapshot{At: now, Sessions: ss, Panes: panes}
	if perr != nil {
		snap.Err = perr.Error()
	}
	return snap
}

// Running probes the socket.
func Running(sock string) bool {
	c, err := net.DialTimeout("unix", sock, 200*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// StartDetached launches `<exe> daemon` as its own session so it outlives
// the caller. Output goes to the agentbar log.
func StartDetached(exe string) error {
	_ = os.MkdirAll(paths.DataDir(), 0o755)
	logf, err := os.OpenFile(paths.LogPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer logf.Close()
	cmd := exec.Command(exe, "daemon")
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}

// EnsureRunning starts the daemon if the socket is dead and waits briefly
// for it to come up.
func EnsureRunning(exe string) error {
	sock := SocketPath()
	if Running(sock) {
		return nil
	}
	if err := StartDetached(exe); err != nil {
		return err
	}
	for i := 0; i < 20; i++ {
		time.Sleep(50 * time.Millisecond)
		if Running(sock) {
			return nil
		}
	}
	return errors.New("daemon did not start; see " + paths.LogPath())
}

// Nudge asks a running daemon for an immediate rebuild.
func Nudge() error {
	b, err := os.ReadFile(PIDPath())
	if err != nil {
		return err
	}
	pid, err := strconv.Atoi(string(b))
	if err != nil {
		return err
	}
	return syscall.Kill(pid, syscall.SIGUSR1)
}

// Stop terminates a running daemon.
func Stop() error {
	b, err := os.ReadFile(PIDPath())
	if err != nil {
		return nil
	}
	pid, err := strconv.Atoi(string(b))
	if err != nil {
		return nil
	}
	return syscall.Kill(pid, syscall.SIGTERM)
}

func dial(sock string, wait time.Duration) (net.Conn, error) {
	return net.DialTimeout("unix", sock, wait)
}

func decodeLine(c net.Conn, s *Snapshot) error {
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	if !sc.Scan() {
		if err := sc.Err(); err != nil {
			return err
		}
		return errors.New("daemon closed the connection")
	}
	return json.Unmarshal(sc.Bytes(), s)
}

// Serve runs the daemon loop until ctx ends or it has had no viewers for
// idle. build is called every interval and on SIGUSR1. keep, when set, is
// asked before an idle exit; true keeps the daemon for another idle period.
func Serve(ctx context.Context, sock string, build func() Snapshot, interval, idle time.Duration, keep func() bool) error {
	if Running(sock) {
		return errors.New("daemon already running")
	}
	_ = os.MkdirAll(filepath.Dir(sock), 0o755)
	_ = os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return err
	}
	defer ln.Close()
	defer os.Remove(sock)
	_ = os.WriteFile(PIDPath(), []byte(strconv.Itoa(os.Getpid())), 0o644)
	defer os.Remove(PIDPath())

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var mu sync.Mutex
	clients := map[net.Conn]bool{}
	var last []byte

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			clients[c] = true
			if last != nil {
				_, _ = c.Write(last)
			}
			mu.Unlock()
		}
	}()

	nudge := make(chan os.Signal, 1)
	signal.Notify(nudge, syscall.SIGUSR1)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(nudge)
	defer signal.Stop(stop)

	tick := time.NewTicker(interval)
	defer tick.Stop()
	idleSince := time.Now()

	publish := func() {
		b, err := json.Marshal(build())
		if err != nil {
			return
		}
		b = append(b, '\n')
		mu.Lock()
		last = b
		for c := range clients {
			c.SetWriteDeadline(time.Now().Add(time.Second))
			if _, err := c.Write(b); err != nil {
				c.Close()
				delete(clients, c)
			}
		}
		n := len(clients)
		mu.Unlock()
		if n > 0 {
			idleSince = time.Now()
		} else if idle > 0 && time.Since(idleSince) > idle {
			if keep != nil && keep() {
				idleSince = time.Now()
			} else {
				cancel()
			}
		}
	}

	publish()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-stop:
			return nil
		case <-nudge:
			publish()
		case <-tick.C:
			publish()
		}
	}
}

// Connect streams snapshots from the daemon into out until ctx ends,
// reconnecting with backoff. start is called when the socket is dead
// (nil to disable auto-start).
func Connect(ctx context.Context, sock string, out chan<- Snapshot, start func() error) {
	backoff := 200 * time.Millisecond
	for {
		if ctx.Err() != nil {
			return
		}
		c, err := net.DialTimeout("unix", sock, 500*time.Millisecond)
		if err != nil {
			if start != nil {
				_ = start()
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < 3*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = 200 * time.Millisecond
		go func() {
			<-ctx.Done()
			c.Close()
		}()
		sc := bufio.NewScanner(c)
		sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
		for sc.Scan() {
			var s Snapshot
			if err := json.Unmarshal(sc.Bytes(), &s); err != nil {
				continue
			}
			select {
			case out <- s:
			case <-ctx.Done():
				c.Close()
				return
			}
		}
		c.Close()
	}
}
