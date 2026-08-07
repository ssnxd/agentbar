package msg

import (
	"path/filepath"
	"sync"
	"testing"
)

func TestSendDrain(t *testing.T) {
	inbox := filepath.Join(t.TempDir(), "inboxes", "orchestrator.json")
	if n := Pending(inbox); n != 0 {
		t.Fatalf("empty inbox pending = %d", n)
	}
	if err := Send(inbox, "api-fix", "done with part 1"); err != nil {
		t.Fatal(err)
	}
	if err := Send(inbox, "ui-fix", "blocked on schema"); err != nil {
		t.Fatal(err)
	}
	if n := Pending(inbox); n != 2 {
		t.Fatalf("pending = %d, want 2", n)
	}
	msgs, err := Drain(inbox)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[0].From != "api-fix" || msgs[1].Body != "blocked on schema" {
		t.Fatalf("unexpected drain: %+v", msgs)
	}
	if n := Pending(inbox); n != 0 {
		t.Fatalf("pending after drain = %d", n)
	}
}

// Concurrent writers must not lose messages (flock serialization).
func TestConcurrentSend(t *testing.T) {
	inbox := filepath.Join(t.TempDir(), "in.json")
	const n = 20
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := Send(inbox, "w", "m"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if got := Pending(inbox); got != n {
		t.Errorf("lost messages: pending = %d, want %d", got, n)
	}
}
