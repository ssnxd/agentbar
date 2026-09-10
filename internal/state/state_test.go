package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteReadDelete(t *testing.T) {
	dir := t.TempDir()
	r := Record{SessionID: "abc", CWD: "/x", Status: StatusNeedsYou, Detail: "Bash git push", Tool: "Bash", Event: "PermissionRequest", UpdatedAt: time.Now()}
	if err := Write(dir, r); err != nil {
		t.Fatal(err)
	}
	got, ok := Read(dir, "abc")
	if !ok || got.Status != StatusNeedsYou || got.Detail != "Bash git push" {
		t.Fatalf("read back: %+v ok=%v", got, ok)
	}
	if err := Delete(dir, "abc"); err != nil {
		t.Fatal(err)
	}
	if _, ok := Read(dir, "abc"); ok {
		t.Fatal("still present after delete")
	}
	if err := Delete(dir, "abc"); err != nil {
		t.Fatal("deleting a missing record must not error")
	}
}

func TestWriteCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "state")
	if err := Write(dir, Record{SessionID: "x", Status: StatusWorking, UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if _, ok := Read(dir, "x"); !ok {
		t.Fatal("not readable after write into a new dir")
	}
}

func TestReadAllSkipsGarbage(t *testing.T) {
	dir := t.TempDir()
	_ = Write(dir, Record{SessionID: "one", Status: StatusWorking, UpdatedAt: time.Now()})
	_ = os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{nope"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, ".tmp-123"), []byte("{}"), 0o644)
	all := ReadAll(dir)
	if len(all) != 1 || all["one"].Status != StatusWorking {
		t.Fatalf("got %+v", all)
	}
}

func TestSweep(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	_ = Write(dir, Record{SessionID: "live-old", Status: StatusWaiting, UpdatedAt: now.Add(-3 * time.Hour)})
	_ = Write(dir, Record{SessionID: "dead-old", Status: StatusWaiting, UpdatedAt: now.Add(-3 * time.Hour)})
	_ = Write(dir, Record{SessionID: "dead-new", Status: StatusWaiting, UpdatedAt: now.Add(-5 * time.Minute)})
	n := Sweep(dir, map[string]bool{"live-old": true}, time.Hour, now)
	if n != 1 {
		t.Fatalf("swept %d, want 1", n)
	}
	if _, ok := Read(dir, "dead-old"); ok {
		t.Fatal("dead-old should be gone")
	}
	if _, ok := Read(dir, "dead-new"); !ok {
		t.Fatal("dead-new is too young to sweep")
	}
	if _, ok := Read(dir, "live-old"); !ok {
		t.Fatal("live sessions are never swept")
	}
}

func TestAgentRecords(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	a1 := Agent{SessionID: "s1", AgentID: "a1", Type: "Explore", StartedAt: now.Add(-time.Minute), UpdatedAt: now}
	a2 := Agent{SessionID: "s1", AgentID: "a2", Type: "general-purpose", Tool: "Bash", Detail: "Bash go test", StartedAt: now, UpdatedAt: now}
	b1 := Agent{SessionID: "s2", AgentID: "b1", Type: "Plan", StartedAt: now, UpdatedAt: now}
	for _, a := range []Agent{a2, a1, b1} {
		if err := WriteAgent(dir, a); err != nil {
			t.Fatal(err)
		}
	}
	got, ok := ReadAgent(dir, "s1", "a2")
	if !ok || got.Detail != "Bash go test" || got.Type != "general-purpose" {
		t.Fatalf("read back: %+v ok=%v", got, ok)
	}
	all := ReadAgents(dir)
	if len(all["s1"]) != 2 || len(all["s2"]) != 1 {
		t.Fatalf("grouping: %+v", all)
	}
	if all["s1"][0].AgentID != "a1" || all["s1"][1].AgentID != "a2" {
		t.Errorf("agents must be ordered by StartedAt: %+v", all["s1"])
	}
	// agent files must not leak into the session records
	if recs := ReadAll(dir); len(recs) != 0 {
		t.Errorf("agent files read as session records: %+v", recs)
	}
	if err := DeleteAgent(dir, "s1", "a1"); err != nil {
		t.Fatal(err)
	}
	if err := DeleteAgent(dir, "s1", "a1"); err != nil {
		t.Fatal("deleting a missing agent must not error")
	}
	if all := ReadAgents(dir); len(all["s1"]) != 1 {
		t.Errorf("after delete: %+v", all)
	}
	if err := DeleteSessionAgents(dir, "s1"); err != nil {
		t.Fatal(err)
	}
	all = ReadAgents(dir)
	if _, ok := all["s1"]; ok || len(all["s2"]) != 1 {
		t.Errorf("session delete must drop only that session's agents: %+v", all)
	}
}

func TestSweepAgents(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	_ = WriteAgent(dir, Agent{SessionID: "live", AgentID: "fresh", UpdatedAt: now.Add(-time.Minute)})
	_ = WriteAgent(dir, Agent{SessionID: "live", AgentID: "stale", UpdatedAt: now.Add(-3 * time.Hour)})
	_ = WriteAgent(dir, Agent{SessionID: "dead", AgentID: "fresh", UpdatedAt: now.Add(-time.Minute)})
	n := SweepAgents(dir, map[string]bool{"live": true}, time.Hour, now)
	if n != 2 {
		t.Fatalf("swept %d, want 2", n)
	}
	all := ReadAgents(dir)
	if len(all) != 1 || len(all["live"]) != 1 || all["live"][0].AgentID != "fresh" {
		t.Errorf("got %+v", all)
	}
}
