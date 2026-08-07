package external

import "testing"

func TestParsePSLine(t *testing.T) {
	ok := []string{
		" 1946  Fri Aug  7 13:08:12 2026     ttys002  claude --dangerously-skip-permissions --resume",
		"22    Mon Jan  5 09:00:00 2026 ttys000 /usr/local/bin/claude",
		"23    Mon Jan  5 09:00:00 2026 ttys000 /Users/x/.local/bin/claude --model fable",
	}
	for _, line := range ok {
		if _, matched := ParsePSLine(line); !matched {
			t.Errorf("should match claude process: %q", line)
		}
	}
	bad := []string{
		" 100 Fri Aug  7 13:08:12 2026 ttys002 vim claude-notes.md",
		" 101 Fri Aug  7 13:08:12 2026 ttys002 grep claude something",
		" 102 Fri Aug  7 13:08:12 2026 ttys002 /bin/claudette --run",
		" 103 Fri Aug  7 13:08:12 2026 ttys002 node /x/claude-helper.js",
		"garbage line",
		"",
	}
	for _, line := range bad {
		if _, matched := ParsePSLine(line); matched {
			t.Errorf("must not match: %q", line)
		}
	}

	s, _ := ParsePSLine(ok[0])
	if s.PID != 1946 || s.TTY != "ttys002" {
		t.Errorf("bad parse: %+v", s)
	}
	if s.StartedAt.IsZero() {
		t.Error("lstart not parsed")
	}
}

func TestMungeProjectDir(t *testing.T) {
	if got := MungeProjectDir("/Users/x/code/my.app"); got != "-Users-x-code-my-app" {
		t.Errorf("munge = %q", got)
	}
}
