package cli

import "testing"

func TestCheckTmuxVersion(t *testing.T) {
	ok := []string{"tmux 3.2", "tmux 3.2a", "tmux 3.7b", "tmux 3.10", "tmux 4.0"}
	for _, v := range ok {
		if err := CheckTmuxVersion(v); err != nil {
			t.Errorf("CheckTmuxVersion(%q) = %v, want nil", v, err)
		}
	}
	bad := []string{"tmux 3.1c", "tmux 2.9", "tmux 1.8", "garbage"}
	for _, v := range bad {
		if err := CheckTmuxVersion(v); err == nil {
			t.Errorf("CheckTmuxVersion(%q) = nil, want error", v)
		}
	}
}
