package cli

import "testing"

func TestTmuxVersionOK(t *testing.T) {
	ok := []string{"tmux 3.2", "tmux 3.2a", "tmux 3.7c", "tmux 4.0", "tmux next-3.5"}
	bad := []string{"tmux 3.1c", "tmux 2.9a", "garbage", ""}
	for _, v := range ok {
		if !TmuxVersionOK(v) {
			t.Errorf("%q should pass", v)
		}
	}
	for _, v := range bad {
		if TmuxVersionOK(v) {
			t.Errorf("%q should fail", v)
		}
	}
}
