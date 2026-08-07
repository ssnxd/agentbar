package manager

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// gitRun executes git in dir, returning combined trimmed output on error.
func gitRun(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	if err != nil {
		return strings.TrimSpace(out.String()),
			fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

// Land merges a task branch into the branch currently checked out in the
// user's main checkout. Preconditions guard the user's repo: clean tree,
// not detached. On conflict the merge is aborted and the error surfaced —
// the checkout is never left mid-merge.
func (m *Manager) Land(taskID int64, squash bool) (string, error) {
	task, err := m.Store.GetTask(taskID)
	if err != nil {
		return "", err
	}
	repo := task.ProjectPath

	dirty, err := gitRun(repo, "status", "--porcelain")
	if err != nil {
		return "", err
	}
	if dirty != "" {
		return "", fmt.Errorf("your checkout at %s has uncommitted changes — commit or stash before landing", repo)
	}
	target, err := gitRun(repo, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	if target == "HEAD" {
		return "", fmt.Errorf("your checkout is on a detached HEAD — check out the branch you want to land into")
	}
	if target == task.Branch {
		return "", fmt.Errorf("your checkout is on the task branch itself — check out the branch you want to land into")
	}

	// Optional pre-land gate (tests, lint) from config; abort on failure.
	if hook := strings.TrimSpace(m.Cfg.PreLand); hook != "" {
		cmd := exec.Command("sh", "-c", hook)
		cmd.Dir = repo
		var errb bytes.Buffer
		cmd.Stderr = &errb
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("pre_land hook failed: %s", strings.TrimSpace(errb.String()))
		}
	}

	if squash {
		if _, err := gitRun(repo, "merge", "--squash", task.Branch); err != nil {
			_, _ = gitRun(repo, "merge", "--abort")
			_, _ = gitRun(repo, "reset", "--merge")
			return "", fmt.Errorf("squash merge into %s hit conflicts — resolve manually or ask the orchestrator to rebase %s: %w", target, task.Branch, err)
		}
		msg := fmt.Sprintf("%s (workflow task #%d)", task.Title, task.ID)
		if _, err := gitRun(repo, "commit", "-m", msg); err != nil {
			return "", err
		}
	} else {
		msg := fmt.Sprintf("Merge %s: %s (workflow task #%d)", task.Branch, task.Title, task.ID)
		if _, err := gitRun(repo, "merge", "--no-ff", "-m", msg, task.Branch); err != nil {
			_, _ = gitRun(repo, "merge", "--abort")
			return "", fmt.Errorf("merge into %s hit conflicts — resolve manually or ask the orchestrator to rebase %s: %w", target, task.Branch, err)
		}
	}
	_ = m.Store.InsertEvent(0, "", "task_landed", fmt.Sprintf("task %d -> %s", taskID, target))
	return target, nil
}

// CreatePR pushes the task branch and opens a GitHub PR with the
// orchestrator's summary as the body. Requires gh and a GitHub remote.
func (m *Manager) CreatePR(taskID int64) (string, error) {
	task, err := m.Store.GetTask(taskID)
	if err != nil {
		return "", err
	}
	repo := task.ProjectPath
	if _, err := exec.LookPath("gh"); err != nil {
		return "", fmt.Errorf("gh CLI not found — install it or land locally with merge")
	}
	if _, err := gitRun(repo, "push", "-u", "origin", task.Branch); err != nil {
		return "", fmt.Errorf("push %s: %w", task.Branch, err)
	}

	body := task.Summary
	if body == "" {
		body = task.Prompt
	}
	agents, err := m.Store.ListAgents(taskID)
	if err == nil && len(agents) > 1 {
		var b strings.Builder
		b.WriteString(body + "\n\n### Agents\n")
		for _, a := range agents {
			if a.Role == "worker" {
				fmt.Fprintf(&b, "- **%s** (%s): %s\n", a.Name, a.Model, a.Summary)
			}
		}
		body = b.String()
	}

	cmd := exec.Command("gh", "pr", "create",
		"--head", task.Branch, "--title", task.Title, "--body", body)
	cmd.Dir = repo
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("gh pr create: %s", strings.TrimSpace(errb.String()))
	}
	url := strings.TrimSpace(out.String())
	_ = m.Store.InsertEvent(0, "", "task_pr_created", url)
	return url, nil
}
