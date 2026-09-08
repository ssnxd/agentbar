package tmuxctl

import (
	"strings"
	"testing"
)

// windows: @1 (work) has zsh %5 + sidebar %6; @2 (work) has claude %7 only;
// @3 (dev, other session) has %8; @4 is a narrow window (60 cols).
const panes2 = "work|@1|2|%5|1|zsh|1|1||177\n" +
	"work|@1|2|%6|2|agentbar|0|1|1|177\n" +
	"work|@2|3|%7|1|claude|1|0||177\n" +
	"dev|@3|1|%8|1|2.1.263|1|1||177\n" +
	"dev|@4|2|%9|1|zsh|1|0||60\n"

func TestOpenAllSkipsExistingAndNarrow(t *testing.T) {
	f := &fake{panes: panes2, opts: map[string]string{"window-active-style": "bg=#1e1e2e"}}
	n, err := OpenAll(f, Opts{Side: "left", Width: 42, Cmd: "/bin/agentbar view"})
	if err != nil || n != 2 {
		t.Fatalf("n=%d err=%v\n%s", n, err, f.joined())
	}
	j := f.joined()
	for _, want := range []string{
		"split-window -h -b -l 42 -f -d -t %7 -P -F #{pane_id} /bin/agentbar view",
		"split-window -h -b -l 42 -f -d -t %8 -P -F #{pane_id} /bin/agentbar view",
		"set-option -p -t %99 @agentbar 1",
		"set-option -p -t %99 window-style bg=#1e1e2e",
		"resize-pane -t %99 -x 42",
	} {
		if !strings.Contains(j, want) {
			t.Errorf("missing %q in\n%s", want, j)
		}
	}
	if strings.Contains(j, "-t %5 ") || strings.Contains(j, "-t %9 ") {
		t.Errorf("must not split the window that has a sidebar or the narrow one:\n%s", j)
	}
}

func TestEnsureIdempotent(t *testing.T) {
	f := &fake{panes: panes2}
	ok, err := Ensure(f, Opts{Side: "left", Width: 42, Cmd: "x"}, "@1")
	if err != nil || ok || strings.Contains(f.joined(), "split-window") {
		t.Errorf("ok=%v err=%v\n%s", ok, err, f.joined())
	}
	f = &fake{panes: panes2}
	ok, err = Ensure(f, Opts{Side: "right", Width: 42, Cmd: "x"}, "@2")
	if err != nil || !ok || !strings.Contains(f.joined(), "split-window -h -l 42 -f -d -t %7") {
		t.Errorf("ok=%v err=%v\n%s", ok, err, f.joined())
	}
}

func TestCloseAll(t *testing.T) {
	f := &fake{panes: panes2}
	n, _ := CloseAll(f)
	if n != 1 || !strings.Contains(f.joined(), "kill-pane -t %6") {
		t.Errorf("n=%d\n%s", n, f.joined())
	}
}

func TestReflowPinsEverySidebar(t *testing.T) {
	f := &fake{panes: panes2}
	_ = Reflow(f, Opts{Width: 40})
	if !strings.Contains(f.joined(), "resize-pane -t %6 -x 40") {
		t.Errorf("\n%s", f.joined())
	}
}

func TestSweepEmpty(t *testing.T) {
	// every sidebar still has a main pane beside it: nothing happens
	f := &fake{panes: panes2}
	n, _ := SweepEmpty(f)
	if n != 0 || strings.Contains(f.joined(), "kill-pane") {
		t.Error("must not kill while a main pane remains")
	}
	// @1 is left with only its sidebar; @2 is fine
	f = &fake{panes: "work|@1|2|%6|2|agentbar|1|1|1|177\nwork|@2|3|%7|1|claude|1|0||177\nwork|@2|3|%8|2|agentbar|0|0|1|177\n"}
	n, _ = SweepEmpty(f)
	if n != 1 || !strings.Contains(f.joined(), "kill-pane -t %6") || strings.Contains(f.joined(), "kill-pane -t %8") {
		t.Errorf("n=%d\n%s", n, f.joined())
	}
}

func TestFocus(t *testing.T) {
	f := &fake{panes: panes2, cur: "%5 @1"}
	a, _ := Focus(f, "%5")
	if a != FocusSidebar || !strings.Contains(f.joined(), "select-pane -t %6") {
		t.Errorf("a=%v\n%s", a, f.joined())
	}
	f = &fake{panes: panes2, cur: "%6 @1"}
	a, _ = Focus(f, "%6")
	if a != FocusBack || !strings.Contains(f.joined(), "select-pane -l") {
		t.Errorf("a=%v\n%s", a, f.joined())
	}
	f = &fake{panes: panes2, cur: "%7 @2"}
	a, _ = Focus(f, "%7")
	if a != FocusNone {
		t.Errorf("a=%v", a)
	}
}

func TestJumpV2(t *testing.T) {
	f := &fake{panes: panes2, cur: "%6 @1"}
	if err := Jump(f, "%7"); err != nil {
		t.Fatal(err)
	}
	j := f.joined()
	if strings.Contains(j, "join-pane") || strings.Contains(j, "switch-client") {
		t.Errorf("same session: no join, no switch:\n%s", j)
	}
	if !strings.Contains(j, "select-window -t @2") || !strings.Contains(j, "select-pane -t %7") {
		t.Errorf("\n%s", j)
	}
	f = &fake{panes: panes2, cur: "%6 @1"}
	_ = Jump(f, "%8")
	if !strings.Contains(f.joined(), "switch-client -t dev") {
		t.Errorf("other session must switch client:\n%s", f.joined())
	}
	if err := Jump(&fake{panes: panes2, cur: "%6 @1"}, "%404"); err == nil {
		t.Error("unknown pane must error")
	}
}

func TestEnabledFlag(t *testing.T) {
	f := &fake{opts: map[string]string{"@agentbar-enabled": "1"}}
	if !Enabled(f) {
		t.Error("should be enabled")
	}
	_ = SetEnabled(f, false)
	if !strings.Contains(f.joined(), "set-option -g @agentbar-enabled 0") {
		t.Errorf("\n%s", f.joined())
	}
}
