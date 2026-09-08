package transcript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const sample = `{"type":"user","message":{"role":"user","content":"fix   the auth\n bug"},"gitBranch":"fix/auth","permissionMode":"default"}
{"type":"assistant","message":{"model":"claude-fable-5-1","usage":{"input_tokens":10,"cache_read_input_tokens":900,"cache_creation_input_tokens":90}}}
{"type":"system","subtype":"turn_duration","isMeta":false}
{"aiTitle":"Fix auth middleware","type":"ai-title"}
`

func TestTail(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	_ = os.WriteFile(p, []byte(sample), 0o644)
	i := Tail(p)
	if !i.Known {
		t.Fatal("not known")
	}
	if i.Title != "Fix auth middleware" || i.FirstPrompt != "fix the auth bug" || i.Model != "claude-fable-5-1" || i.Branch != "fix/auth" || i.ContextTokens != 1000 || i.PermissionMode != "default" {
		t.Errorf("%+v", i)
	}
	if i.TurnOpen {
		t.Error("turn_duration closes the turn")
	}
	if i.LastActivity.IsZero() {
		t.Error("LastActivity should be the file mtime")
	}
}

func TestTailReopensTurnAndInvalidatesCache(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	_ = os.WriteFile(p, []byte(sample), 0o644)
	if Tail(p).TurnOpen {
		t.Fatal("closed turn expected")
	}
	open := sample + `{"type":"assistant","message":{"model":"m"}}` + "\n"
	_ = os.WriteFile(p, []byte(open), 0o644)
	if !Tail(p).TurnOpen {
		t.Error("assistant line after turn_duration reopens the turn; cache must notice the size change")
	}
}

func TestTailIgnoresUserToolResultsForFirstPrompt(t *testing.T) {
	// A user line whose content is an array (tool_result) is not a prompt.
	body := `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}
{"type":"user","message":{"role":"user","content":"real prompt"}}
`
	p := filepath.Join(t.TempDir(), "s.jsonl")
	_ = os.WriteFile(p, []byte(body), 0o644)
	if got := Tail(p).FirstPrompt; got != "real prompt" {
		t.Errorf("got %q", got)
	}
}

func TestTailOnlyReadsLastChunk(t *testing.T) {
	p := filepath.Join(t.TempDir(), "big.jsonl")
	var b strings.Builder
	b.WriteString(`{"aiTitle":"old title"}` + "\n")
	for i := 0; i < 3000; i++ {
		b.WriteString(`{"type":"assistant","message":{"model":"m","usage":{"input_tokens":1}}}` + "\n")
	}
	b.WriteString(`{"aiTitle":"new title"}` + "\n")
	_ = os.WriteFile(p, []byte(b.String()), 0o644)
	if got := Tail(p).Title; got != "new title" {
		t.Errorf("got %q", got)
	}
}

func TestTailMissing(t *testing.T) {
	if Tail("/nonexistent/x.jsonl").Known {
		t.Error("missing file must not be Known")
	}
}

func TestNewestPrefersSessionFile(t *testing.T) {
	projects := t.TempDir()
	cwd := "/Users/x/code/app"
	dir := filepath.Join(projects, MungeProjectDir(cwd))
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "other.jsonl"), []byte("{}\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "sid.jsonl"), []byte("{}\n"), 0o644)
	// make "other" newer so the fallback would pick it
	future := time.Now().Add(time.Hour)
	_ = os.Chtimes(filepath.Join(dir, "other.jsonl"), future, future)

	if got := Newest(projects, cwd, "sid"); filepath.Base(got) != "sid.jsonl" {
		t.Errorf("got %q", got)
	}
	if got := Newest(projects, cwd, "missing"); filepath.Base(got) != "other.jsonl" {
		t.Errorf("fallback to newest: got %q", got)
	}
	if got := Newest(projects, "/nope", ""); got != "" {
		t.Error("missing dir must yield empty")
	}
}

func TestNewestFindsSessionUnderAnotherProjectDir(t *testing.T) {
	projects := t.TempDir()
	origin := filepath.Join(projects, MungeProjectDir("/u/code/app"))
	_ = os.MkdirAll(origin, 0o755)
	_ = os.WriteFile(filepath.Join(origin, "moved.jsonl"), []byte("{}\n"), 0o644)
	// the session later cd'd into a worktree; its cwd's project dir is empty
	wt := "/u/code/app/.claude/worktrees/feat"
	_ = os.MkdirAll(filepath.Join(projects, MungeProjectDir(wt)), 0o755)
	got := Newest(projects, wt, "moved")
	if filepath.Base(got) != "moved.jsonl" || filepath.Dir(got) != origin {
		t.Errorf("got %q", got)
	}
}

func TestMungeProjectDir(t *testing.T) {
	if got := MungeProjectDir("/Users/x/code/my.app"); got != "-Users-x-code-my-app" {
		t.Errorf("munge = %q", got)
	}
}
