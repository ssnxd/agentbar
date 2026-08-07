package db

import (
	"path/filepath"
	"testing"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestTaskAgentRoundtrip(t *testing.T) {
	s := openTest(t)
	proj, err := s.UpsertProject("repo", "/tmp/repo")
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := s.CreateTask(proj.ID, "fix auth", "do it", "wf/1-fix-auth")
	if err != nil {
		t.Fatal(err)
	}
	agentID, err := s.CreateAgent(Agent{
		TaskID: taskID, Name: "orchestrator", Role: RoleOrchestrator,
		Model: "fable", ClaudeSessionID: "uuid-1",
	})
	if err != nil {
		t.Fatal(err)
	}

	a, err := s.GetAgentBySession("uuid-1")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != agentID || a.Status != StatusStarting {
		t.Errorf("unexpected agent: %+v", a)
	}

	tasks, err := s.ListTasks(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].ProjectName != "repo" {
		t.Errorf("unexpected tasks: %+v", tasks)
	}
}

// Sticky terminal statuses: a late Stop hook must not resurrect a done agent.
func TestStatusStickiness(t *testing.T) {
	s := openTest(t)
	proj, _ := s.UpsertProject("r", "/tmp/r")
	taskID, _ := s.CreateTask(proj.ID, "t", "p", "b")
	id, _ := s.CreateAgent(Agent{TaskID: taskID, Name: "w", Role: RoleWorker,
		Model: "sonnet", ClaudeSessionID: "u2"})

	steps := []struct {
		set, want string
	}{
		{StatusWorking, StatusWorking},
		{StatusIdle, StatusIdle},
		{StatusDone, StatusDone},
		{StatusIdle, StatusDone},    // sticky: Stop after done
		{StatusWorking, StatusDone}, // sticky: PreToolUse after done
		{StatusNeedsYou, StatusNeedsYou},
		{StatusDead, StatusDead},
		{StatusWorking, StatusWorking}, // NOT sticky: a live hook resurrects dead
	}
	for _, st := range steps {
		if err := s.SetAgentStatus(id, st.set); err != nil {
			t.Fatal(err)
		}
		a, _ := s.GetAgent(id)
		if a.Status != st.want {
			t.Errorf("after set %q: status = %q, want %q", st.set, a.Status, st.want)
		}
	}
}

func TestArchiveHidesTask(t *testing.T) {
	s := openTest(t)
	proj, _ := s.UpsertProject("r", "/tmp/r2")
	taskID, _ := s.CreateTask(proj.ID, "t", "p", "b")
	if err := s.ArchiveTask(taskID); err != nil {
		t.Fatal(err)
	}
	tasks, _ := s.ListTasks(false)
	if len(tasks) != 0 {
		t.Errorf("archived task still listed: %+v", tasks)
	}
	all, _ := s.ListTasks(true)
	if len(all) != 1 {
		t.Errorf("archived task not in full list")
	}
}
