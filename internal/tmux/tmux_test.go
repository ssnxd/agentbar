package tmux

import "testing"

func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		"api.fix":            "api-fix", // dots break tmux target parsing
		"my task: urgent":    "my-task-urgent",
		"..":                 "unnamed",
		"ok-name_1":          "ok-name_1",
		"":                   "unnamed",
		"a b\tc":             "a-b-c",
		"UPPER and lower":    "UPPER-and-lower",
		"-leading-trailing-": "leading-trailing",
	}
	for in, want := range cases {
		if got := SanitizeName(in); got != want {
			t.Errorf("SanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
	long := SanitizeName("this-is-a-very-long-name-that-should-be-truncated-somewhere")
	if len(long) > 40 {
		t.Errorf("long name not truncated: %q (%d)", long, len(long))
	}
}
