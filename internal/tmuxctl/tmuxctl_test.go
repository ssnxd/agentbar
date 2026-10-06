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

const panes = "work|@1|2|%5|1|zsh|1|1||177|0|$1\nwork|@1|2|%6|2|agentbar|0|1|1|177|0|$1\nwork|@2|3|%7|1|claude|1|0||177|0|$1\ndev|@3|1|%8|1|2.1.263|1|1||177|0|$1\n"

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
	if ps[0].WindowWidth != 177 {
		t.Errorf("window width: %+v", ps[0])
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

func TestCurrent(t *testing.T) {
	f := &fake{cur: "%5 @1"}
	p, w, err := Current(f, "%5")
	if err != nil || p != "%5" || w != "@1" || !strings.Contains(f.joined(), "display-message -p -t %5") {
		t.Errorf("%q %q %v\n%s", p, w, err, f.joined())
	}
}

func TestListPanesReadsZoomAndSessionID(t *testing.T) {
	f := &fake{panes: "work|@1|2|%5|1|zsh|1|1||177|1|$4\nwork|@2|3|%7|1|claude|1|0||177|0|$4\n"}
	ps, err := ListPanes(f)
	if err != nil || len(ps) != 2 {
		t.Fatalf("got %v, %v", ps, err)
	}
	if !ps[0].Zoomed || ps[1].Zoomed || ps[0].SessionID != "$4" {
		t.Errorf("zoom and session id: %+v", ps)
	}
}

type clientFake struct{ out string }

func (c clientFake) Run(args ...string) (string, error) { return c.out, nil }

func TestListClients(t *testing.T) {
	// a session name may hold the separator; the flags never do
	cs, err := ListClients(clientFake{"attached,focused,UTF-8|work\nattached,UTF-8|a|b\n\n"})
	if err != nil || len(cs) != 2 {
		t.Fatalf("got %v, %v", cs, err)
	}
	if !cs[0].Focused || cs[0].Session != "work" || cs[1].Focused || cs[1].Session != "a|b" {
		t.Errorf("clients: %+v", cs)
	}
}
