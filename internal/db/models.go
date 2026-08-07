package db

// Agent status values. Transitions come from Claude Code hooks (primary) and
// tmux polling (fallback: dead panes).
const (
	StatusStarting = "starting"  // spawned, no hook event yet
	StatusWorking  = "working"   // processing a turn / running tools
	StatusIdle     = "idle"      // finished a turn, waiting for input
	StatusNeedsYou = "needs_you" // blocked on a permission or a question
	StatusError    = "error"     // turn failed (rate limit, billing, API)
	StatusDone     = "done"      // agent reported its assignment complete
	StatusEnded    = "ended"     // claude session exited
	StatusDead     = "dead"      // tmux pane/session is gone or pane died
)

const (
	RoleOrchestrator = "orchestrator"
	RoleWorker       = "worker"
)

type Project struct {
	ID   int64
	Name string
	Path string
}

type Task struct {
	ID              int64
	ProjectID       int64
	Title           string
	Prompt          string
	Status          string // active | done | archived
	Summary         string
	Branch          string
	TmuxSessionID   string
	TmuxSessionName string
	CreatedAt       string

	// Joined for display.
	ProjectName string
	ProjectPath string
}

type Agent struct {
	ID              int64
	TaskID          int64
	Name            string
	Role            string
	Model           string
	ClaudeSessionID string
	TmuxWindowID    string
	TmuxPaneID      string
	WorktreePath    string
	Branch          string
	Status          string
	Summary         string
	CostUSD         float64
	ContextPct      float64
	LinesAdded      int64
	LinesRemoved    int64
	UpdatedAt       string
}
