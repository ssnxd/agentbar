package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ssnxd/agentbar/internal/session"
	"github.com/ssnxd/agentbar/internal/state"
)

type fakeTmux struct {
	calls []string
	opts  map[string]string
	tty   string
}

func (f *fakeTmux) Run(args ...string) (string, error) {
	f.calls = append(f.calls, strings.Join(args, " "))
	switch args[0] {
	case "show-options":
		return f.opts[args[len(args)-1]], nil
	case "display-message":
		return f.tty, nil
	}
	return "", nil
}

func (f *fakeTmux) joined() string { return strings.Join(f.calls, "\n") }

func snapOf(ss ...session.Session) Snapshot { return Snapshot{Sessions: ss} }

func TestPublishSetsStatusOptionsOnlyWhenChanged(t *testing.T) {
	f := &fakeTmux{}
	p := &Publisher{}
	p.Publish(f, snapOf(
		session.Session{ID: "a", Status: state.StatusNeedsYou},
		session.Session{ID: "b", Status: state.StatusWorking},
		session.Session{ID: "c", Status: state.StatusNeedsYou},
		session.Session{ID: "d", Status: state.StatusWaiting},
		session.Session{ID: "e", Status: state.StatusWaiting},
	))
	j := f.joined()
	for _, want := range []string{"set-option -g @agentbar_hot 2", "set-option -g @agentbar_status 5 · 2 need you"} {
		if !strings.Contains(j, want) {
			t.Errorf("missing %q in\n%s", want, j)
		}
	}
	n := len(f.calls)
	p.Publish(f, snapOf(
		session.Session{ID: "a", Status: state.StatusNeedsYou},
		session.Session{ID: "b", Status: state.StatusWorking},
		session.Session{ID: "c", Status: state.StatusNeedsYou},
		session.Session{ID: "d", Status: state.StatusWaiting},
		session.Session{ID: "e", Status: state.StatusWaiting},
	))
	if len(f.calls) != n {
		t.Errorf("unchanged summary must not touch tmux again:\n%s", f.joined())
	}
	p.Publish(f, snapOf(session.Session{ID: "b", Status: state.StatusWorking}))
	if !strings.Contains(f.joined(), "set-option -g @agentbar_hot 0") || !strings.Contains(f.joined(), "set-option -g @agentbar_status 1 session") {
		t.Errorf("after change:\n%s", f.joined())
	}
	p.Publish(f, snapOf())
	if !strings.HasSuffix(f.joined(), "set-option -g @agentbar_status ") {
		t.Errorf("no sessions clears the status text:\n%s", f.joined())
	}
}

func TestPublishBellOnNeedsYouWhenEnabled(t *testing.T) {
	tty := filepath.Join(t.TempDir(), "tty")
	_ = os.WriteFile(tty, nil, 0o644)
	f := &fakeTmux{opts: map[string]string{"@agentbar-bell": "on"}, tty: tty}
	p := &Publisher{}
	p.Publish(f, snapOf(session.Session{ID: "a", Status: state.StatusWorking, InTmux: true, TmuxPaneID: "%1"}))
	if b, _ := os.ReadFile(tty); len(b) != 0 {
		t.Fatal("no bell while working")
	}
	p.Publish(f, snapOf(session.Session{ID: "a", Status: state.StatusNeedsYou, InTmux: true, TmuxPaneID: "%1"}))
	if b, _ := os.ReadFile(tty); string(b) != "\a" {
		t.Errorf("expected one BEL on the pane tty, got %q", b)
	}
	if !strings.Contains(f.joined(), "display-message -p -t %1 #{pane_tty}") {
		t.Errorf("tty lookup missing:\n%s", f.joined())
	}
	// still needs-you on the next tick: no second bell
	p.Publish(f, snapOf(session.Session{ID: "a", Status: state.StatusNeedsYou, InTmux: true, TmuxPaneID: "%1"}))
	if b, _ := os.ReadFile(tty); string(b) != "\a" {
		t.Errorf("bell must ring once per transition, got %q", b)
	}
}

func TestPublishNoBellByDefault(t *testing.T) {
	tty := filepath.Join(t.TempDir(), "tty")
	_ = os.WriteFile(tty, nil, 0o644)
	f := &fakeTmux{tty: tty}
	p := &Publisher{}
	p.Publish(f, snapOf(session.Session{ID: "a", Status: state.StatusWorking, InTmux: true, TmuxPaneID: "%1"}))
	p.Publish(f, snapOf(session.Session{ID: "a", Status: state.StatusNeedsYou, InTmux: true, TmuxPaneID: "%1"}))
	if b, _ := os.ReadFile(tty); len(b) != 0 {
		t.Errorf("bell is opt-in, got %q", b)
	}
	if strings.Contains(f.joined(), "display-message") {
		t.Errorf("no tty lookup when the bell is off:\n%s", f.joined())
	}
}
