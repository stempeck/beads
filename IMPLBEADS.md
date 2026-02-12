# IMPLBEADS.md

This document describes how Gas Town uses the beads (`bd`) CLI for issue tracking, work coordination, and agent lifecycle management.

## Beads Architecture Overview

Gas Town uses a **two-level beads architecture**:

1. **Town-level beads** (`~/gt/.beads/`) - Global coordination with `hq-*` prefix
   - Mail messages between agents
   - Convoys (batched work tracking)
   - Town-wide agent beads (Mayor, Deacon, Dogs)
   - Role definition beads

2. **Rig-level beads** (`<rig>/mayor/rig/.beads/`) - Per-project issues with configurable prefix (e.g., `gt-*`, `bd-*`)
   - Project issues (tasks, bugs, features, epics)
   - Merge requests
   - Rig-level agent beads (Witness, Refinery, Polecats, Crew)

## How Gastown Uses Beads - Process Flow

When working with a rig, the beads process typically follows these steps:

### Initial Setup

1. `gt rig add myproject <repo-url>` creates the rig structure including `.beads/` directory
2. `bd init --prefix <prefix>` initializes the beads database with a project-specific prefix (e.g., `gt`, `mp`)
3. The rig's `.beads/` directory contains a `redirect` file pointing to `mayor/rig/.beads/` (the canonical beads location)
4. Polecats, crew, and refinery worktrees also get `.beads/redirect` files pointing to the rig's canonical beads

### Issue Creation and Management

5. `bd create --type task --title "Implement feature X"` creates a new issue
6. The issue gets an auto-generated ID with the rig's prefix (e.g., `gt-abc`, `mp-123`)
7. Issues can have parent/child relationships, dependencies, and blockers
8. `bd ready` lists issues that are unblocked and ready for work
9. `bd list --status open` shows all open issues

### Work Assignment (Slinging)

10. `gt sling gt-abc gastown` spawns a polecat and assigns the issue
11. Internally, gastown runs `bd slot set <agent-bead-id> hook <issue-id>` to "hook" the work to the agent
12. The agent bead's `hook_bead` field tracks what work is currently assigned
13. Agent beads use the format `<prefix>-<rig>-<role>-<name>` (e.g., `gt-gastown-polecat-Toast`)

### Agent Bead Lifecycle

14. When a polecat is spawned, `bd create --type agent` creates an agent bead
15. Agent beads track: `role_type`, `rig`, `agent_state`, `hook_bead`, `role_bead`, `cleanup_status`
16. `bd agent state <bead-id> <state>` updates the agent's state (spawning, working, done, stuck)
17. `bd slot set <agent-bead-id> hook <work-bead-id>` attaches work to an agent
18. `bd slot clear <agent-bead-id> hook` releases work when complete

### Work Execution

19. Polecats run `gt prime` which reads their hooked bead via `bd show <bead-id>`
20. The bead's title, description, and any attached formula provide the work context
21. Progress is tracked by updating issue status: `bd update <id> --status in_progress`
22. Molecules (multi-step workflows) use `bd slot` commands to track step completion

### Work Completion

23. Polecat runs `gt done` when work is complete
24. This creates a merge-request bead: `bd create --type merge-request`
25. The MR bead tracks: branch, target, source issue, priority
26. Issue is closed: `bd close <issue-id> --session=<session-id>` (for work attribution)
27. Agent's hook is cleared: `bd slot clear <agent-bead-id> hook`
28. Agent bead is deleted: `bd delete <agent-bead-id> --hard --force`

### Convoy Tracking

29. `gt convoy create "Feature batch" gt-abc gt-def` creates a convoy bead
30. Convoys track batched work across multiple issues
31. `gt convoy status <convoy-id>` shows progress of all tracked issues
32. Convoys auto-close when all tracked issues land

### Redirect System

33. Worktrees (polecats, crew, refinery) don't have their own beads databases
34. Each worktree has a `.beads/redirect` file containing a relative path (e.g., `../../mayor/rig/.beads`)
35. The `beads.ResolveBeadsDir()` function follows redirects to find the canonical beads
36. This ensures all agents share the same beads database

## Key Beads Commands Used by Gastown

| Command | Purpose |
|---------|---------|
| `bd create` | Create issues, agent beads, MR beads |
| `bd show <id>` | Get detailed issue info (JSON output) |
| `bd list` | List issues with filters |
| `bd ready` | List unblocked issues ready for work |
| `bd update` | Update issue status, assignee, labels |
| `bd close` | Close completed issues |
| `bd delete` | Delete agent beads on cleanup |
| `bd slot set/clear/get` | Manage agent-work attachments |
| `bd agent state` | Update agent lifecycle state |
| `bd dep add/remove` | Manage issue dependencies |
| `bd sync` | Sync beads with remote (git-backed) |
| `bd config get/set` | Manage beads configuration |

## Bead Types

| Type | Purpose | Example ID |
|------|---------|------------|
| `task` | Discrete work unit | `gt-abc` |
| `bug` | Bug report | `gt-def` |
| `feature` | Feature request | `gt-ghi` |
| `epic` | Large work container | `gt-jkl` |
| `agent` | Agent lifecycle tracking | `gt-gastown-polecat-Toast` |
| `role` | Role definition template | `hq-polecat-role` |
| `merge-request` | MR in merge queue | `gt-mr-xyz` |
| `rig` | Rig identity bead | `gt-rig-gastown` |
| `convoy` | Batched work tracking | `hq-cv-abc` |

## Beads Database Files

The `.beads/` directory contains:

- `*.db` - SQLite database (runtime state)
- `issues.jsonl` - Git-tracked issue log (source of truth for sync)
- `interactions.jsonl` - Git-tracked interaction log
- `config.yaml` - Beads configuration (prefix, sync settings)
- `redirect` - Path to canonical beads (in worktrees)
- `mq/` - Merge queue events directory

## Attribution and Provenance

Every beads operation includes attribution:

- `BD_ACTOR` environment variable identifies who performed the action
- `--actor` flag on create/update commands
- `--session` flag on close commands links to Claude session ID
- `created_by` field on issues tracks provenance
- All changes are git-tracked for audit trail

## Common Beads Patterns in Gastown

### Hook-based Work Assignment
```
bd slot set gt-gastown-polecat-Toast hook gt-abc
# Work flows: Issue -> Agent's hook -> Agent executes
```

### Cascade Completion
```
bd close gt-child-1 gt-child-2 gt-child-3
bd close gt-parent  # Parent auto-closes when children done
```

### Redirect Chain
```
polecats/Toast/.beads/redirect -> "../../.beads"
.beads/redirect -> "mayor/rig/.beads"
# Final destination: mayor/rig/.beads (canonical)
```

### Agent Bead ID Format
```
<prefix>-<role>                    # Town-level (hq-mayor)
<prefix>-<rig>-<role>              # Rig-level singleton (gt-gastown-witness)
<prefix>-<rig>-<role>-<name>       # Named agent (gt-gastown-polecat-Toast)
```
