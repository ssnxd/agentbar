package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/ssnxd/agentbar/internal/session"
	"github.com/ssnxd/agentbar/internal/state"
)

func TestPill(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	snap := func(ss ...session.Session) Snapshot { return Snapshot{At: now, Sessions: ss} }
	hot := func(ago time.Duration) session.Session {
		return session.Session{Status: state.StatusNeedsYou, LastActivity: now.Add(-ago)}
	}
	work := session.Session{Status: state.StatusWorking}
	wait := session.Session{Status: state.StatusWaiting}

	if p := Pill(snap()); p != "" {
		t.Errorf("no sessions, no pill: %q", p)
	}

	// one request: the long form carries the label and the age, the short
	// form only the count; tmux picks by client width
	p := Pill(snap(hot(12*time.Minute), work, wait))
	for _, want := range []string{
		"#[range=user|agentbar]", "#[norange default] ",
		"#{?#{e|>=:#{client_width},90},● 1 needs you 12m,● 1}",
		"bg=#fab387", "#{@agentbar-cap-left}", "#{@agentbar-cap-right}",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("missing %q in %q", want, p)
		}
	}
	if strings.Contains(p, "◐") || strings.Contains(p, "○") {
		t.Errorf("a request is the only thing the pill says: %q", p)
	}

	// several: plural, and the age of the oldest
	p = Pill(snap(hot(3*time.Minute), hot(2*time.Hour)))
	if !strings.Contains(p, "● 2 need you 2h,● 2}") {
		t.Errorf("plural with the oldest age: %q", p)
	}
	// a request under a minute old has no age yet
	p = Pill(snap(hot(10 * time.Second)))
	if !strings.Contains(p, "● 1 needs you,● 1}") {
		t.Errorf("fresh request: %q", p)
	}

	// nothing asked: soft pill, counts by state, zero counts left out
	p = Pill(snap(work, work, wait))
	if !strings.Contains(p, "◐ 2 #[fg=#9399b2]○ 1") || !strings.Contains(p, "bg=#313244") || strings.Contains(p, "●") {
		t.Errorf("quiet pill: %q", p)
	}
	p = Pill(snap(wait, session.Session{Status: state.StatusError}))
	if !strings.Contains(p, "✗ 1 #[fg=#9399b2]○ 1") || strings.Contains(p, "◐") {
		t.Errorf("error leads, no working count: %q", p)
	}
	// text inside the width conditional must hold no comma
	if body := Pill(snap(hot(time.Hour))); strings.Count(body[strings.Index(body, "#{?#{e|"):strings.Index(body, "● 1}")], ",") != 3 {
		t.Errorf("stray comma in the conditional: %q", body)
	}
	// done unseen joins the soft pill, after an error and before the rest;
	// it never fills the pill
	doneS := session.Session{Status: state.StatusDone}
	p = Pill(snap(work, doneS, doneS, wait))
	if !strings.Contains(p, "#[fg=#a6e3a1]✓ 2 #[fg=#89b4fa]◐ 1 #[fg=#9399b2]○ 1") || strings.Contains(p, "bg=#fab387") {
		t.Errorf("done in the soft pill: %q", p)
	}
	if p = Pill(snap(hot(time.Minute), doneS)); strings.Contains(p, "✓") {
		t.Errorf("a request is the only thing the pill says: %q", p)
	}
}

func TestStatusReads(t *testing.T) {
	f := &fakeTmux{opts: map[string]string{"status-right": "#{E:@agentbar_pill}#{session_name}"}}
	if !StatusReads(f) {
		t.Error("status-right reads the pill")
	}
	f = &fakeTmux{opts: map[string]string{"status-right": "#{session_name}", "status-left": "#S"}}
	if StatusReads(f) {
		t.Error("nothing reads agentbar options")
	}
}

func TestPublishPillAndRefresh(t *testing.T) {
	f := &fakeTmux{clients: "/dev/ttys001\n/dev/ttys002"}
	p := &Publisher{}
	s := Snapshot{At: time.Now(), Sessions: []session.Session{{ID: "a", Status: state.StatusWorking}}}
	p.Publish(f, s)
	j := f.joined()
	if !strings.Contains(j, "set-option -g @agentbar_pill #[range=user|agentbar]") {
		t.Errorf("pill not published:\n%s", j)
	}
	if !strings.Contains(j, "refresh-client -S -t /dev/ttys001") || !strings.Contains(j, "refresh-client -S -t /dev/ttys002") {
		t.Errorf("every client redraws on a change:\n%s", j)
	}
	n := len(f.calls)
	p.Publish(f, s)
	if len(f.calls) != n {
		t.Errorf("no change, no tmux calls:\n%s", f.joined())
	}
	p.Clear(f)
	if !strings.Contains(f.joined(), "set-option -g @agentbar_pill \n") {
		t.Errorf("clear empties the pill:\n%s", f.joined())
	}
}
