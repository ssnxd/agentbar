package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/ssnxd/agentbar/internal/session"
	"github.com/ssnxd/agentbar/internal/state"
	"github.com/ssnxd/agentbar/internal/tmuxctl"
)

// Options the user sets:
//
//	@agentbar-notify on   a sound, and a desktop notification while you are
//	                      in another app, when a session needs you or is done
//	@agentbar-sound off   the notification without the sound
const (
	NotifyOption = "@agentbar-notify"
	SoundOption  = "@agentbar-sound"
)

// notifySettle is how long a status must hold before it interrupts you: a
// request answered at once, or a turn that ends in front of you, says
// nothing. Just under the daemon's tick, so it is one tick.
const notifySettle = 900 * time.Millisecond

// Notifier interrupts you, as little as it can, for a session that started
// needing you or finished:
//
//	its pane is on your screen     nothing: you see it
//	you are in tmux, elsewhere     a sound; the status line shows the rest
//	you are in another app         a sound and one desktop notification
//
// What the status line shows is immediate and is not its business.
type Notifier struct {
	prev    map[string]string // session id → status at the last snapshot
	pending map[string]note   // changes that have not settled yet
	started bool

	// seams for tests; nil means the real thing
	Sound   func(request bool)
	Desktop func(r tmuxctl.Runner, pane, title, body string)
}

type note struct {
	status string
	at     time.Time
}

func notable(status string) bool {
	return status == state.StatusNeedsYou || status == state.StatusDone
}

// Observe takes the snapshot of one tick, after Seen has applied to it.
func (n *Notifier) Observe(r tmuxctl.Runner, s Snapshot, v View) {
	if n.pending == nil {
		n.pending = map[string]note{}
	}
	cur := make(map[string]string, len(s.Sessions))
	byID := make(map[string]session.Session, len(s.Sessions))
	for _, ss := range s.Sessions {
		cur[ss.ID], byID[ss.ID] = ss.Status, ss
		// what was already so when the daemon started is not news
		if n.started && notable(ss.Status) && n.prev[ss.ID] != ss.Status {
			n.pending[ss.ID] = note{status: ss.Status, at: s.At}
		}
	}
	n.prev, n.started = cur, true

	var due []session.Session
	for id, p := range n.pending {
		ss, ok := byID[id]
		if !ok || ss.Status != p.status {
			delete(n.pending, id) // it did not last
			continue
		}
		if s.At.Sub(p.at) < notifySettle {
			continue
		}
		delete(n.pending, id)
		if !v.Visible[ss.TmuxPaneID] {
			due = append(due, ss)
		}
	}
	if len(due) == 0 || !optionOn(r, NotifyOption) {
		return
	}
	sort.SliceStable(due, func(i, j int) bool { return due[i].TmuxTarget < due[j].TmuxTarget })
	title, body, request := Message(due)
	if v, err := r.Run("show-options", "-gqv", SoundOption); err != nil || (v != "off" && v != "0") {
		if n.Sound != nil {
			n.Sound(request)
		} else {
			playSound(request)
		}
	}
	if v.Focused {
		return
	}
	for _, pane := range v.Front {
		if n.Desktop != nil {
			n.Desktop(r, pane, title, body)
		} else {
			writePane(r, pane, osc777(title, body))
		}
	}
}

func optionOn(r tmuxctl.Runner, name string) bool {
	v, err := r.Run("show-options", "-gqv", name)
	return err == nil && (v == "on" || v == "1")
}

// Message words one notification for the sessions that are due: a single
// session by name with what it asks or what it did, several as counts with
// their names. request reports whether any of them needs you.
func Message(due []session.Session) (title, body string, request bool) {
	var hot, done int
	var names []string
	seen := map[string]bool{}
	for _, s := range due {
		if s.Status == state.StatusNeedsYou {
			hot++
		} else {
			done++
		}
		if !seen[s.Project] {
			seen[s.Project] = true
			names = append(names, s.Project)
		}
	}
	if len(due) == 1 {
		s := due[0]
		if hot == 1 {
			body = s.Detail
			if body == "" {
				body = s.Title
			}
			return s.Project + " needs you", body, true
		}
		return s.Project + " done", s.Title, false
	}
	var parts []string
	if hot > 0 {
		parts = append(parts, session.NeedYou(hot))
	}
	if done > 0 {
		parts = append(parts, fmt.Sprintf("%d done", done))
	}
	return strings.Join(parts, ", "), strings.Join(names, ", "), hot > 0
}

// clean makes external text safe inside an escape sequence and short
// enough for a notification: control characters go, whitespace collapses.
// A title also loses its semicolons, which part the fields of OSC 777.
func clean(s string, max int, title bool) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r):
			return ' '
		case unicode.IsControl(r):
			return -1
		case title && r == ';':
			return ','
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		s = string(r[:max-1]) + "…"
	}
	return s
}

// osc777 is a desktop notification for the terminal (Ghostty, foot,
// WezTerm, ...), wrapped so that tmux passes it through to the terminal
// instead of reading it itself. It needs `allow-passthrough on`.
func osc777(title, body string) []byte {
	seq := "\x1b]777;notify;" + clean(title, 60, true) + ";" + clean(body, 140, false) + "\a"
	return []byte("\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\")
}

// The sounds macOS ships: a clear one for a request, a soft one, lower,
// for a turn that ended, which happens far more often.
const soundDir = "/System/Library/Sounds/"

func playSound(request bool) {
	name, vol := "Tink.aiff", "0.5"
	if request {
		name, vol = "Glass.aiff", "1"
	}
	afplay, err := exec.LookPath("afplay")
	if err != nil {
		return
	}
	if _, err := os.Stat(soundDir + name); err != nil {
		return
	}
	cmd := exec.Command(afplay, "-v", vol, soundDir+name)
	if cmd.Start() == nil {
		go func() { _ = cmd.Wait() }()
	}
}
