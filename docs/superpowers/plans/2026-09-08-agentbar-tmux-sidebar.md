# agentbar tmux sidebar Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the `workflow` orchestrator in this repo with `agentbar`, a tmux sidebar that lists every running Claude Code session with its status, tmux location, and one-key jump/accept/kill.

**Architecture:** Hooks in the user's global Claude settings write one JSON state file per session. Claude's own registry (`~/.claude/sessions/*.json`) supplies discovery and tmux location. A Bubble Tea sidebar polls both plus the transcript tail every second. A `toggle` subcommand bound to `prefix a` opens, focuses, moves, or closes the tagged sidebar pane.

**Tech Stack:** Go 1.27, charm.land/bubbletea/v2 v2.0.8, charm.land/lipgloss/v2 v2.0.5, tmux 3.7 (3.2 minimum), Claude Code 2.1.263 hooks.

**Spec:** `docs/superpowers/specs/2026-09-08-agentbar-tmux-sidebar-design.md`

## Global Constraints

- Module path: `github.com/ssnxd/agentbar`. Binary name: `agentbar`. Installed to `~/.local/bin/agentbar`.
- tmux ≥ 3.2. Only the user's default tmux server. Strip `TMUX` and `TMUX_PANE` from env on every tmux call.
- Hook receiver must exit 0 always, never print to stdout, timeout 5s in settings.
- State dir: `~/.local/share/agentbar/state/`. Log: `~/.local/share/agentbar/agentbar.log`.
- Status vocabulary (exact strings): `needs-you`, `working`, `waiting`, `error`, `unknown`.
- Sidebar defaults: key `a`, width 42, side `left`, pane option tag `@agentbar=1`.
- Palette: Catppuccin Mocha.
- Commit after every task. No `Co-Authored-By` or AI mentions in commit messages. Never push.
- Never claim a step works without running it and showing output.

---

## File structure

```
cmd/agentbar/main.go              dispatch: ui | event | toggle | jump | install | doctor | version
internal/paths/paths.go           StateDir, LogDir, ClaudeDir, SettingsPath; env overrides for tests
internal/state/state.go           Record type, Write (atomic), Read, ReadAll, Delete, Sweep
internal/state/state_test.go
internal/hooks/payload.go         Payload type, Parse(stdin), Summarize(toolName, toolInput)
internal/hooks/mapping.go         Map(event, statusOverride, payload) -> (status, detail, delete bool)
internal/hooks/settings.go        Entries(exe) hook JSON, Merge(settingsJSON, exe), Remove(settingsJSON, exe)
internal/hooks/hooks_test.go
internal/registry/registry.go     Entry type, Load(dir) []Entry, ParseTmux("s:@w.%p"), pidAlive
internal/registry/registry_test.go + testdata/11477.json
internal/transcript/transcript.go moved parser: Info type, Tail(path) Info, Newest(cwd, sessionID) path, MungeProjectDir, cache
internal/transcript/transcript_test.go
internal/session/session.go       Session type, Discover(deps) []Session, resolveStatus(...)
internal/session/session_test.go
internal/tmuxctl/tmuxctl.go       Runner interface, Exec runner, pane queries and actions
internal/tmuxctl/toggle.go        Decide(state) action; Toggle(r, opts)
internal/tmuxctl/tmuxctl_test.go  fake runner
internal/ui/model.go              Model, Init/Update/View, messages
internal/ui/render.go             group/sort, row rendering, header, footer
internal/ui/styles.go             palette and status looks
internal/ui/render_test.go        snapshot tests
internal/cli/event.go             agentbar event
internal/cli/toggle.go            agentbar toggle | jump
internal/cli/install.go           agentbar install [--uninstall]
internal/cli/doctor.go            agentbar doctor
agentbar.tmux                     plugin entry
Makefile, README.md, CHANGELOG.md, .goreleaser.yaml, .github/workflows/*.yml
```

---

### Task 1: Strip the repo and rename the module

**Files:**
- Delete: `internal/{db,manager,msg,gitx,repos,notify,config,tui,cli,hooks}`, `assets/`, `cmd/workflow/`, `ROADMAP.md`, `CHANGELOG.md`, `docs/dashboard.png`
- Move: `internal/external/external.go` → `internal/transcript/transcript.go` (trimmed in Task 4)
- Modify: `go.mod` (module path), `Makefile`, `.goreleaser.yaml`, `.github/workflows/*.yml`, `README.md` (stub)
- Create: `cmd/agentbar/main.go`, `internal/paths/paths.go`

**Interfaces:**
- Produces: `paths.StateDir() string`, `paths.LogPath() string`, `paths.ClaudeDir() string`, `paths.SettingsPath() string`, `paths.SessionsDir() string`, `paths.ProjectsDir() string`. Each honours an env override (`AGENTBAR_STATE_DIR`, `AGENTBAR_CLAUDE_DIR`) so tests can point at temp dirs.

- [ ] **Step 1: Delete the orchestrator**

```bash
git rm -rq internal/db internal/manager internal/msg internal/gitx internal/repos internal/notify internal/config internal/tui internal/cli internal/hooks assets cmd/workflow ROADMAP.md CHANGELOG.md docs/dashboard.png
git mv internal/external internal/transcript
git mv internal/transcript/external.go internal/transcript/transcript.go
git mv internal/transcript/external_test.go internal/transcript/transcript_test.go
```

- [ ] **Step 2: Rename the module**

```bash
sed -i '' 's#github.com/ssnxd/workflow#github.com/ssnxd/agentbar#g' go.mod $(git ls-files '*.go') .goreleaser.yaml
sed -i '' 's/^package external/package transcript/' internal/transcript/*.go
```

- [ ] **Step 3: Write `internal/paths/paths.go`**

```go
// Package paths centralises every filesystem location agentbar reads or
// writes. Env overrides exist for tests and for users with unusual layouts.
package paths

import (
	"os"
	"path/filepath"
)

func home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return h
}

// DataDir is ~/.local/share/agentbar (AGENTBAR_STATE_DIR overrides).
func DataDir() string {
	if d := os.Getenv("AGENTBAR_STATE_DIR"); d != "" {
		return d
	}
	return filepath.Join(home(), ".local", "share", "agentbar")
}

func StateDir() string { return filepath.Join(DataDir(), "state") }
func LogPath() string  { return filepath.Join(DataDir(), "agentbar.log") }

// ClaudeDir is ~/.claude (AGENTBAR_CLAUDE_DIR overrides).
func ClaudeDir() string {
	if d := os.Getenv("AGENTBAR_CLAUDE_DIR"); d != "" {
		return d
	}
	return filepath.Join(home(), ".claude")
}

func SettingsPath() string { return filepath.Join(ClaudeDir(), "settings.json") }
func SessionsDir() string  { return filepath.Join(ClaudeDir(), "sessions") }
func ProjectsDir() string  { return filepath.Join(ClaudeDir(), "projects") }
```

- [ ] **Step 4: Write a minimal `cmd/agentbar/main.go`**

```go
// agentbar — a tmux sidebar for running Claude Code sessions.
package main

import (
	"fmt"
	"os"
)

var version = "dev"

func main() {
	args := os.Args[1:]
	cmd := "ui"
	if len(args) > 0 {
		cmd = args[0]
		args = args[1:]
	}
	switch cmd {
	case "version", "-v", "--version":
		fmt.Println("agentbar", version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "agentbar: unknown command %q\n%s", cmd, usage)
		os.Exit(1)
	}
}

const usage = `agentbar — a tmux sidebar for running Claude Code sessions

usage:
  agentbar            run the sidebar UI (normally launched by toggle)
  agentbar toggle     open / focus / move / close the sidebar (bind to prefix a)
  agentbar jump <pane-id>   move the sidebar next to a pane and focus the pane
  agentbar install [--uninstall]   add hooks to ~/.claude/settings.json
  agentbar doctor     check tmux, hooks, registry
  agentbar event      (hook plumbing) receive a Claude Code hook on stdin
  agentbar version
`
```

Subcommands are wired in later tasks; each task adds its `case`.

- [ ] **Step 5: Makefile, goreleaser, CI**

Makefile: `BIN := agentbar`, build path `./cmd/agentbar`, `install` target writes `$(HOME)/.local/bin/$(BIN)`. In `.goreleaser.yaml` replace `cmd/workflow` with `cmd/agentbar` and the binary name. In `.github/workflows/ci.yml` nothing but the module name should need changing; check with `grep -n workflow .github/workflows/*.yml .goreleaser.yaml Makefile`.

- [ ] **Step 6: Stub README**

Replace `README.md` with a title, one paragraph, and "Docs: see docs/superpowers/specs/...". Full README comes in Task 9.

- [ ] **Step 7: Verify build and tests**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: transcript tests pass (`TestParsePSLine` will be deleted in Task 4; for now it still compiles because the function is still there).

- [ ] **Step 8: Commit**

```bash
git add -A && git commit -m "strip orchestrator; rename module to agentbar"
```

---

### Task 2: State files

**Files:**
- Create: `internal/state/state.go`, `internal/state/state_test.go`

**Interfaces:**
- Produces:
  ```go
  const (StatusNeedsYou = "needs-you"; StatusWorking = "working"; StatusWaiting = "waiting"; StatusError = "error"; StatusUnknown = "unknown")
  type Record struct { SessionID, CWD, Status, Detail, Tool, Event string; UpdatedAt time.Time }
  func Write(dir string, r Record) error          // atomic: tmp + rename; creates dir
  func Read(dir, sessionID string) (Record, bool)
  func ReadAll(dir string) map[string]Record       // keyed by SessionID; unreadable files skipped
  func Delete(dir, sessionID string) error         // missing file is not an error
  func Sweep(dir string, live map[string]bool, olderThan time.Duration, now time.Time) int
  ```

- [ ] **Step 1: Failing tests**

```go
package state

import (
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

func TestReadAllSkipsGarbage(t *testing.T) {
	dir := t.TempDir()
	_ = Write(dir, Record{SessionID: "one", Status: StatusWorking, UpdatedAt: time.Now()})
	_ = os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{nope"), 0o644)
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
}
```

- [ ] **Step 2: Run, expect compile failure**

Run: `go test ./internal/state/`

- [ ] **Step 3: Implement**

```go
// Package state stores one small JSON record per Claude session, written by
// the hook receiver and read by the sidebar.
package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	StatusNeedsYou = "needs-you"
	StatusWorking  = "working"
	StatusWaiting  = "waiting"
	StatusError    = "error"
	StatusUnknown  = "unknown"
)

type Record struct {
	SessionID string    `json:"session_id"`
	CWD       string    `json:"cwd"`
	Status    string    `json:"status"`
	Detail    string    `json:"detail,omitempty"`
	Tool      string    `json:"tool,omitempty"`
	Event     string    `json:"event"`
	UpdatedAt time.Time `json:"updated_at"`
}

func path(dir, id string) string { return filepath.Join(dir, safe(id)+".json") }

// safe keeps ids filesystem-friendly; session ids are UUIDs but never trust input.
func safe(id string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == 0 {
			return '_'
		}
		return r
	}, id)
}

func Write(dir string, r Record) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path(dir, r.SessionID))
}

func Read(dir, id string) (Record, bool) {
	b, err := os.ReadFile(path(dir, id))
	if err != nil {
		return Record{}, false
	}
	var r Record
	if json.Unmarshal(b, &r) != nil || r.SessionID == "" {
		return Record{}, false
	}
	return r, true
}

func ReadAll(dir string) map[string]Record {
	out := map[string]Record{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") || strings.HasPrefix(name, ".") {
			continue
		}
		if r, ok := Read(dir, strings.TrimSuffix(name, ".json")); ok {
			out[r.SessionID] = r
		}
	}
	return out
}

func Delete(dir, id string) error {
	err := os.Remove(path(dir, id))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Sweep removes records for sessions not in live whose UpdatedAt is older
// than olderThan. Returns the number removed.
func Sweep(dir string, live map[string]bool, olderThan time.Duration, now time.Time) int {
	n := 0
	for id, r := range ReadAll(dir) {
		if live[id] || now.Sub(r.UpdatedAt) < olderThan {
			continue
		}
		if Delete(dir, id) == nil {
			n++
		}
	}
	return n
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/state/ -v`
Expected: 3 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/state && git commit -m "state: per-session JSON records with sweep"
```

---

### Task 3: Hook payloads, mapping, and settings merge

**Files:**
- Create: `internal/hooks/payload.go`, `internal/hooks/mapping.go`, `internal/hooks/settings.go`, `internal/hooks/hooks_test.go`, `internal/cli/event.go`
- Modify: `cmd/agentbar/main.go` (add `event` case)

**Interfaces:**
- Consumes: `state.Write/Delete`, `paths.StateDir/LogPath`.
- Produces:
  ```go
  type Payload struct { SessionID, CWD, HookEventName, Message, ToolName, NotificationType, PermissionMode string; ToolInput map[string]any }
  func Parse(r io.Reader) (Payload, error)
  func Summarize(tool string, input map[string]any) string      // Bash→command, Edit/Write/Read→basename(file_path), else first string value; ≤80 runes
  type Outcome struct { Status, Detail, Tool string; Delete bool; Ignore bool }
  func Map(p Payload, statusOverride string) Outcome
  func Merge(settings []byte, exe string) ([]byte, bool, error)   // returns new JSON, changed
  func Remove(settings []byte, exe string) ([]byte, bool, error)
  ```

- [ ] **Step 1: Failing tests**

```go
package hooks

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ssnxd/agentbar/internal/state"
)

func TestMapTable(t *testing.T) {
	cases := []struct {
		event, override, tool string
		input                 map[string]any
		wantStatus, wantDetail string
		wantDelete            bool
	}{
		{event: "SessionStart", wantStatus: state.StatusWorking},
		{event: "UserPromptSubmit", wantStatus: state.StatusWorking},
		{event: "PreToolUse", tool: "Bash", input: map[string]any{"command": "ls"}, wantStatus: state.StatusWorking, wantDetail: "Bash ls"},
		{event: "PermissionRequest", tool: "Bash", input: map[string]any{"command": "git push origin main"}, wantStatus: state.StatusNeedsYou, wantDetail: "Bash git push origin main"},
		{event: "Notification", override: state.StatusNeedsYou, wantStatus: state.StatusNeedsYou},
		{event: "Notification", override: state.StatusWaiting, wantStatus: state.StatusWaiting},
		{event: "Stop", wantStatus: state.StatusWaiting},
		{event: "StopFailure", wantStatus: state.StatusError},
		{event: "SessionEnd", wantDelete: true},
	}
	for _, c := range cases {
		o := Map(Payload{HookEventName: c.event, ToolName: c.tool, ToolInput: c.input}, c.override)
		if o.Delete != c.wantDelete || o.Status != c.wantStatus || (c.wantDetail != "" && o.Detail != c.wantDetail) {
			t.Errorf("%s/%s: got %+v", c.event, c.override, o)
		}
	}
	if o := Map(Payload{HookEventName: "PostToolUse"}, ""); !o.Ignore {
		t.Error("unknown events must be ignored, not mapped")
	}
}

func TestSummarize(t *testing.T) {
	long := strings.Repeat("x", 200)
	if got := Summarize("Bash", map[string]any{"command": long}); len([]rune(got)) > 80+len("Bash ") {
		t.Errorf("not truncated: %d", len(got))
	}
	if got := Summarize("Edit", map[string]any{"file_path": "/a/b/c.go"}); got != "Edit c.go" {
		t.Errorf("edit: %q", got)
	}
	if got := Summarize("WebFetch", map[string]any{"url": "https://x"}); got != "WebFetch https://x" {
		t.Errorf("fallback: %q", got)
	}
	if got := Summarize("Foo", nil); got != "Foo" {
		t.Errorf("nil input: %q", got)
	}
}

func TestParsePayload(t *testing.T) {
	in := `{"session_id":"s1","cwd":"/p","hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"ls"},"permission_mode":"default"}`
	p, err := Parse(strings.NewReader(in))
	if err != nil || p.SessionID != "s1" || p.ToolName != "Bash" || p.ToolInput["command"] != "ls" {
		t.Fatalf("%+v %v", p, err)
	}
}

const userSettings = `{
  "model": "claude-fable-5-1[1m]",
  "hooks": {
    "Stop": [{"hooks": [{"type": "command", "command": "osascript -e 'display notification'", "timeout": 10}]}]
  },
  "theme": "auto"
}`

func TestMergeIsIdempotentAndPreservesForeignHooks(t *testing.T) {
	out, changed, err := Merge([]byte(userSettings), "/usr/local/bin/agentbar")
	if err != nil || !changed {
		t.Fatal(err, changed)
	}
	var s map[string]any
	if err := json.Unmarshal(out, &s); err != nil {
		t.Fatal(err)
	}
	if s["model"] != "claude-fable-5-1[1m]" || s["theme"] != "auto" {
		t.Error("foreign top-level keys lost")
	}
	hooks := s["hooks"].(map[string]any)
	stop := hooks["Stop"].([]any)
	if len(stop) != 2 {
		t.Fatalf("Stop should have osascript + agentbar, got %d", len(stop))
	}
	for _, ev := range []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PermissionRequest", "Notification", "Stop", "StopFailure", "SessionEnd"} {
		if _, ok := hooks[ev]; !ok {
			t.Errorf("missing %s", ev)
		}
	}
	if len(hooks["Notification"].([]any)) != 2 {
		t.Error("Notification needs two matcher groups")
	}
	again, changed2, _ := Merge(out, "/usr/local/bin/agentbar")
	if changed2 || string(again) != string(out) {
		t.Error("second merge must be a no-op")
	}
	removed, changed3, _ := Remove(again, "/usr/local/bin/agentbar")
	if !changed3 {
		t.Fatal("remove should change")
	}
	var r map[string]any
	_ = json.Unmarshal(removed, &r)
	rh := r["hooks"].(map[string]any)
	if len(rh["Stop"].([]any)) != 1 {
		t.Error("osascript hook must survive removal")
	}
	if _, ok := rh["SessionStart"]; ok {
		t.Error("agentbar-only events must be removed entirely")
	}
}
```

- [ ] **Step 2: Run, expect failure**

Run: `go test ./internal/hooks/`

- [ ] **Step 3: Implement `payload.go`**

```go
package hooks

import (
	"encoding/json"
	"io"
	"path/filepath"
	"sort"
	"strings"
)

type Payload struct {
	SessionID        string         `json:"session_id"`
	CWD              string         `json:"cwd"`
	HookEventName    string         `json:"hook_event_name"`
	Message          string         `json:"message"`
	ToolName         string         `json:"tool_name"`
	ToolInput        map[string]any `json:"tool_input"`
	NotificationType string         `json:"notification_type"`
	PermissionMode   string         `json:"permission_mode"`
}

func Parse(r io.Reader) (Payload, error) {
	var p Payload
	dec := json.NewDecoder(io.LimitReader(r, 1<<20))
	return p, dec.Decode(&p)
}

const maxDetail = 80

// Summarize renders "Tool <what>" for the sidebar's detail line.
func Summarize(tool string, input map[string]any) string {
	what := ""
	switch tool {
	case "Bash":
		what, _ = input["command"].(string)
	case "Edit", "Write", "Read", "MultiEdit", "NotebookEdit":
		if fp, ok := input["file_path"].(string); ok {
			what = filepath.Base(fp)
		}
	}
	if what == "" && input != nil {
		keys := make([]string, 0, len(input))
		for k := range input {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if s, ok := input[k].(string); ok && s != "" {
				what = s
				break
			}
		}
	}
	what = strings.Join(strings.Fields(what), " ")
	if r := []rune(what); len(r) > maxDetail {
		what = string(r[:maxDetail-1]) + "…"
	}
	if what == "" {
		return tool
	}
	return tool + " " + what
}
```

- [ ] **Step 4: Implement `mapping.go`**

```go
package hooks

import "github.com/ssnxd/agentbar/internal/state"

type Outcome struct {
	Status string
	Detail string
	Tool   string
	Delete bool
	Ignore bool
}

// Map turns a hook event into a state change. statusOverride comes from the
// --status flag that matcher-scoped Notification hooks pass explicitly.
func Map(p Payload, statusOverride string) Outcome {
	o := Outcome{Tool: p.ToolName}
	if p.ToolName != "" {
		o.Detail = Summarize(p.ToolName, p.ToolInput)
	}
	if statusOverride != "" {
		o.Status = statusOverride
		if o.Detail == "" {
			o.Detail = p.Message
		}
		return o
	}
	switch p.HookEventName {
	case "SessionStart", "UserPromptSubmit", "PreToolUse":
		o.Status = state.StatusWorking
	case "PermissionRequest":
		o.Status = state.StatusNeedsYou
	case "Stop":
		o.Status = state.StatusWaiting
	case "StopFailure":
		o.Status = state.StatusError
		o.Detail = p.Message
	case "SessionEnd":
		o.Delete = true
	default:
		o.Ignore = true
	}
	return o
}
```

- [ ] **Step 5: Implement `settings.go`**

Marker: every agentbar hook command starts with `<exe> event`. Merge and Remove identify ours by that prefix so a path change (new exe) is handled by `Remove(old)` then `Merge(new)`; `install` does exactly that when it finds entries from a different exe path.

```go
package hooks

import (
	"bytes"
	"encoding/json"
	"strings"
)

type hookCmd struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Async   bool   `json:"async,omitempty"`
	Timeout int    `json:"timeout,omitempty"`
}

type hookGroup struct {
	Matcher string    `json:"matcher,omitempty"`
	Hooks   []hookCmd `json:"hooks"`
}

// Entries returns the hook groups agentbar installs, keyed by event.
func Entries(exe string) map[string][]hookGroup {
	ev := func(status string, async bool) []hookGroup {
		cmd := exe + " event"
		if status != "" {
			cmd += " --status " + status
		}
		return []hookGroup{{Hooks: []hookCmd{{Type: "command", Command: cmd, Async: async, Timeout: 5}}}}
	}
	return map[string][]hookGroup{
		"SessionStart":      ev("", false),
		"UserPromptSubmit":  ev("", false),
		"PreToolUse":        ev("", true),
		"PermissionRequest": ev("", false),
		"Stop":              ev("", false),
		"StopFailure":       ev("", false),
		"SessionEnd":        ev("", false),
		"Notification": {
			{Matcher: "permission_prompt|elicitation_dialog|agent_needs_input",
				Hooks: []hookCmd{{Type: "command", Command: exe + " event --status needs-you", Timeout: 5}}},
			{Matcher: "idle_prompt",
				Hooks: []hookCmd{{Type: "command", Command: exe + " event --status waiting", Timeout: 5}}},
		},
	}
}

func isOurs(group any, exe string) bool {
	g, _ := group.(map[string]any)
	hs, _ := g["hooks"].([]any)
	for _, h := range hs {
		hm, _ := h.(map[string]any)
		if c, _ := hm["command"].(string); strings.HasPrefix(c, exe+" event") {
			return true
		}
	}
	return false
}

func toAny(v any) any {
	b, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(b, &out)
	return out
}

// Merge adds agentbar's hook groups to settings JSON, preserving everything
// else. Groups whose command already matches are not duplicated.
func Merge(settings []byte, exe string) ([]byte, bool, error) {
	var root map[string]any
	if len(bytes.TrimSpace(settings)) == 0 {
		root = map[string]any{}
	} else if err := json.Unmarshal(settings, &root); err != nil {
		return nil, false, err
	}
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	changed := false
	for event, groups := range Entries(exe) {
		existing, _ := hooks[event].([]any)
		for _, g := range groups {
			want := toAny(g)
			dup := false
			for _, e := range existing {
				if jsonEqual(e, want) {
					dup = true
					break
				}
			}
			if !dup {
				existing = append(existing, want)
				changed = true
			}
		}
		hooks[event] = existing
	}
	root["hooks"] = hooks
	if !changed {
		return settings, false, nil
	}
	out, err := marshal(root)
	return out, true, err
}

// Remove strips every hook group that belongs to exe.
func Remove(settings []byte, exe string) ([]byte, bool, error) {
	var root map[string]any
	if err := json.Unmarshal(settings, &root); err != nil {
		return nil, false, err
	}
	hooks, _ := root["hooks"].(map[string]any)
	changed := false
	for event, v := range hooks {
		groups, _ := v.([]any)
		kept := groups[:0:0]
		for _, g := range groups {
			if isOurs(g, exe) {
				changed = true
				continue
			}
			kept = append(kept, g)
		}
		if len(kept) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = kept
		}
	}
	if !changed {
		return settings, false, nil
	}
	if len(hooks) == 0 {
		delete(root, "hooks")
	}
	out, err := marshal(root)
	return out, true, err
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	err := enc.Encode(v)
	return buf.Bytes(), err
}
```

Note: `Remove` on a settings file where `hooks[event]` is `[]any` of `map[string]any` works because `json.Unmarshal` into `any` produces exactly those types.

- [ ] **Step 6: Run tests**

Run: `go test ./internal/hooks/ -v`
Expected: 4 PASS.

- [ ] **Step 7: Write `internal/cli/event.go`**

```go
package cli

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/ssnxd/agentbar/internal/hooks"
	"github.com/ssnxd/agentbar/internal/paths"
	"github.com/ssnxd/agentbar/internal/state"
)

// Event is the hook receiver. It must never fail loudly: any problem is
// logged and the process exits 0 so Claude Code is never blocked.
func Event(args []string) {
	fs := flag.NewFlagSet("event", flag.ContinueOnError)
	status := fs.String("status", "", "status override (from Notification matchers)")
	if err := fs.Parse(args); err != nil {
		logf("event: bad args: %v", err)
		return
	}
	p, err := hooks.Parse(os.Stdin)
	if err != nil {
		logf("event: parse: %v", err)
		return
	}
	if p.SessionID == "" {
		logf("event: %s without session_id", p.HookEventName)
		return
	}
	o := hooks.Map(p, *status)
	dir := paths.StateDir()
	switch {
	case o.Ignore:
		return
	case o.Delete:
		if err := state.Delete(dir, p.SessionID); err != nil {
			logf("event: delete %s: %v", p.SessionID, err)
		}
	default:
		err := state.Write(dir, state.Record{
			SessionID: p.SessionID, CWD: p.CWD, Status: o.Status, Detail: o.Detail,
			Tool: o.Tool, Event: p.HookEventName, UpdatedAt: time.Now(),
		})
		if err != nil {
			logf("event: write %s: %v", p.SessionID, err)
		}
	}
}

func logf(format string, a ...any) {
	f, err := os.OpenFile(paths.LogPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, time.Now().Format(time.RFC3339)+" "+format+"\n", a...)
}
```

Add to `main.go`: `case "event": cli.Event(args)`. Ensure `paths.DataDir()` exists before logging: `os.MkdirAll(paths.DataDir(), 0o755)` at the top of `Event`.

- [ ] **Step 8: Manual check**

```bash
go build -o /tmp/agentbar-test ./cmd/agentbar
echo '{"session_id":"t1","cwd":"/tmp","hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"git push"}}' | AGENTBAR_STATE_DIR=/tmp/ab-state /tmp/agentbar-test event; echo "exit=$?"; cat /tmp/ab-state/state/t1.json
echo '{"session_id":"t1","hook_event_name":"SessionEnd"}' | AGENTBAR_STATE_DIR=/tmp/ab-state /tmp/agentbar-test event; ls /tmp/ab-state/state/
```
Expected: exit=0, JSON with `"status":"needs-you"`, then an empty dir.

- [ ] **Step 9: Commit**

```bash
git add internal/hooks internal/cli cmd && git commit -m "hooks: payload mapping, settings merge, event receiver"
```

---

### Task 4: Registry reader and transcript parser

**Files:**
- Create: `internal/registry/registry.go`, `internal/registry/registry_test.go`, `internal/registry/testdata/11477.json`
- Modify: `internal/transcript/transcript.go` (remove ps/lsof/tmux code; keep tail parser; add first prompt, cache, Newest), `internal/transcript/transcript_test.go`

**Interfaces:**
- Produces:
  ```go
  // registry
  type Entry struct { PID int; SessionID, CWD, Name, Kind, Status, WaitingFor string; StartedAt, StatusUpdatedAt time.Time; TmuxSession, TmuxWindowID, TmuxPaneID string }
  func Load(dir string, alive func(pid int) bool) []Entry     // sorted by StartedAt
  func ParseTmux(s string) (session, windowID, paneID string, ok bool)   // "miivo:@10.%12"
  func Alive(pid int) bool
  // transcript
  type Info struct { Title, FirstPrompt, Model, Branch, PermissionMode string; ContextTokens int64; LastActivity time.Time; TurnOpen bool; Known bool }
  func Newest(projectsDir, cwd, sessionID string) string   // path or ""
  func Tail(path string) Info                                // cached by (size, mtime)
  func MungeProjectDir(cwd string) string
  ```

- [ ] **Step 1: Registry fixture and tests**

`testdata/11477.json` is the real file observed on this machine (copy verbatim, it is not secret):

```json
{"pid":11477,"sessionId":"7da9487e-b329-4e2f-9b9a-6e7420ee2c90","cwd":"/Users/miivo/code/miivo","startedAt":1788502479886,"procStart":"Fri Sep  4 06:14:39 2026","version":"2.1.260","peerProtocol":1,"peerFeatures":["notify_idle"],"kind":"interactive","entrypoint":"cli","pidDomain":"darwin","tmux":"miivo:@0.%0","messagingSocketPath":"/tmp/cc-socks/11477.sock","name":"miivo-ef","nameSource":"derived","nameSince":1788502479886,"status":"busy","updatedAt":1788853774910,"statusUpdatedAt":1788853774910,"bridgeSessionId":"session_x"}
```

```go
package registry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRealShape(t *testing.T) {
	dir := t.TempDir()
	src, _ := os.ReadFile("testdata/11477.json")
	_ = os.WriteFile(filepath.Join(dir, "11477.json"), src, 0o644)
	_ = os.WriteFile(filepath.Join(dir, "11477.abc.key"), []byte("k"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "99.json"), []byte(`{"pid":99,"sessionId":"dead","cwd":"/x","kind":"interactive"}`), 0o644)
	alive := func(pid int) bool { return pid == 11477 }
	es := Load(dir, alive)
	if len(es) != 1 {
		t.Fatalf("want 1 live entry, got %d", len(es))
	}
	e := es[0]
	if e.PID != 11477 || e.SessionID != "7da9487e-b329-4e2f-9b9a-6e7420ee2c90" || e.Name != "miivo-ef" || e.Kind != "interactive" {
		t.Errorf("%+v", e)
	}
	if e.TmuxSession != "miivo" || e.TmuxWindowID != "@0" || e.TmuxPaneID != "%0" {
		t.Errorf("tmux: %+v", e)
	}
	if e.StartedAt.Year() != 2026 || e.StatusUpdatedAt.IsZero() {
		t.Errorf("times: %+v", e)
	}
}

func TestParseTmux(t *testing.T) {
	s, w, p, ok := ParseTmux("miivo:@10.%12")
	if !ok || s != "miivo" || w != "@10" || p != "%12" {
		t.Errorf("%q %q %q %v", s, w, p, ok)
	}
	if _, _, _, ok := ParseTmux(""); ok {
		t.Error("empty must fail")
	}
	if _, _, _, ok := ParseTmux("garbage"); ok {
		t.Error("garbage must fail")
	}
}
```

- [ ] **Step 2: Implement registry**

```go
// Package registry reads Claude Code's own session registry:
// ~/.claude/sessions/<pid>.json. Undocumented; shape observed on 2.1.263.
package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
)

type Entry struct {
	PID             int
	SessionID       string
	CWD             string
	Name            string
	Kind            string
	Status          string // busy | idle | waiting | shell (Claude's own, not trusted for UI)
	WaitingFor      string
	StartedAt       time.Time
	StatusUpdatedAt time.Time
	TmuxSession     string
	TmuxWindowID    string
	TmuxPaneID      string
}

type raw struct {
	PID             int    `json:"pid"`
	SessionID       string `json:"sessionId"`
	CWD             string `json:"cwd"`
	Name            string `json:"name"`
	Kind            string `json:"kind"`
	Status          string `json:"status"`
	WaitingFor      string `json:"waitingFor"`
	StartedAt       int64  `json:"startedAt"`
	StatusUpdatedAt int64  `json:"statusUpdatedAt"`
	Tmux            string `json:"tmux"`
}

var tmuxRe = regexp.MustCompile(`^(.+):(@\d+)\.(%\d+)$`)

func ParseTmux(s string) (session, windowID, paneID string, ok bool) {
	m := tmuxRe.FindStringSubmatch(s)
	if m == nil {
		return "", "", "", false
	}
	return m[1], m[2], m[3], true
}

func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

func ms(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.UnixMilli(v)
}

func Load(dir string, alive func(int) bool) []Entry {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Entry
	for _, de := range ents {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, de.Name()))
		if err != nil {
			continue
		}
		var r raw
		if json.Unmarshal(b, &r) != nil || r.SessionID == "" || !alive(r.PID) {
			continue
		}
		e := Entry{PID: r.PID, SessionID: r.SessionID, CWD: r.CWD, Name: r.Name, Kind: r.Kind,
			Status: r.Status, WaitingFor: r.WaitingFor, StartedAt: ms(r.StartedAt), StatusUpdatedAt: ms(r.StatusUpdatedAt)}
		e.TmuxSession, e.TmuxWindowID, e.TmuxPaneID, _ = ParseTmux(r.Tmux)
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out
}
```

- [ ] **Step 3: Transcript tests**

Replace `transcript_test.go`:

```go
package transcript

import (
	"os"
	"path/filepath"
	"testing"
)

const sample = `{"type":"user","message":{"role":"user","content":"fix the auth bug"},"gitBranch":"fix/auth","permissionMode":"default"}
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
	if i.Title != "Fix auth middleware" || i.FirstPrompt != "fix the auth bug" || i.Model != "claude-fable-5-1" || i.Branch != "fix/auth" || i.ContextTokens != 1000 {
		t.Errorf("%+v", i)
	}
	if i.TurnOpen {
		t.Error("turn_duration closes the turn")
	}
	open := sample + `{"type":"assistant","message":{"model":"m"}}` + "\n"
	_ = os.WriteFile(p, []byte(open), 0o644)
	if !Tail(p).TurnOpen {
		t.Error("assistant line after turn_duration reopens the turn")
	}
}

func TestNewestPrefersSessionFile(t *testing.T) {
	projects := t.TempDir()
	cwd := "/Users/x/code/app"
	dir := filepath.Join(projects, MungeProjectDir(cwd))
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "other.jsonl"), []byte("{}\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "sid.jsonl"), []byte("{}\n"), 0o644)
	if got := Newest(projects, cwd, "sid"); filepath.Base(got) != "sid.jsonl" {
		t.Errorf("got %q", got)
	}
	if got := Newest(projects, cwd, "missing"); got == "" {
		t.Error("should fall back to newest file")
	}
	if got := Newest(projects, "/nope", ""); got != "" {
		t.Error("missing dir must yield empty")
	}
}

func TestMungeProjectDir(t *testing.T) {
	if got := MungeProjectDir("/Users/x/code/my.app"); got != "-Users-x-code-my-app" {
		t.Errorf("munge = %q", got)
	}
}
```

- [ ] **Step 4: Rewrite `transcript.go`**

Keep `parseTranscriptTail` logic and `MungeProjectDir`; delete `Discover`, `listClaudeProcs`, `ParsePSLine`, `cwdByPID`, `userPanesByTTY`, `EnvWithoutTmux`. New shape:

```go
// Package transcript reads the tail of a Claude Code transcript (.jsonl)
// for display metadata: title, model, branch, context size, and whether a
// turn is open. Transcripts can be tens of MB; only the last 64KB is read.
package transcript

type Info struct {
	Title, FirstPrompt, Model, Branch, PermissionMode string
	ContextTokens                                    int64
	LastActivity                                     time.Time
	TurnOpen                                         bool // last substantive line was not turn_duration
	Known                                            bool // file existed and parsed
}

// Newest returns the transcript for (cwd, sessionID): <sessionID>.jsonl when
// present, otherwise the most recently modified .jsonl in the project dir.
func Newest(projectsDir, cwd, sessionID string) string { ... }

var cache = struct{ sync.Mutex; m map[string]cached }{m: map[string]cached{}}
type cached struct{ size int64; mod time.Time; info Info }

// Tail parses the last 64KB of path; results are cached by size+mtime.
func Tail(path string) Info { ... }
```

In the line loop add: `Message.Content` as `json.RawMessage`; when `d.Type == "user"` and `FirstPrompt == ""` and the content unmarshals as a string, set `FirstPrompt` (collapse whitespace, cut at 80 runes). Only the first user line in the tail is used; that is fine because the tail is a display hint. `TurnOpen` = `lastKind == "turn_open"`. `LastActivity` = file mtime.

- [ ] **Step 5: Run**

Run: `go test ./internal/registry/ ./internal/transcript/ -v`
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/registry internal/transcript && git commit -m "registry reader and transcript tail parser"
```

---

### Task 5: Session merge

**Files:**
- Create: `internal/session/session.go`, `internal/session/session_test.go`

**Interfaces:**
- Consumes: `registry.Entry`, `state.Record`, `transcript.Info`.
- Produces:
  ```go
  type Session struct {
      ID, PID-as-int, CWD, Project, Name, Title, Model, Branch string; PID int; ContextTokens int64
      Status, Detail, Source string   // Source: hook | registry | transcript | none
      StartedAt, LastActivity time.Time
      InTmux bool; TmuxSession, TmuxWindowID, TmuxPaneID, TmuxTarget string  // TmuxTarget: "work:2.1" display, filled by tmuxctl later
  }
  type Deps struct { Registry []registry.Entry; States map[string]state.Record; Transcript func(cwd, sessionID string) transcript.Info; Now time.Time }
  func Build(d Deps) []Session
  func Resolve(e registry.Entry, rec state.Record, hasRec bool, ti transcript.Info, now time.Time) (status, detail, source string)
  ```

- [ ] **Step 1: Tests**

```go
package session

import (
	"testing"
	"time"

	"github.com/ssnxd/agentbar/internal/registry"
	"github.com/ssnxd/agentbar/internal/state"
	"github.com/ssnxd/agentbar/internal/transcript"
)

func TestResolvePriority(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	started := now.Add(-time.Hour)
	e := registry.Entry{SessionID: "s", StartedAt: started}
	fresh := state.Record{SessionID: "s", Status: state.StatusNeedsYou, Detail: "Bash git push", UpdatedAt: now.Add(-time.Minute)}
	stale := state.Record{SessionID: "s", Status: state.StatusWorking, UpdatedAt: started.Add(-time.Minute)} // from a previous process with same id (resume)
	cases := []struct {
		name          string
		rec           state.Record
		has           bool
		e             registry.Entry
		ti            transcript.Info
		wantStatus    string
		wantSource    string
	}{
		{"hook wins", fresh, true, e, transcript.Info{Known: true, TurnOpen: false}, state.StatusNeedsYou, "hook"},
		{"stale hook ignored, registry waiting", stale, true, registry.Entry{SessionID: "s", StartedAt: started, Status: "waiting", WaitingFor: "permission prompt"}, transcript.Info{}, state.StatusNeedsYou, "registry"},
		{"transcript closed turn", state.Record{}, false, e, transcript.Info{Known: true, TurnOpen: false, LastActivity: now.Add(-time.Minute)}, state.StatusWaiting, "transcript"},
		{"transcript open recent", state.Record{}, false, e, transcript.Info{Known: true, TurnOpen: true, LastActivity: now.Add(-5 * time.Second)}, state.StatusWorking, "transcript"},
		{"transcript open silent", state.Record{}, false, e, transcript.Info{Known: true, TurnOpen: true, LastActivity: now.Add(-2 * time.Minute)}, state.StatusNeedsYou, "transcript"},
		{"nothing", state.Record{}, false, e, transcript.Info{}, state.StatusUnknown, "none"},
	}
	for _, c := range cases {
		st, _, src := Resolve(c.e, c.rec, c.has, c.ti, now)
		if st != c.wantStatus || src != c.wantSource {
			t.Errorf("%s: got %s/%s want %s/%s", c.name, st, src, c.wantStatus, c.wantSource)
		}
	}
}

func TestBuildProjectNamesDisambiguate(t *testing.T) {
	now := time.Now()
	d := Deps{
		Registry: []registry.Entry{
			{SessionID: "a", CWD: "/u/code/app", StartedAt: now, TmuxSession: "w", TmuxWindowID: "@1", TmuxPaneID: "%1"},
			{SessionID: "b", CWD: "/u/other/app", StartedAt: now},
		},
		States:     map[string]state.Record{},
		Transcript: func(string, string) transcript.Info { return transcript.Info{} },
		Now:        now,
	}
	ss := Build(d)
	if len(ss) != 2 {
		t.Fatal(len(ss))
	}
	if ss[0].Project != "code/app" || ss[1].Project != "other/app" {
		t.Errorf("projects: %q %q", ss[0].Project, ss[1].Project)
	}
	if !ss[0].InTmux || ss[1].InTmux {
		t.Error("InTmux wrong")
	}
}
```

- [ ] **Step 2: Implement**

```go
// Package session merges Claude's registry, agentbar's hook state, and the
// transcript tail into the rows the sidebar shows.
package session

const (
	SourceHook       = "hook"
	SourceRegistry   = "registry"
	SourceTranscript = "transcript"
	SourceNone       = "none"
)

const workingWindow = 30 * time.Second

func Resolve(e registry.Entry, rec state.Record, hasRec bool, ti transcript.Info, now time.Time) (status, detail, source string) {
	if hasRec && rec.UpdatedAt.After(e.StartedAt) {
		return rec.Status, rec.Detail, SourceHook
	}
	if e.Status == "waiting" && e.WaitingFor != "" {
		return state.StatusNeedsYou, e.WaitingFor, SourceRegistry
	}
	if ti.Known {
		switch {
		case !ti.TurnOpen:
			return state.StatusWaiting, "", SourceTranscript
		case now.Sub(ti.LastActivity) < workingWindow:
			return state.StatusWorking, "", SourceTranscript
		default:
			return state.StatusNeedsYou, "probably a prompt (no hook data)", SourceTranscript
		}
	}
	return state.StatusUnknown, "", SourceNone
}

func Build(d Deps) []Session {
	// 1. one Session per registry entry (Title: ti.Title → ti.FirstPrompt → e.Name)
	// 2. project names: basename; where two differ in CWD but share a basename, use parent/base
	// 3. LastActivity = max(ti.LastActivity, rec.UpdatedAt, e.StartedAt)
}
```

Sorting is the UI's job (Task 7); `Build` returns registry order.

- [ ] **Step 3: Run, commit**

Run: `go test ./internal/session/ -v` → PASS.

```bash
git add internal/session && git commit -m "session: merge registry, hook state, transcript"
```

---

### Task 6: tmux control and toggle

**Files:**
- Create: `internal/tmuxctl/tmuxctl.go`, `internal/tmuxctl/toggle.go`, `internal/tmuxctl/tmuxctl_test.go`, `internal/cli/toggle.go`
- Modify: `cmd/agentbar/main.go` (`toggle`, `jump` cases)

**Interfaces:**
- Produces:
  ```go
  type Runner interface { Run(args ...string) (string, error) }
  type Exec struct{}                    // real tmux, env without TMUX/TMUX_PANE
  type Pane struct { SessionName, WindowID, WindowIndex, PaneID, PaneIndex, Command string; Active, WindowActive bool; Sidebar bool }
  func ListPanes(r Runner) ([]Pane, error)          // -a, format includes #{@agentbar}
  func Current(r Runner) (paneID, windowID string, err error)   // display-message -p
  type Opts struct { Side string; Width int; Cmd string }
  type Action int  // Open, Close, Focus, Move
  type Situation struct { Sidebar *Pane; CurrentPane, CurrentWindow string }
  func Decide(s Situation) Action
  func Toggle(r Runner, o Opts) error
  func Jump(r Runner, o Opts, targetPane string) error
  func SendEnter(r Runner, pane string) error
  func TargetLabel(p Pane) string   // "work:2.1"
  ```

- [ ] **Step 1: Tests with a fake runner**

```go
package tmuxctl

import (
	"strings"
	"testing"
)

type fake struct {
	panes string
	cur   string
	calls []string
}

func (f *fake) Run(args ...string) (string, error) {
	f.calls = append(f.calls, strings.Join(args, " "))
	switch args[0] {
	case "list-panes":
		return f.panes, nil
	case "display-message":
		return f.cur, nil
	}
	return "", nil
}

const panes = "work|@1|2|%5|1|zsh|1|1|\nwork|@1|2|%6|2|agentbar|0|1|1\nwork|@2|3|%7|1|claude|1|0|\n"

func TestDecide(t *testing.T) {
	sb := Pane{WindowID: "@1", PaneID: "%6", Sidebar: true}
	cases := []struct {
		name string
		s    Situation
		want Action
	}{
		{"none → open", Situation{CurrentPane: "%5", CurrentWindow: "@1"}, ActionOpen},
		{"here focused → close", Situation{Sidebar: &sb, CurrentPane: "%6", CurrentWindow: "@1"}, ActionClose},
		{"here unfocused → focus", Situation{Sidebar: &sb, CurrentPane: "%5", CurrentWindow: "@1"}, ActionFocus},
		{"elsewhere → move", Situation{Sidebar: &sb, CurrentPane: "%7", CurrentWindow: "@2"}, ActionMove},
	}
	for _, c := range cases {
		if got := Decide(c.s); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestToggleOpensWithTagAndStyle(t *testing.T) {
	f := &fake{panes: "work|@1|2|%5|1|zsh|1|1|\n", cur: "%5 @1"}
	if err := Toggle(f, Opts{Side: "left", Width: 42, Cmd: "/bin/agentbar"}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(f.calls, "\n")
	for _, want := range []string{"split-window -hb -l 42 -t %5 -P -F #{pane_id}", "set-option -p -t", "@agentbar 1"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in\n%s", want, joined)
		}
	}
}

func TestJumpMovesSidebarThenSelects(t *testing.T) {
	f := &fake{panes: panes, cur: "%6 @1"}
	if err := Jump(f, Opts{Side: "left", Width: 42}, "%7"); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(f.calls, "\n")
	if !strings.Contains(joined, "join-pane -hb -l 42 -s %6 -t %7") || !strings.Contains(joined, "select-pane -t %7") {
		t.Errorf("calls:\n%s", joined)
	}
}

func TestListPanesParsesSidebarFlag(t *testing.T) {
	ps, err := ListPanes(&fake{panes: panes})
	if err != nil || len(ps) != 3 {
		t.Fatal(err, len(ps))
	}
	if !ps[1].Sidebar || ps[0].Sidebar || TargetLabel(ps[2]) != "work:3.1" {
		t.Errorf("%+v", ps)
	}
}
```

- [ ] **Step 2: Implement `tmuxctl.go`**

Format string for list-panes:
`#{session_name}|#{window_id}|#{window_index}|#{pane_id}|#{pane_index}|#{pane_current_command}|#{pane_active}|#{window_active}|#{@agentbar}`

`Exec.Run` uses `exec.Command("tmux", args...)` with env minus TMUX/TMUX_PANE, returns trimmed stdout, error includes stderr.

`Current`: `display-message -p "#{pane_id} #{window_id}"` (this needs the client context, so for `Current` only, keep `TMUX` in env: implement `Exec` with a field `KeepClient bool`, or simply pass `-t` from `os.Getenv("TMUX_PANE")` when set: `display-message -p -t $TMUX_PANE ...`). Use the `-t $TMUX_PANE` approach; `toggle` runs from `run-shell`, which sets `TMUX_PANE` to the pane the key was pressed in.

`Toggle`:
- Open: `split-window -h[b] -l W -t <cur> -P -F #{pane_id} -- <cmd>` (b only for left), then `set-option -p -t <new> @agentbar 1`, then `select-pane -t <new> -P 'bg=default'` is not enough for the dimming problem; instead read the user's active style: `show-options -gv window-active-style` and if non-empty `select-pane -t <new> -P <that>`.
- Close: `kill-pane -t <sidebar>`.
- Focus: `select-pane -t <sidebar>`.
- Move: `join-pane -h[b] -l W -s <sidebar> -t <cur>` then `select-pane -t <sidebar>`.

`Jump(r, o, target)`: find sidebar; if it exists and is not in target's window, `join-pane -h[b] -l W -s <sb> -t <target>`; then `select-window -t <target's window>` and `select-pane -t <target>`. (`select-pane -t %id` alone also switches the window in tmux ≥ 3.x, but be explicit.)

`SendEnter(r, pane)`: `send-keys -t <pane> Enter`.

- [ ] **Step 3: Implement `internal/cli/toggle.go`**

```go
func Toggle() {
	if os.Getenv("TMUX") == "" && os.Getenv("TMUX_PANE") == "" {
		fmt.Fprintln(os.Stderr, "agentbar toggle: run this from inside tmux")
		os.Exit(1)
	}
	o := optsFromTmux() // show-options -gqv @agentbar-side / @agentbar-width; defaults left / 42
	exe, _ := os.Executable()
	o.Cmd = exe + " ui"
	if err := tmuxctl.Toggle(tmuxctl.Exec{}, o); err != nil { fmt.Fprintln(os.Stderr, "agentbar:", err); os.Exit(1) }
}

func Jump(args []string) { /* same opts; tmuxctl.Jump(Exec{}, o, args[0]) */ }
```

- [ ] **Step 4: Run tests, then a live smoke test**

Run: `go test ./internal/tmuxctl/ -v` → PASS.

Live:
```bash
go build -o ~/.local/bin/agentbar ./cmd/agentbar
tmux run-shell "~/.local/bin/agentbar toggle"   # from inside tmux; expect a left pane running `agentbar ui`, which at this point prints usage error — fine
tmux list-panes -F '#{pane_id} #{@agentbar} #{pane_current_command}'
tmux run-shell "~/.local/bin/agentbar toggle"   # focus (pressed from the other pane) or close
```
Expected: the pane appears with `@agentbar 1`; second toggle from that pane closes it.

- [ ] **Step 5: Commit**

```bash
git add internal/tmuxctl internal/cli cmd && git commit -m "tmuxctl: tagged sidebar pane, toggle, jump"
```

---

### Task 7: Sidebar UI

**Files:**
- Create: `internal/ui/styles.go`, `internal/ui/render.go`, `internal/ui/model.go`, `internal/ui/render_test.go`
- Modify: `cmd/agentbar/main.go` (`ui` default case)

**Interfaces:**
- Consumes: `session.Build`, `state.ReadAll/Sweep`, `registry.Load/Alive`, `transcript.Newest/Tail`, `tmuxctl.ListPanes/Jump/SendEnter/TargetLabel`, `paths.*`.
- Produces: `ui.Run()`; pure helpers `Group(ss []session.Session) []Group`, `Render(m Model) string` for tests.

- [ ] **Step 1: styles.go**

Catppuccin Mocha tokens (from the user's tmux config): base `#1e1e2e`, mantle `#181825`, surface0 `#313244`, surface1 `#45475a`, overlay1 `#7f849c`, subtext0 `#a6adc8`, text `#cdd6f4`, blue `#89b4fa`, sky `#89dceb`, green `#a6e3a1`, yellow `#f9e2af`, peach `#fab387`, red `#f38ba8`, mauve `#cba6f7`.

```go
var statusLooks = map[string]statusLook{
	state.StatusNeedsYou: {"●", "needs you", bold peach},
	state.StatusWorking:  {"◐", "working", blue},      // frames ◐ ◑
	state.StatusWaiting:  {"○", "waiting", green},
	state.StatusError:    {"✗", "error", red},
	state.StatusUnknown:  {"◌", "unknown", overlay1},
}
```

- [ ] **Step 2: render_test.go (snapshot)**

```go
func TestRenderGrouped(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	m := Model{width: 42, height: 20, now: now, sessions: []session.Session{
		{ID: "1", Project: "miivo-api", Title: "fix auth middleware", Status: state.StatusNeedsYou, Detail: "Bash git push origin main", TmuxTarget: "work:2.1", InTmux: true, Model: "claude-fable-5-1", Branch: "fix/auth", LastActivity: now.Add(-12 * time.Minute)},
		{ID: "2", Project: "miivo-api", Title: "add rate limiter", Status: state.StatusWorking, TmuxTarget: "work:3.1", InTmux: true, Model: "claude-opus-5", Branch: "main", LastActivity: now.Add(-3 * time.Minute)},
		{ID: "3", Project: "dotfiles", Title: "ghostty theme", Status: state.StatusWaiting, Model: "claude-sonnet-5", Branch: "main", LastActivity: now.Add(-40 * time.Minute)},
	}}
	m.selected = "1"
	out := ansi.Strip(Render(m))
	for _, want := range []string{"3 · 1 needs you", "miivo-api", "● needs you  fix auth middleware", "work:2.1 · fable · fix/auth", "12m", "permission: Bash git push origin main", "dotfiles", "not in tmux", "enter jump", "y accept"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if lipgloss.Width(line) > 42 {
			t.Errorf("line too wide: %q", line)
		}
	}
}

func TestOrdering(t *testing.T) {
	ss := []session.Session{
		{ID: "w", Project: "b", Status: state.StatusWaiting},
		{ID: "n", Project: "a", Status: state.StatusNeedsYou},
		{ID: "k", Project: "a", Status: state.StatusWorking},
	}
	gs := Group(ss)
	if gs[0].Name != "a" || gs[0].Rows[0].ID != "n" || gs[0].Rows[1].ID != "k" || gs[1].Name != "b" {
		t.Errorf("%+v", gs)
	}
}
```

Use `github.com/charmbracelet/x/ansi` (already an indirect dep) for `ansi.Strip`.

- [ ] **Step 3: render.go**

- `shortModel("claude-fable-5-1") == "fable"`: strip `claude-` prefix, take the first token before `-`.
- `age(d)`: `<1m` → `now`, minutes `12m`, hours `3h`, days `2d`.
- `Group`: rank map needs-you=0, error=1, waiting=2, working=3, unknown=4; sort rows by rank then LastActivity desc; groups by min rank then name.
- Row rendering at width W: line 1 = ` ` + icon + ` ` + label padded to 9 + ` ` + title truncated; line 2 = 4 spaces + `target · model · branch` truncated to leave room for the age right-aligned; line 3 (needs-you) = 4 spaces + `permission: ` + detail truncated. Selected row: whole lines rendered with `Background(surface0)`, padded to W.
- Header: ` claude ` + rule + ` N · K needs you ` fitted to W; when K == 0 just ` N sessions`.
- Footer: context hints joined by two spaces; when `helpOpen` render the full table above the footer.
- Kill-confirm: footer becomes `kill <title>? y/n`.
- Filter mode: footer becomes `/ <text>` and rows are filtered by substring over project, title, branch, status label.
- Height: header, blank, groups; if content exceeds height, scroll so the selected row is visible (keep a `scroll` offset in Model).

- [ ] **Step 4: model.go**

```go
type Model struct {
	sessions []session.Session
	panes    map[string]tmuxctl.Pane // by pane id
	selected string
	scroll   int
	filter   string; filtering bool
	helpOpen bool
	confirmKill bool
	msg string; msgUntil time.Time
	width, height, tick int
	now time.Time
	err error
}
type tickMsg struct{}; type snapshotMsg struct{ sessions []session.Session; panes map[string]tmuxctl.Pane; err error }; type actionMsg struct{ text string; err error }
```

`Init`: `tea.Batch(refresh(), tick())`. `refresh()` runs off-thread: `registry.Load(paths.SessionsDir(), registry.Alive)`, `state.ReadAll(paths.StateDir())`, `state.Sweep(...)`, `tmuxctl.ListPanes`, `session.Build`, then fills `TmuxTarget` via `TargetLabel(panes[s.TmuxPaneID])` and clears `InTmux` when the pane no longer exists.

Keys (`tea.KeyPressMsg`, `msg.String()`): `j`/`down`, `k`/`up`, `enter`, `tab`, `y`, `x`, `/`, `esc`, `r`, `?`, `q`, `ctrl+c`. In filter mode: printable runes append, `backspace` deletes, `enter`/`esc` exits filter mode (enter keeps filter, esc clears).

Actions return `tea.Cmd`s that call `tmuxctl.Jump`, `tmuxctl.SendEnter` (guard: status needs-you and `panes[pane].Command` looks like claude: equals `claude` or matches `^\d+\.\d+\.\d+$`, which is how Claude 2.1.x names its process), `syscall.Kill(pid, SIGTERM)`. Results come back as `actionMsg` shown in the footer for 3s.

`View`: `tea.NewView(Render(m))` with `AltScreen = true`, `WindowTitle = "agentbar"`.

`Run()`: `tea.NewProgram(Model{}).Run()`.

- [ ] **Step 5: Tests and live run**

Run: `go test ./internal/ui/ -v` → PASS.
Live: `make install && tmux run-shell "$HOME/.local/bin/agentbar toggle"` from inside tmux. Expected: sidebar lists the currently running sessions with correct `session:window.pane` labels. Screenshot via `tmux capture-pane -p -t <sidebar pane>` and paste in the report.

- [ ] **Step 6: Commit**

```bash
git add internal/ui cmd && git commit -m "ui: grouped sidebar with jump, accept, kill, filter"
```

---

### Task 8: install, doctor, plugin script

**Files:**
- Create: `internal/cli/install.go`, `internal/cli/doctor.go`, `agentbar.tmux`
- Modify: `cmd/agentbar/main.go`

- [ ] **Step 1: install.go**

```go
func Install(args []string) {
	uninstall := len(args) > 0 && args[0] == "--uninstall"
	exe, _ := os.Executable(); exe, _ = filepath.EvalSymlinks(exe)
	p := paths.SettingsPath()
	cur, err := os.ReadFile(p)   // ENOENT → empty
	// If the file contains "agentbar event" with a different exe path, Remove that path first (find via regexp `(\S+/agentbar) event`).
	var out []byte; var changed bool
	if uninstall { out, changed, err = hooks.Remove(cur, exe) } else { out, changed, err = hooks.Merge(cur, exe) }
	if changed { backup := p + ".bak." + time.Now().Format("20060102-150405"); os.WriteFile(backup, cur, 0o600); os.WriteFile(p, out, 0o600); fmt.Println("updated", p, "(backup:", backup+")") } else { fmt.Println("no change to", p) }
	if !uninstall { fmt.Printf("\nAdd to your tmux config and reload:\n  run-shell %s\n", filepath.Join(repoDir(exe), "agentbar.tmux")) }
}
```

`repoDir` can't be known from the binary; print the generic form: `run-shell /path/to/agentbar/agentbar.tmux` and alternatively `bind a run-shell "<exe> toggle"`.

- [ ] **Step 2: doctor.go**

Checks, each printing `ok`/`FAIL` with a hint: tmux present and `tmux -V` ≥ 3.2; exe absolute and executable; settings contain `<exe> event`; `paths.SessionsDir()` readable; number of live registry entries. Exit 1 if any FAIL.

- [ ] **Step 3: agentbar.tmux**

```bash
#!/usr/bin/env bash
# agentbar tmux plugin entry. TPM-compatible; also fine via `run-shell` directly.
# Options (set -g in tmux.conf before this runs):
#   @agentbar-key    key after prefix (default: a)
#   @agentbar-bin    path to the agentbar binary (default: agentbar on PATH, then ~/.local/bin/agentbar)
#   @agentbar-side   left | right (default: left)
#   @agentbar-width  columns (default: 42)
set -eu
get() { tmux show-option -gqv "$1"; }
key="$(get @agentbar-key)"; key="${key:-a}"
bin="$(get @agentbar-bin)"
if [ -z "$bin" ]; then bin="$(command -v agentbar || true)"; fi
if [ -z "$bin" ] && [ -x "$HOME/.local/bin/agentbar" ]; then bin="$HOME/.local/bin/agentbar"; fi
if [ -z "$bin" ]; then tmux display-message "agentbar: binary not found (set @agentbar-bin)"; exit 0; fi
tmux bind-key "$key" run-shell "$bin toggle"
```

`chmod +x agentbar.tmux`.

- [ ] **Step 4: Wire main.go, run doctor**

Run: `make install && agentbar doctor`. Expected output shows tmux ok, hooks FAIL (not installed yet).

- [ ] **Step 5: Commit**

```bash
git add internal/cli cmd agentbar.tmux && git commit -m "install, doctor, tmux plugin script"
```

---

### Task 9: README, changelog, CI

**Files:**
- Modify: `README.md`, `CHANGELOG.md` (new), `.github/workflows/ci.yml`, `.goreleaser.yaml`

- [ ] **Step 1: README** — sections: what it is (with the ASCII mockup), requirements, install (`go install github.com/ssnxd/agentbar/cmd/agentbar@latest` or `make install`), setup (`agentbar install`, tmux line, reload), keys table, options table, how status is derived (hooks; registry; transcript fallback), privacy (`AGENTBAR_STATE_DIR`), troubleshooting (`agentbar doctor`, log path).
- [ ] **Step 2: CHANGELOG** — `## 0.1.0 (unreleased)` listing the rewrite.
- [ ] **Step 3: CI** — confirm `make ci` passes locally: `make ci`.
- [ ] **Step 4: Commit** — `git commit -m "docs: README and changelog for agentbar"`.

---

### Task 10: Live installation and end-to-end verification

- [ ] **Step 1:** `make install && agentbar install` → show output and `diff` of settings backup vs new.
- [ ] **Step 2:** append `run-shell /Users/miivo/code/agentbar/agentbar.tmux` to `~/config/tmux/tmux.conf` (this file is a dotfiles symlink target; the repo path is where the worktree will be merged; until merge use the worktree path and note it) and `tmux source-file ~/.tmux.conf`.
- [ ] **Step 3:** `agentbar doctor` → all ok.
- [ ] **Step 4:** Press `prefix a` (simulate: `tmux send-keys -t <a pane> C-a a`); `tmux capture-pane -p` on the sidebar; verify the four live sessions appear with correct targets (`tmux list-panes -a` to compare).
- [ ] **Step 5:** Jump: send `Enter` to the sidebar; verify `tmux display-message -p '#{window_id} #{pane_id}'` equals the target and the sidebar pane now lives in that window.
- [ ] **Step 6:** Permission flow: in a scratch window, `cd $(mktemp -d) && claude -p` is non-interactive, so instead start interactive `claude` in a new tmux window with a prompt that runs a Bash command outside the allowlist, e.g. type `run: touch /tmp/agentbar-e2e` ; wait for the permission prompt; capture the sidebar and confirm the needs-you row with `Bash touch …`; press `y` in the sidebar; confirm the row turns working then waiting, and `/tmp/agentbar-e2e` exists. Kill the scratch session with `x` and confirm.
- [ ] **Step 7:** `prefix a` from another pane focuses; again closes. Capture both.
- [ ] **Step 8:** Commit anything that changed during verification; report with the captured outputs.

---

## Self-review

- Spec coverage: hotkey table → Task 6; layout/keys → Task 7; hook table and settings merge → Task 3; registry/transcript/merge → Tasks 4–5; sweep → Task 2 + Task 7 refresh; plugin/options → Task 8; install/doctor → Task 8; error handling (hook exit 0, degrade rows, footer errors) → Tasks 3, 5, 7; testing list → each task; live E2E → Task 10; repo strip and module rename → Task 1.
- Names are consistent: `state.Status*`, `hooks.Map/Merge/Remove/Summarize/Parse`, `registry.Load/ParseTmux/Alive`, `transcript.Newest/Tail/Info`, `session.Build/Resolve/Deps`, `tmuxctl.Toggle/Jump/ListPanes/SendEnter/TargetLabel/Decide`, `ui.Run/Render/Group`.
- Known judgment calls left to the implementer: exact lipgloss styling values, scroll math, the regex for Claude's process name.
