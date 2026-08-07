package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Integration tests against a real, throwaway tmux server on a test socket.
func testClient(t *testing.T) *Client {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	conf := filepath.Join(t.TempDir(), "tmux.conf")
	if err := os.WriteFile(conf, []byte("set -g default-size 200x50\nset -g remain-on-exit on\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := New(fmt.Sprintf("workflow-test-%d", os.Getpid()), conf)
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-L", c.Socket, "kill-server").Run()
	})
	return c
}

func TestSessionWindowLifecycle(t *testing.T) {
	c := testClient(t)

	sessID, winID, paneID, err := c.NewSession("test.task: one", t.TempDir(),
		[]string{"FOO=bar"}, []string{"sleep", "60"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sessID, "$") || !strings.HasPrefix(winID, "@") || !strings.HasPrefix(paneID, "%") {
		t.Fatalf("unexpected ids: %s %s %s", sessID, winID, paneID)
	}

	win2, pane2, err := c.NewWindow(sessID, "worker.1", t.TempDir(), nil, []string{"sleep", "60"})
	if err != nil {
		t.Fatal(err)
	}
	if win2 == winID || pane2 == paneID {
		t.Fatal("window ids not unique")
	}

	panes, err := c.ListPanes()
	if err != nil {
		t.Fatal(err)
	}
	if len(panes) != 2 {
		t.Fatalf("ListPanes = %d panes, want 2", len(panes))
	}
	for _, p := range panes {
		if p.PanesInWindow != 1 {
			t.Errorf("window has %d panes, want 1", p.PanesInWindow)
		}
	}

	if err := c.KillWindow(win2); err != nil {
		t.Fatal(err)
	}
	if err := c.KillSession(sessID); err != nil {
		t.Fatal(err)
	}
}

func TestSendTextAndCapture(t *testing.T) {
	c := testClient(t)
	_, _, paneID, err := c.NewSession("send-test", t.TempDir(), nil, []string{"cat"})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond) // let cat start
	if err := c.SendText(paneID, "hello workflow"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		out, err := c.CapturePane(paneID)
		if err != nil {
			t.Fatal(err)
		}
		// cat echoes the line back, so it appears twice.
		if strings.Count(out, "hello workflow") >= 2 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	out, _ := c.CapturePane(paneID)
	t.Fatalf("sent text never echoed; pane content:\n%s", out)
}

func TestDeadPaneDetection(t *testing.T) {
	c := testClient(t)
	_, _, paneID, err := c.NewSession("dead-test", t.TempDir(), nil, []string{"sh", "-c", "exit 3"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		panes, err := c.ListPanes()
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range panes {
			if p.PaneID == paneID && p.Dead {
				if p.DeadStatus != "3" {
					t.Errorf("dead status = %q, want 3", p.DeadStatus)
				}
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("pane death never observed (remain-on-exit)")
}

func TestNoServerIsNotAnError(t *testing.T) {
	c := testClient(t)
	_, err := c.ListPanes()
	if _, ok := err.(ErrNoServer); !ok {
		t.Fatalf("ListPanes with no server = %v, want ErrNoServer", err)
	}
}

func TestMultilinePaste(t *testing.T) {
	c := testClient(t)
	_, _, paneID, err := c.NewSession("paste-test", t.TempDir(), nil, []string{"cat"})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if err := c.SendText(paneID, "line one\nline two"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		out, _ := c.CapturePane(paneID)
		if strings.Contains(out, "line two") {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("multiline paste never arrived")
}
