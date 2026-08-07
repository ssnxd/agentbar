package manager

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ssnxd/workflow/internal/config"
	"github.com/ssnxd/workflow/internal/db"
)

// landFixture builds a real git repo with a task branch and matching DB rows.
func landFixture(t *testing.T, conflicting bool) (*Manager, int64, string) {
	t.Helper()
	repo := t.TempDir()
	git := func(args ...string) string {
		out, err := gitRun(repo, args...)
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return out
	}
	git("init", "-q", "-b", "main")
	git("config", "user.email", "t@t.local")
	git("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-qm", "init")

	git("switch", "-qc", "wf/9-test")
	content := "from-task\n"
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	git("commit", "-aqm", "task work")
	git("switch", "-q", "main")
	if conflicting {
		if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("diverged-on-main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		git("commit", "-aqm", "conflicting main work")
	}

	store, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	proj, err := store.UpsertProject("r", repo)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := store.CreateTask(proj.ID, "test land", "p", "wf/9-test")
	if err != nil {
		t.Fatal(err)
	}

	m := &Manager{Store: store, Cfg: config.Config{}, Exe: "/bin/false"}
	return m, taskID, repo
}

func TestLandMerge(t *testing.T) {
	m, taskID, repo := landFixture(t, false)
	target, err := m.Land(taskID, false)
	if err != nil {
		t.Fatal(err)
	}
	if target != "main" {
		t.Errorf("landed into %q, want main", target)
	}
	raw, _ := os.ReadFile(filepath.Join(repo, "a.txt"))
	if string(raw) != "from-task\n" {
		t.Errorf("merge did not apply task content: %q", raw)
	}
	log, _ := gitRun(repo, "log", "--oneline", "-1")
	if !strings.Contains(log, "Merge wf/9-test") {
		t.Errorf("expected merge commit, got %q", log)
	}
}

func TestLandSquash(t *testing.T) {
	m, taskID, repo := landFixture(t, false)
	if _, err := m.Land(taskID, true); err != nil {
		t.Fatal(err)
	}
	log, _ := gitRun(repo, "log", "--oneline", "-1")
	if !strings.Contains(log, "workflow task #") {
		t.Errorf("expected squash commit message, got %q", log)
	}
}

func TestLandRefusesDirtyTree(t *testing.T) {
	m, taskID, repo := landFixture(t, false)
	if err := os.WriteFile(filepath.Join(repo, "dirty.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Land(taskID, false); err == nil || !strings.Contains(err.Error(), "uncommitted") {
		t.Errorf("dirty tree not refused: %v", err)
	}
}

func TestLandConflictAbortsCleanly(t *testing.T) {
	m, taskID, repo := landFixture(t, true)
	_, err := m.Land(taskID, false)
	if err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("conflict not surfaced: %v", err)
	}
	// The user's checkout must be left clean, not mid-merge.
	status, _ := gitRun(repo, "status", "--porcelain")
	if status != "" {
		t.Errorf("checkout left dirty after aborted merge:\n%s", status)
	}
}

func TestLandPreLandGate(t *testing.T) {
	m, taskID, _ := landFixture(t, false)
	m.Cfg.PreLand = "false" // always-failing gate
	if _, err := m.Land(taskID, false); err == nil || !strings.Contains(err.Error(), "pre_land") {
		t.Errorf("pre_land failure not surfaced: %v", err)
	}
}
