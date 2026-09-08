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
