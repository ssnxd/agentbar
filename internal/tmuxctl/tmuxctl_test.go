package tmuxctl

import (
	"strings"
	"testing"
)

type fake struct {
	panes string
	cur   string
	opts  map[string]string
	calls []string
}

func (f *fake) Run(args ...string) (string, error) {
	f.calls = append(f.calls, strings.Join(args, " "))
	switch args[0] {
	case "list-panes":
		return f.panes, nil
	case "display-message":
		return f.cur, nil
	case "show-options":
		return f.opts[args[len(args)-1]], nil
	case "split-window":
		return "%99", nil
	}
	return "", nil
}

func (f *fake) joined() string { return strings.Join(f.calls, "\n") }

const panes = "work|@1|2|%5|1|zsh|1|1|\nwork|@1|2|%6|2|agentbar|0|1|1\nwork|@2|3|%7|1|claude|1|0|\ndev|@3|1|%8|1|2.1.263|1|1|\n"

func TestListPanesParsesSidebarFlag(t *testing.T) {
	ps, err := ListPanes(&fake{panes: panes})
	if err != nil || len(ps) != 4 {
		t.Fatal(err, len(ps))
	}
	if !ps[1].Sidebar || ps[0].Sidebar || ps[2].Sidebar {
		t.Errorf("sidebar flags: %+v", ps)
	}
	if TargetLabel(ps[2]) != "work:3.1" || TargetLabel(ps[0]) != "work:2.1" {
		t.Errorf("labels: %q %q", TargetLabel(ps[2]), TargetLabel(ps[0]))
	}
	if !ps[0].Active || ps[1].Active || !ps[0].WindowActive || ps[2].WindowActive {
		t.Errorf("active flags: %+v", ps)
	}
	if sb := FindSidebar(ps); sb == nil || sb.PaneID != "%6" {
		t.Errorf("FindSidebar: %+v", sb)
	}
	if sb := FindSidebar(ps[:1]); sb != nil {
		t.Errorf("no sidebar expected, got %+v", sb)
	}
}

func TestLooksLikeClaude(t *testing.T) {
	for _, ok := range []string{"claude", "2.1.263", "node"} {
		if !LooksLikeClaude(ok) {
			t.Errorf("%q should look like claude", ok)
		}
	}
	for _, bad := range []string{"zsh", "agentbar", "vim", ""} {
		if LooksLikeClaude(bad) {
			t.Errorf("%q must not look like claude", bad)
		}
	}
}

func TestDecide(t *testing.T) {
	sb := Pane{WindowID: "@1", PaneID: "%6", Sidebar: true}
	cases := []struct {
		name string
		s    Situation
		want Action
	}{
		{"none → open", Situation{CurrentPane: "%5", CurrentWindow: "@1"}, ActionOpen},
		{"here focused → close", Situation{Sidebar: &sb, CurrentPane: "%6", CurrentWindow: "@1"}, ActionClose},
		{"here unfocused → focus", Situation{Sidebar: &sb, CurrentPane: "%5", CurrentWindow: "@1"}, ActionFocus},
		{"elsewhere → move", Situation{Sidebar: &sb, CurrentPane: "%7", CurrentWindow: "@2"}, ActionMove},
	}
	for _, c := range cases {
		if got := Decide(c.s); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestToggleOpensWithTagAndStyle(t *testing.T) {
	f := &fake{panes: "work|@1|2|%5|1|zsh|1|1|\n", cur: "%5 @1", opts: map[string]string{"window-active-style": "fg=#cdd6f4,bg=#1e1e2e"}}
	if err := Toggle(f, Opts{Side: "left", Width: 42, Cmd: "/bin/agentbar ui"}, "%5"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"split-window -h -b -l 42 -t %5 -P -F #{pane_id} /bin/agentbar ui",
		"set-option -p -t %99 @agentbar 1",
		"select-pane -t %99 -P fg=#cdd6f4,bg=#1e1e2e",
	} {
		if !strings.Contains(f.joined(), want) {
			t.Errorf("missing %q in\n%s", want, f.joined())
		}
	}
}

func TestToggleRightSideHasNoB(t *testing.T) {
	f := &fake{panes: "work|@1|2|%5|1|zsh|1|1|\n", cur: "%5 @1"}
	_ = Toggle(f, Opts{Side: "right", Width: 30, Cmd: "x"}, "%5")
	if !strings.Contains(f.joined(), "split-window -h -l 30 -t %5") || strings.Contains(f.joined(), " -b ") {
		t.Errorf("calls:\n%s", f.joined())
	}
}

func TestToggleCloseFocusMove(t *testing.T) {
	f := &fake{panes: panes, cur: "%6 @1"}
	_ = Toggle(f, Opts{Side: "left", Width: 42}, "%6")
	if !strings.Contains(f.joined(), "kill-pane -t %6") {
		t.Errorf("close:\n%s", f.joined())
	}

	f = &fake{panes: panes, cur: "%5 @1"}
	_ = Toggle(f, Opts{Side: "left", Width: 42}, "%5")
	if !strings.Contains(f.joined(), "select-pane -t %6") || strings.Contains(f.joined(), "join-pane") {
		t.Errorf("focus:\n%s", f.joined())
	}

	f = &fake{panes: panes, cur: "%7 @2"}
	_ = Toggle(f, Opts{Side: "left", Width: 42}, "%7")
	if !strings.Contains(f.joined(), "join-pane -h -b -l 42 -s %6 -t %7") || !strings.Contains(f.joined(), "select-pane -t %6") {
		t.Errorf("move:\n%s", f.joined())
	}
}

func TestJumpMovesSidebarThenSelects(t *testing.T) {
	f := &fake{panes: panes, cur: "%6 @1"}
	if err := Jump(f, Opts{Side: "left", Width: 42}, "%7"); err != nil {
		t.Fatal(err)
	}
	j := f.joined()
	idxJoin := strings.Index(j, "join-pane -h -b -l 42 -s %6 -t %7")
	idxSel := strings.Index(j, "select-pane -t %7")
	if idxJoin < 0 || idxSel < 0 || idxJoin > idxSel {
		t.Errorf("calls:\n%s", j)
	}
	if !strings.Contains(j, "select-window -t @2") {
		t.Errorf("window not selected:\n%s", j)
	}
	if strings.Contains(j, "switch-client") {
		t.Errorf("same session must not switch client:\n%s", j)
	}
}

func TestJumpAcrossSessionsSwitchesClient(t *testing.T) {
	f := &fake{panes: panes, cur: "%6 @1"}
	_ = Jump(f, Opts{Side: "left", Width: 42}, "%8")
	if !strings.Contains(f.joined(), "switch-client -t dev") {
		t.Errorf("calls:\n%s", f.joined())
	}
}

func TestJumpSameWindowDoesNotJoin(t *testing.T) {
	f := &fake{panes: panes, cur: "%6 @1"}
	_ = Jump(f, Opts{Side: "left", Width: 42}, "%5")
	if strings.Contains(f.joined(), "join-pane") {
		t.Errorf("no join expected:\n%s", f.joined())
	}
}

func TestJumpUnknownPane(t *testing.T) {
	f := &fake{panes: panes, cur: "%6 @1"}
	if err := Jump(f, Opts{Side: "left", Width: 42}, "%404"); err == nil {
		t.Error("unknown pane must error")
	}
}

func TestFollow(t *testing.T) {
	// sidebar %6 lives in @1; user switched to @2 whose active pane is %7
	f := &fake{panes: panes}
	if err := Follow(f, Opts{Side: "left", Width: 42}, "@2"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.joined(), "join-pane -d -h -b -l 42 -s %6 -t %7") {
		t.Errorf("calls:\n%s", f.joined())
	}
	if strings.Contains(f.joined(), "select-pane") {
		t.Error("follow must not steal focus")
	}

	f = &fake{panes: panes}
	_ = Follow(f, Opts{Side: "left", Width: 42}, "@1")
	if strings.Contains(f.joined(), "join-pane") {
		t.Error("already in this window: no join")
	}

	f = &fake{panes: "work|@1|2|%5|1|zsh|1|1|\n"}
	_ = Follow(f, Opts{Side: "left", Width: 42}, "@2")
	if strings.Contains(f.joined(), "join-pane") {
		t.Error("no sidebar: no join")
	}
}

func TestLoadOpts(t *testing.T) {
	f := &fake{opts: map[string]string{"@agentbar-side": "right", "@agentbar-width": "50"}}
	o := LoadOpts(f)
	if o.Side != "right" || o.Width != 50 {
		t.Errorf("%+v", o)
	}
	o = LoadOpts(&fake{opts: map[string]string{"@agentbar-width": "junk", "@agentbar-side": "up"}})
	if o.Side != "left" || o.Width != 42 {
		t.Errorf("defaults: %+v", o)
	}
}
