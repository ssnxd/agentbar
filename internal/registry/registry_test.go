package registry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRealShape(t *testing.T) {
	dir := t.TempDir()
	src, err := os.ReadFile("testdata/11477.json")
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "11477.json"), src, 0o644)
	_ = os.WriteFile(filepath.Join(dir, "11477.abc.key"), []byte("k"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "99.json"), []byte(`{"pid":99,"sessionId":"dead","cwd":"/x","kind":"interactive"}`), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "bad.json"), []byte(`{`), 0o644)
	alive := func(pid int) bool { return pid == 11477 }

	es := Load(dir, alive)
	if len(es) != 1 {
		t.Fatalf("want 1 live entry, got %d: %+v", len(es), es)
	}
	e := es[0]
	if e.PID != 11477 || e.SessionID != "7da9487e-b329-4e2f-9b9a-6e7420ee2c90" || e.Name != "miivo-ef" || e.Kind != "interactive" || e.CWD != "/Users/miivo/code/miivo" {
		t.Errorf("%+v", e)
	}
	if e.TmuxSession != "miivo" || e.TmuxWindowID != "@0" || e.TmuxPaneID != "%0" {
		t.Errorf("tmux: %+v", e)
	}
	if e.StartedAt.Year() != 2026 || e.StatusUpdatedAt.IsZero() {
		t.Errorf("times: %+v", e)
	}
	if e.Status != "busy" {
		t.Errorf("status: %q", e.Status)
	}
}

func TestLoadSortsByStart(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "2.json"), []byte(`{"pid":2,"sessionId":"b","startedAt":2000}`), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "1.json"), []byte(`{"pid":1,"sessionId":"a","startedAt":1000}`), 0o644)
	es := Load(dir, func(int) bool { return true })
	if len(es) != 2 || es[0].SessionID != "a" || es[1].SessionID != "b" {
		t.Errorf("%+v", es)
	}
}

func TestLoadMissingDir(t *testing.T) {
	if es := Load("/nonexistent/agentbar-test", func(int) bool { return true }); es != nil {
		t.Errorf("expected nil, got %+v", es)
	}
}

func TestParseTmux(t *testing.T) {
	s, w, p, ok := ParseTmux("miivo:@10.%12")
	if !ok || s != "miivo" || w != "@10" || p != "%12" {
		t.Errorf("%q %q %q %v", s, w, p, ok)
	}
	s, _, _, ok = ParseTmux("my:sess:@1.%2")
	if !ok || s != "my:sess" {
		t.Errorf("session names may contain colons: %q %v", s, ok)
	}
	for _, bad := range []string{"", "garbage", "miivo:@1", "miivo:1.2"} {
		if _, _, _, ok := ParseTmux(bad); ok {
			t.Errorf("%q must fail", bad)
		}
	}
}

func TestAlive(t *testing.T) {
	if !Alive(os.Getpid()) {
		t.Error("own pid must be alive")
	}
	if Alive(0) || Alive(-1) {
		t.Error("non-positive pids are never alive")
	}
}
