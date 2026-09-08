package daemon

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ssnxd/agentbar/internal/session"
)

func TestServeConnectRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGENTBAR_STATE_DIR", dir)
	sock := filepath.Join(dir, "d.sock")

	n := 0
	build := func() Snapshot {
		n++
		return Snapshot{At: time.Now(), Sessions: []session.Session{{ID: "s1", Title: "hello", Status: "waiting"}}}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, sock, build, 50*time.Millisecond, 0) }()

	for i := 0; i < 40 && !Running(sock); i++ {
		time.Sleep(25 * time.Millisecond)
	}
	if !Running(sock) {
		t.Fatal("daemon did not come up")
	}

	out := make(chan Snapshot, 4)
	cctx, ccancel := context.WithCancel(ctx)
	go Connect(cctx, sock, out, nil)

	select {
	case s := <-out:
		if len(s.Sessions) != 1 || s.Sessions[0].Title != "hello" {
			t.Fatalf("bad snapshot: %+v", s)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no snapshot received")
	}
	// a second one arrives on the next tick
	select {
	case <-out:
	case <-time.After(3 * time.Second):
		t.Fatal("no periodic snapshot")
	}
	ccancel()

	if err := Nudge(); err != nil {
		t.Errorf("nudge: %v", err)
	}
	if Serve(ctx, sock, build, time.Second, 0) == nil {
		t.Error("second Serve on a live socket must fail")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not stop on cancel")
	}
	if Running(sock) {
		t.Error("socket should be gone after stop")
	}
}

func TestServeIdleExit(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGENTBAR_STATE_DIR", dir)
	sock := filepath.Join(dir, "d.sock")
	done := make(chan error, 1)
	go func() {
		done <- Serve(context.Background(), sock, func() Snapshot { return Snapshot{} }, 30*time.Millisecond, 150*time.Millisecond)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("daemon should exit when idle")
	}
}

func TestConnectWithoutDaemonCallsStart(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "none.sock")
	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()
	calls := 0
	Connect(ctx, sock, make(chan Snapshot), func() error { calls++; return nil })
	if calls == 0 {
		t.Error("start should have been attempted")
	}
}
