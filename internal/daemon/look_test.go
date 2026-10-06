package daemon

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ssnxd/agentbar/internal/session"
	"github.com/ssnxd/agentbar/internal/state"
	"github.com/ssnxd/agentbar/internal/tmuxctl"
)

func lookPanes() map[string]tmuxctl.Pane {
	return map[string]tmuxctl.Pane{
		// work: window @1 is current, two panes; window @2 is behind it
		"%1": {SessionName: "work", WindowID: "@1", PaneID: "%1", Active: true, WindowActive: true},
		"%2": {SessionName: "work", WindowID: "@1", PaneID: "%2", WindowActive: true},
		"%3": {SessionName: "work", WindowID: "@2", PaneID: "%3", Active: true},
		// side: attached on a second terminal that does not have the focus
		"%4": {SessionName: "side", WindowID: "@3", PaneID: "%4", Active: true, WindowActive: true},
		// away: nobody is attached
		"%5": {SessionName: "away", WindowID: "@4", PaneID: "%5", Active: true, WindowActive: true},
	}
}

func TestLook(t *testing.T) {
	f := &fakeTmux{clients: "attached,focused,UTF-8|work\nattached,UTF-8|side"}
	v := Look(f, lookPanes())
	if !v.Focused {
		t.Error("a focused client means you are in tmux")
	}
	if want := map[string]bool{"%1": true, "%2": true}; !reflect.DeepEqual(v.Visible, want) {
		t.Errorf("visible = %v, want the panes of the focused client's window %v", v.Visible, want)
	}
	if want := []string{"%1", "%4"}; !reflect.DeepEqual(v.Front, want) {
		t.Errorf("front = %v, want the active pane of each attached client %v", v.Front, want)
	}

	// zoomed: only the active pane is on screen
	ps := lookPanes()
	for _, id := range []string{"%1", "%2"} {
		p := ps[id]
		p.Zoomed = true
		ps[id] = p
	}
	if v := Look(f, ps); !v.Visible["%1"] || v.Visible["%2"] {
		t.Errorf("a zoomed window shows its active pane only: %v", v.Visible)
	}

	// you are in another app: nothing is seen, the front panes remain
	f.clients = "attached,UTF-8|work"
	v = Look(f, lookPanes())
	if v.Focused || len(v.Visible) != 0 || !reflect.DeepEqual(v.Front, []string{"%1"}) {
		t.Errorf("unfocused: %+v", v)
	}
}

func TestSeenTurnsDoneIntoWaiting(t *testing.T) {
	data := t.TempDir()
	t.Setenv("AGENTBAR_STATE_DIR", data)
	t0 := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	done := func(id, pane string, since time.Time) session.Session {
		return session.Session{ID: id, Status: state.StatusDone, StatusSince: since, InTmux: true, TmuxPaneID: pane}
	}
	status := func(s Snapshot, id string) string {
		for _, ss := range s.Sessions {
			if ss.ID == id {
				return ss.Status
			}
		}
		return ""
	}
	seen := &Seen{}
	at := func(d time.Duration, v View, ss ...session.Session) Snapshot {
		s := Snapshot{At: t0.Add(d), Sessions: ss}
		seen.Apply(&s, v)
		return s
	}
	nobody := View{Visible: map[string]bool{}}
	on := func(panes ...string) View {
		v := View{Focused: true, Visible: map[string]bool{}}
		for _, p := range panes {
			v.Visible[p] = true
		}
		return v
	}

	// it finished behind your back: done, for as long as you do not look
	a := done("a", "%1", t0)
	if s := at(0, nobody, a); status(s, "a") != state.StatusDone {
		t.Fatal("unseen stays done")
	}
	if s := at(time.Hour, nobody, a); status(s, "a") != state.StatusDone {
		t.Fatal("unseen stays done however long")
	}
	// passing through its window is not reading it
	if s := at(time.Hour+time.Second, on("%1"), a); status(s, "a") != state.StatusDone {
		t.Error("a glance shorter than the dwell must not mark it seen")
	}
	at(time.Hour+1500*time.Millisecond, nobody, a) // you left again
	if s := at(time.Hour+2*time.Second, on("%1"), a); status(s, "a") != state.StatusDone {
		t.Error("the dwell starts over when the pane left the screen")
	}
	// staying is
	if s := at(time.Hour+3*time.Second, on("%1"), a); status(s, "a") != state.StatusWaiting {
		t.Error("on screen for the dwell: seen, so waiting")
	}
	// and it stays seen once you look away
	if s := at(time.Hour+4*time.Second, nobody, a); status(s, "a") != state.StatusWaiting {
		t.Error("seen is remembered")
	}
	if _, ok := state.ReadSeen(filepath.Join(data, "state"))["a"]; !ok {
		t.Error("the mark is on disk, for the next daemon")
	}
	if s := (&Seen{}).applied(Snapshot{At: t0.Add(5 * time.Hour), Sessions: []session.Session{a}}, nobody); status(s, "a") != state.StatusWaiting {
		t.Error("a new daemon reads the marks of the last one")
	}
	// a later turn that ends unseen is done again
	a2 := done("a", "%1", t0.Add(6*time.Hour))
	if s := at(6*time.Hour, nobody, a2); status(s, "a") != state.StatusDone {
		t.Error("an old mark does not cover a newer turn")
	}

	// it finished in front of you: never done at all
	b := done("b", "%2", t0.Add(7*time.Hour))
	at(7*time.Hour-time.Minute, on("%2"))
	if s := at(7*time.Hour, on("%2"), b); status(s, "b") != state.StatusWaiting {
		t.Error("a turn that ends on screen is seen at once")
	}

	// outside tmux there is no screen to ask
	c := session.Session{ID: "c", Status: state.StatusDone, StatusSince: t0}
	if s := at(8*time.Hour, nobody, c); status(s, "c") != state.StatusWaiting {
		t.Error("a session outside tmux is not held as unseen")
	}
}

func (t *Seen) applied(s Snapshot, v View) Snapshot {
	t.Apply(&s, v)
	return s
}
