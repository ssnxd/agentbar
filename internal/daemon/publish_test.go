package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ssnxd/agentbar/internal/session"
	"github.com/ssnxd/agentbar/internal/state"
	"github.com/ssnxd/agentbar/internal/tmuxctl"
)

type fakeTmux struct {
	calls   []string
	opts    map[string]string
	tty     string
	clients string
}

func (f *fakeTmux) Run(args ...string) (string, error) {
	f.calls = append(f.calls, strings.Join(args, " "))
	switch args[0] {
	case "show-options":
		return f.opts[args[len(args)-1]], nil
	case "display-message":
		return f.tty, nil
	case "list-clients":
		return f.clients, nil
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
	if !strings.Contains(f.joined(), "set-option -g @agentbar_status \nset-option -g @agentbar_pill \n") {
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

func TestPublishStatusOneRequest(t *testing.T) {
	f := &fakeTmux{}
	(&Publisher{}).Publish(f, snapOf(
		session.Session{ID: "a", Status: state.StatusNeedsYou},
		session.Session{ID: "b", Status: state.StatusWorking},
	))
	if want := "set-option -g @agentbar_status 2 · 1 needs you"; !strings.Contains(f.joined(), want) {
		t.Errorf("missing %q in\n%s", want, f.joined())
	}
}

func TestPublishMarksWindowsAndSessionsThatNeedYou(t *testing.T) {
	panes := map[string]tmuxctl.Pane{
		"%1": {SessionName: "work", SessionID: "$1", WindowID: "@1", PaneID: "%1"},
		"%2": {SessionName: "work", SessionID: "$1", WindowID: "@2", PaneID: "%2"},
		"%3": {SessionName: "side", SessionID: "$2", WindowID: "@3", PaneID: "%3"},
	}
	snap := func(a, b, c string) Snapshot {
		return Snapshot{Panes: panes, Sessions: []session.Session{
			{ID: "a", Status: a, InTmux: true, TmuxPaneID: "%1"},
			{ID: "b", Status: b, InTmux: true, TmuxPaneID: "%2"},
			{ID: "c", Status: c, InTmux: true, TmuxPaneID: "%3"},
		}}
	}
	f := &fakeTmux{}
	p := &Publisher{}
	// the first pass also cleans what a dead daemon left: every window and
	// session that does not need you is unmarked
	p.Publish(f, snap(state.StatusWorking, state.StatusNeedsYou, state.StatusDone))
	j := f.joined()
	for _, want := range []string{
		"set-option -w -t @2 @agentbar_win 1", "set-option -t $1 @agentbar_sess 1",
		"set-option -wu -t @1 @agentbar_win", "set-option -wu -t @3 @agentbar_win", "set-option -u -t $2 @agentbar_sess",
	} {
		if !strings.Contains(j, want) {
			t.Errorf("missing %q in\n%s", want, j)
		}
	}
	if strings.Contains(j, "@agentbar_win 1\nset-option -w -t @3") || strings.Contains(j, "-t @3 @agentbar_win 1") {
		t.Errorf("done is not a mark on a window:\n%s", j)
	}
	// nothing changed: tmux is left alone
	n := len(f.calls)
	p.Publish(f, snap(state.StatusWorking, state.StatusNeedsYou, state.StatusDone))
	if len(f.calls) != n {
		t.Errorf("unchanged marks must not touch tmux again:\n%s", strings.Join(f.calls[n:], "\n"))
	}
	// answered there, asked elsewhere
	f.calls = nil
	p.Publish(f, snap(state.StatusWorking, state.StatusWorking, state.StatusNeedsYou))
	j = f.joined()
	for _, want := range []string{
		"set-option -wu -t @2 @agentbar_win", "set-option -u -t $1 @agentbar_sess",
		"set-option -w -t @3 @agentbar_win 1", "set-option -t $2 @agentbar_sess 1",
	} {
		if !strings.Contains(j, want) {
			t.Errorf("missing %q in\n%s", want, j)
		}
	}
	// a daemon that stops takes its marks with it
	f.calls = nil
	p.Clear(f)
	if j = f.joined(); !strings.Contains(j, "set-option -wu -t @3 @agentbar_win") || !strings.Contains(j, "set-option -u -t $2 @agentbar_sess") {
		t.Errorf("Clear should unmark:\n%s", j)
	}
}
