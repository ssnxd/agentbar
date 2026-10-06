package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/ssnxd/agentbar/internal/session"
	"github.com/ssnxd/agentbar/internal/state"
	"github.com/ssnxd/agentbar/internal/tmuxctl"
)

type heard struct {
	sounds  []bool   // request?
	desktop []string // "pane|title|body"
}

func notifier(h *heard) *Notifier {
	return &Notifier{
		Sound: func(request bool) { h.sounds = append(h.sounds, request) },
		Desktop: func(_ tmuxctl.Runner, pane, title, body string) {
			h.desktop = append(h.desktop, pane+"|"+title+"|"+body)
		},
	}
}

func TestNotifier(t *testing.T) {
	t0 := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	on := &fakeTmux{opts: map[string]string{NotifyOption: "on"}}
	sess := func(id, status string) session.Session {
		return session.Session{ID: id, Status: status, Project: "proj-" + id, Title: "title " + id,
			Detail: "Bash rm " + id, InTmux: true, TmuxPaneID: "%" + id, TmuxTarget: "w:1." + id}
	}
	snap := func(sec int, ss ...session.Session) Snapshot {
		return Snapshot{At: t0.Add(time.Duration(sec) * time.Second), Sessions: ss}
	}
	away := View{Visible: map[string]bool{}, Front: []string{"%9"}}
	inTmux := View{Focused: true, Visible: map[string]bool{"%9": true}, Front: []string{"%9"}}

	// what is already so when the daemon starts is not news
	h := &heard{}
	n := notifier(h)
	n.Observe(on, snap(0, sess("1", state.StatusNeedsYou), sess("2", state.StatusWorking)), away)
	n.Observe(on, snap(1, sess("1", state.StatusNeedsYou), sess("2", state.StatusWorking)), away)
	if len(h.sounds)+len(h.desktop) != 0 {
		t.Fatalf("state at start must stay quiet: %+v", h)
	}

	// a turn ends while you are in another app: nothing at once, then one
	// sound and one notification on the terminal's front pane
	n.Observe(on, snap(2, sess("1", state.StatusNeedsYou), sess("2", state.StatusDone)), away)
	if len(h.sounds)+len(h.desktop) != 0 {
		t.Fatalf("a change waits for a tick before it interrupts: %+v", h)
	}
	n.Observe(on, snap(3, sess("1", state.StatusNeedsYou), sess("2", state.StatusDone)), away)
	if len(h.sounds) != 1 || h.sounds[0] || len(h.desktop) != 1 || h.desktop[0] != "%9|proj-2 done|title 2" {
		t.Fatalf("done while away: %+v", h)
	}
	n.Observe(on, snap(4, sess("1", state.StatusNeedsYou), sess("2", state.StatusDone)), away)
	if len(h.sounds) != 1 {
		t.Fatalf("it says it once: %+v", h)
	}

	// in tmux, elsewhere: the sound, no desktop notification
	h = &heard{}
	n = notifier(h)
	n.Observe(on, snap(0, sess("1", state.StatusWorking)), inTmux)
	n.Observe(on, snap(1, sess("1", state.StatusNeedsYou)), inTmux)
	n.Observe(on, snap(2, sess("1", state.StatusNeedsYou)), inTmux)
	if len(h.sounds) != 1 || !h.sounds[0] || len(h.desktop) != 0 {
		t.Fatalf("a request while in tmux is the request sound alone: %+v", h)
	}

	// its pane is on your screen: nothing
	h = &heard{}
	n = notifier(h)
	seeing := View{Focused: true, Visible: map[string]bool{"%1": true}, Front: []string{"%1"}}
	n.Observe(on, snap(0, sess("1", state.StatusWorking)), seeing)
	n.Observe(on, snap(1, sess("1", state.StatusNeedsYou)), seeing)
	n.Observe(on, snap(2, sess("1", state.StatusNeedsYou)), seeing)
	n.Observe(on, snap(3, sess("1", state.StatusNeedsYou)), away)
	if len(h.sounds)+len(h.desktop) != 0 {
		t.Fatalf("a request you are looking at says nothing, also later: %+v", h)
	}

	// a status that does not last a tick never interrupts
	h = &heard{}
	n = notifier(h)
	n.Observe(on, snap(0, sess("1", state.StatusWorking)), away)
	n.Observe(on, snap(1, sess("1", state.StatusNeedsYou)), away)
	n.Observe(on, snap(2, sess("1", state.StatusWorking)), away)
	n.Observe(on, snap(3, sess("1", state.StatusWorking)), away)
	if len(h.sounds)+len(h.desktop) != 0 {
		t.Fatalf("answered at once: %+v", h)
	}

	// several in the same tick are one notification, the request leading
	h = &heard{}
	n = notifier(h)
	n.Observe(on, snap(0, sess("1", state.StatusWorking), sess("2", state.StatusWorking), sess("3", state.StatusWorking)), away)
	n.Observe(on, snap(1, sess("1", state.StatusDone), sess("2", state.StatusNeedsYou), sess("3", state.StatusDone)), away)
	n.Observe(on, snap(2, sess("1", state.StatusDone), sess("2", state.StatusNeedsYou), sess("3", state.StatusDone)), away)
	if len(h.sounds) != 1 || !h.sounds[0] || len(h.desktop) != 1 || h.desktop[0] != "%9|1 needs you, 2 done|proj-1, proj-2, proj-3" {
		t.Fatalf("a batch: %+v", h)
	}

	// off unless asked for; and the sound can go alone
	for _, opts := range []map[string]string{nil, {NotifyOption: "off"}} {
		h = &heard{}
		n = notifier(h)
		f := &fakeTmux{opts: opts}
		n.Observe(f, snap(0, sess("1", state.StatusWorking)), away)
		n.Observe(f, snap(1, sess("1", state.StatusDone)), away)
		n.Observe(f, snap(2, sess("1", state.StatusDone)), away)
		if len(h.sounds)+len(h.desktop) != 0 {
			t.Fatalf("notifications are opt-in (%v): %+v", opts, h)
		}
	}
	h = &heard{}
	n = notifier(h)
	mute := &fakeTmux{opts: map[string]string{NotifyOption: "on", SoundOption: "off"}}
	n.Observe(mute, snap(0, sess("1", state.StatusWorking)), away)
	n.Observe(mute, snap(1, sess("1", state.StatusDone)), away)
	n.Observe(mute, snap(2, sess("1", state.StatusDone)), away)
	if len(h.sounds) != 0 || len(h.desktop) != 1 {
		t.Fatalf("sound off keeps the notification: %+v", h)
	}
}

func TestMessage(t *testing.T) {
	hot := session.Session{Status: state.StatusNeedsYou, Project: "api", Title: "Add rate limits", Detail: "Bash pnpm prisma migrate deploy"}
	title, body, req := Message([]session.Session{hot})
	if title != "api needs you" || body != "Bash pnpm prisma migrate deploy" || !req {
		t.Errorf("one request: %q / %q / %v", title, body, req)
	}
	hot.Detail = ""
	if _, body, _ := Message([]session.Session{hot}); body != "Add rate limits" {
		t.Errorf("a request with no detail falls back to the title: %q", body)
	}
	done := session.Session{Status: state.StatusDone, Project: "api", Title: "Add rate limits"}
	title, body, req = Message([]session.Session{done, done})
	if title != "2 done" || body != "api" || req {
		t.Errorf("two done in one project: %q / %q / %v", title, body, req)
	}
}

func TestOSC777IsSafe(t *testing.T) {
	// tool input is external text: it must not be able to end the sequence
	// or start one of its own
	b := string(osc777("a;b\x1b]0;x\a", "line one\nline\ttwo \x1b[31m \u009b \x07"))
	if !strings.HasPrefix(b, "\x1bPtmux;\x1b\x1b]777;notify;") || !strings.HasSuffix(b, "\a\x1b\\") {
		t.Fatalf("not wrapped for tmux passthrough: %q", b)
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(b, "\x1bPtmux;\x1b\x1b]777;notify;"), "\a\x1b\\")
	if strings.ContainsAny(inner, "\x1b\a\n\t\u009b") {
		t.Errorf("control characters left in %q", inner)
	}
	if inner != "a,b]0,x;line one line two [31m" {
		t.Errorf("payload = %q", inner)
	}
	if got := clean(strings.Repeat("é", 100), 10, false); got != strings.Repeat("é", 9)+"…" {
		t.Errorf("truncates by rune: %q", got)
	}
}
