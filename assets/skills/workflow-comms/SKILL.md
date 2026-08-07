---
name: workflow-comms
description: How agents in a workflow task communicate, report status, and use their worktree. Use when you need to message the orchestrator or another agent, check your inbox, or finish your assignment.
---

# Workflow agent protocol

You run inside **workflow**, a manager that runs each agent in its own tmux window. Your identity (agent name, task ID, role) is in your system prompt and in the environment variables `WORKFLOW_AGENT`, `WORKFLOW_TASK`, and `WORKFLOW_ROLE`.

## Rules

1. Work only inside your own worktree (your current directory). Do not touch other agents' worktrees or the main checkout.
2. Commit your work to your own branch with clear messages. Commit before you report completion.
3. Do not create tmux panes or windows. Do not run `tmux` commands.
4. If you are blocked on a decision only a human can make, say so clearly in your final message and stop. The manager surfaces your idle state to the human.
5. Run `workflow` and `git` commands standalone, one per Bash call. Do not chain them with `;`, `&&`, or pipes into other commands — chained commands lose their pre-approval and force a human permission stop.

## Messaging

Send a message to another agent in your task (the orchestrator is named `orchestrator`):

```bash
workflow msg send --to orchestrator "Done with the API layer. Two tests still fail in auth_test.go."
```

Read your inbox (prints all unread messages and marks them read):

```bash
workflow msg read
```

Check your inbox when: you finish a unit of work, you are told "you have mail", or you are about to make a cross-cutting decision.

## Finishing

When your assignment is complete and committed:

```bash
workflow agent done --summary "One line: what you did and where it stands."
```

Then send the orchestrator a short completion message with anything it needs to merge your branch.
