# PRD: Claude Code Task Tracking Synchronization with Beads

**Version:** 1.0
**Status:** Draft
**Author:** Claude
**Date:** 2026-01-25

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Problem Statement](#2-problem-statement)
3. [Goals and Non-Goals](#3-goals-and-non-goals)
4. [Background Research](#4-background-research)
5. [Proposed Solution](#5-proposed-solution)
6. [Technical Design](#6-technical-design)
7. [Data Model](#7-data-model)
8. [Hook Implementation](#8-hook-implementation)
9. [Sync Agent Architecture](#9-sync-agent-architecture)
10. [User Experience](#10-user-experience)
11. [Migration Strategy](#11-migration-strategy)
12. [Security Considerations](#12-security-considerations)
13. [Testing Strategy](#13-testing-strategy)
14. [Open Questions](#14-open-questions)
15. [Future Considerations](#15-future-considerations)
16. [Appendix](#appendix)

---

## 1. Executive Summary

This PRD proposes extending beads to capture and persist Claude Code's in-session task tracking data (TodoWrite tasks) alongside bead records. The feature enables visibility into real-time agent work, improves work attribution, and creates a bridge between ephemeral session tasks and persistent bead-based issue tracking.

**Key deliverable:** A `PostToolUse` hook that detects TodoWrite tool usage and synchronizes task state to beads via a background sub-agent, with minimal impact on session performance.

---

## 2. Problem Statement

### Current State

Today, beads and Claude Code have the following integration points:
- **SessionStart hook** runs `bd prime` to inject workflow context
- **PreCompact hook** refreshes context before compaction
- **ClosedBySession field** captures which session closed an issue
- **Plan-to-beads conversion** manually converts Claude Code plans to beads

### Gap

Claude Code's TodoWrite tool creates **ephemeral, session-scoped tasks** that:
- Are not persisted after session ends
- Are not visible to other sessions or team members
- Cannot be correlated with bead work items
- Provide no audit trail of agent planning/execution

This creates several problems:

1. **Visibility gap:** Humans and other agents cannot see what tasks a Claude Code session is currently working on
2. **Lost context:** When sessions end or crash, task state is lost
3. **No attribution:** We know a session closed a bead, but not what sub-tasks it performed
4. **No continuity:** Resumed sessions cannot pick up exactly where they left off
5. **Multi-agent coordination:** Parallel agents cannot see each other's active work

### User Stories

> "As a human supervisor, I want to see what tasks Claude Code is currently working on for a bead, so I can understand progress and intervene if needed."

> "As a Claude Code session resuming work, I want to see what tasks were in progress when the last session ended, so I can continue seamlessly."

> "As a system operator running multiple agents, I want visibility into all active tasks across sessions, so I can avoid duplicate work and detect stuck agents."

---

## 3. Goals and Non-Goals

### Goals

1. **Persist TodoWrite task state** linked to beads and sessions
2. **Real-time sync** via PostToolUse hook with minimal latency impact
3. **Background processing** to avoid blocking Claude Code sessions
4. **Bi-directional visibility:** tasks visible from bead, bead visible from tasks
5. **Support session continuity** for resumed sessions
6. **Enable multi-agent awareness** of active work

### Non-Goals (Out of Scope for V1)

1. **Modifying TodoWrite behavior** in Claude Code itself
2. **Two-way sync** (beads → TodoWrite) — V1 is one-way: TodoWrite → beads
3. **Task assignment/delegation** across agents
4. **Real-time push notifications** to other clients
5. **Automatic task-to-bead conversion** (e.g., auto-creating sub-beads)
6. **Historical task analytics/reporting**

---

## 4. Background Research

### 4.1 Beads Architecture

Beads uses a three-layer storage model:

```
┌─────────────────────────────────────────────────────────────────┐
│                        CLI Layer                                 │
│  bd create, list, update, close, ready, show, dep, sync, ...    │
└──────────────────────────────────────────────────────────────────┘
                               ↕
┌─────────────────────────────────────────────────────────────────┐
│                     SQLite Database (.beads/beads.db)           │
│  - Local working copy, fast queries, indexes                    │
└──────────────────────────────────────────────────────────────────┘
                               ↕
┌─────────────────────────────────────────────────────────────────┐
│                       JSONL File (.beads/issues.jsonl)          │
│  - Git-tracked, merge-friendly                                  │
└──────────────────────────────────────────────────────────────────┘
```

**Extension pattern:** The storage layer supports custom tables via `UnderlyingDB()`, allowing foreign-key relationships to beads without polluting the core schema.

### 4.2 Claude Code TodoWrite Tool

The TodoWrite tool manages session-scoped tasks:

```typescript
interface Todo {
  content: string;        // Imperative task description
  activeForm: string;     // Present continuous form (e.g., "Running tests")
  status: "pending" | "in_progress" | "completed";
}

// Tool invocation
{
  tool_name: "TodoWrite",
  input: {
    todos: Todo[]
  }
}
```

**Key characteristics:**
- Tasks exist only during session lifetime
- Complete task list is sent with each invocation (not differential)
- No persistent storage mechanism
- Session ID available via `CLAUDE_SESSION_ID` env var

### 4.3 Claude Code Hooks

Relevant hooks for this feature:

| Hook | Trigger | Use Case |
|------|---------|----------|
| `PostToolUse` | After any tool completes | Detect TodoWrite calls |
| `SessionStart` | Session begins | Initialize sync state |
| `SessionEnd` | Session ends | Final sync, mark orphaned tasks |
| `SubagentStop` | Sub-agent completes | Aggregate sub-agent tasks |

**Hook input (PostToolUse):**
```json
{
  "session_id": "abc123",
  "tool_name": "TodoWrite",
  "tool_input": { "todos": [...] },
  "tool_result": "...",
  "cwd": "/path/to/project"
}
```

### 4.4 Existing Session Integration

Beads already captures session IDs:

```go
// In cmd/bd/close.go
session := os.Getenv("CLAUDE_SESSION_ID")
updates["closed_by_session"] = session
```

The `ClosedBySession` field in the Issue struct:
```go
ClosedBySession string `json:"closed_by_session,omitempty"`
```

---

## 5. Proposed Solution

### Overview

Implement a three-component system:

1. **New `cc_tasks` table** in beads SQLite to store task state
2. **PostToolUse hook** to detect TodoWrite invocations
3. **Background sync agent** to update beads without blocking Claude Code

### Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         Claude Code Session                              │
│  ┌─────────────────────────────────────────────────────────────────┐   │
│  │ User Prompt → Agent Planning → TodoWrite() → Tool Result        │   │
│  └──────────────────────────────────────┬──────────────────────────┘   │
└─────────────────────────────────────────┼───────────────────────────────┘
                                          │ PostToolUse hook fires
                                          ↓
┌─────────────────────────────────────────────────────────────────────────┐
│                    bd tasks sync (hook handler)                          │
│  ┌─────────────────────────────────────────────────────────────────┐   │
│  │ 1. Parse TodoWrite input from stdin                             │   │
│  │ 2. Detect active bead (from session context or explicit link)   │   │
│  │ 3. Spawn/resume background sync agent                           │   │
│  └──────────────────────────────────────┬──────────────────────────┘   │
└─────────────────────────────────────────┼───────────────────────────────┘
                                          │ Async (non-blocking)
                                          ↓
┌─────────────────────────────────────────────────────────────────────────┐
│                    Background Sync Agent                                 │
│  ┌─────────────────────────────────────────────────────────────────┐   │
│  │ 1. Debounce rapid updates (500ms window)                        │   │
│  │ 2. Diff against last known state                                │   │
│  │ 3. Update cc_tasks table via RPC                                │   │
│  │ 4. Update session_task_lists table                              │   │
│  └──────────────────────────────────────┬──────────────────────────┘   │
└─────────────────────────────────────────┼───────────────────────────────┘
                                          ↓
┌─────────────────────────────────────────────────────────────────────────┐
│                         Beads Storage                                    │
│  ┌──────────────────────┐  ┌──────────────────────┐                    │
│  │ cc_tasks             │  │ session_task_lists   │                    │
│  │ - task_id (PK)       │  │ - session_id (PK)    │                    │
│  │ - session_id (FK)    │  │ - bead_id (FK)       │                    │
│  │ - task_list_id       │  │ - started_at         │                    │
│  │ - content            │  │ - last_sync_at       │                    │
│  │ - active_form        │  │ - state (json)       │                    │
│  │ - status             │  │                      │                    │
│  │ - ordinal            │  └──────────────────────┘                    │
│  │ - created_at         │                                              │
│  │ - updated_at         │                                              │
│  │ - completed_at       │                                              │
│  └──────────────────────┘                                              │
└─────────────────────────────────────────────────────────────────────────┘
```

### Key Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Storage location | Custom SQLite table via `UnderlyingDB()` | Follows established extension pattern, enables foreign keys |
| Sync mechanism | PostToolUse hook + background agent | Non-blocking, survives tool failures |
| Bead association | Explicit via `--bead` flag or inferred from session | Flexible for different workflows |
| Task identity | Stable ID based on content hash + ordinal | TodoWrite doesn't provide IDs; need stability for diffs |
| Git tracking | Optional (not in JSONL by default) | Tasks are high-frequency, low-value for git history |

---

## 6. Technical Design

### 6.1 New CLI Commands

#### `bd tasks sync`

Hook handler that receives PostToolUse input and triggers sync.

```bash
# Called by PostToolUse hook
bd tasks sync --session "$SESSION_ID" [--bead <bead-id>]

# Manual invocation
bd tasks sync --session abc123 --bead bd-f7k2
```

**Behavior:**
1. Read TodoWrite input from stdin (JSON)
2. Validate and parse task list
3. Spawn or signal background sync agent
4. Exit immediately (non-blocking)

#### `bd tasks list`

Query tasks for a session or bead.

```bash
# List tasks for a session
bd tasks list --session abc123

# List tasks for a bead (all sessions)
bd tasks list --bead bd-f7k2

# List active tasks across all sessions
bd tasks list --active

# JSON output
bd tasks list --session abc123 --json
```

**Output:**
```
Session: abc123 (linked to bd-f7k2: "Implement auth feature")
Last sync: 2 minutes ago

  ✓ Research existing auth code
  → Implementing JWT token validation
  ○ Adding refresh token logic
  ○ Writing tests

Legend: ✓ completed  → in_progress  ○ pending
```

#### `bd tasks link`

Explicitly link a session to a bead.

```bash
# Link current session to a bead
bd tasks link bd-f7k2

# Link specific session
bd tasks link --session abc123 bd-f7k2
```

#### `bd tasks unlink`

Remove session-bead association.

```bash
bd tasks unlink [--session <session-id>]
```

### 6.2 Hook Installation

Extend `bd setup claude` to install the PostToolUse hook:

```json
{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "TodoWrite",
        "hooks": [
          {
            "type": "command",
            "command": "bd tasks sync --session \"$CLAUDE_SESSION_ID\""
          }
        ]
      }
    ],
    "SessionStart": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "bd prime"
          }
        ]
      }
    ],
    "SessionEnd": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "bd tasks finalize --session \"$CLAUDE_SESSION_ID\""
          }
        ]
      }
    ]
  }
}
```

### 6.3 Background Agent Design

The sync agent runs as a detached process to avoid blocking:

```go
// internal/tasks/agent.go

type SyncAgent struct {
    sessionID    string
    beadID       string
    lastState    []Task
    debouncer    *time.Timer
    updates      chan []Task
}

func (a *SyncAgent) Run(ctx context.Context) error {
    for {
        select {
        case tasks := <-a.updates:
            // Reset debounce timer
            a.debouncer.Reset(500 * time.Millisecond)
            a.lastState = tasks

        case <-a.debouncer.C:
            // Debounce window passed, sync to storage
            if err := a.syncToStorage(ctx); err != nil {
                log.Errorf("sync failed: %v", err)
            }

        case <-ctx.Done():
            // Final sync on shutdown
            a.syncToStorage(context.Background())
            return nil
        }
    }
}
```

**Agent lifecycle:**
1. Started on first TodoWrite detection for a session
2. Receives updates via Unix domain socket or file-based IPC
3. Debounces rapid updates (500ms default)
4. Persists state to SQLite
5. Terminates on SessionEnd hook or timeout (30 min idle)

---

## 7. Data Model

### 7.1 New Tables

#### `session_task_lists`

Tracks Claude Code sessions and their bead associations.

```sql
CREATE TABLE session_task_lists (
    session_id TEXT PRIMARY KEY,
    bead_id TEXT,                          -- FK to issues.id (nullable)
    task_list_id TEXT,                     -- Claude Code task list ID if available
    started_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_sync_at TIMESTAMP,
    ended_at TIMESTAMP,                    -- Set by SessionEnd hook
    state TEXT,                            -- JSON: agent state, metadata

    FOREIGN KEY (bead_id) REFERENCES issues(id) ON DELETE SET NULL
);

CREATE INDEX idx_session_task_lists_bead ON session_task_lists(bead_id);
CREATE INDEX idx_session_task_lists_active ON session_task_lists(ended_at) WHERE ended_at IS NULL;
```

#### `cc_tasks`

Individual task records from TodoWrite.

```sql
CREATE TABLE cc_tasks (
    id TEXT PRIMARY KEY,                   -- Generated: hash(session_id + content + ordinal)
    session_id TEXT NOT NULL,              -- FK to session_task_lists
    ordinal INTEGER NOT NULL,              -- Position in task list
    content TEXT NOT NULL,                 -- Task description (imperative form)
    active_form TEXT,                      -- Present continuous form
    status TEXT NOT NULL DEFAULT 'pending', -- pending, in_progress, completed
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at TIMESTAMP,                -- Set when status → completed

    FOREIGN KEY (session_id) REFERENCES session_task_lists(session_id) ON DELETE CASCADE,
    UNIQUE(session_id, ordinal)
);

CREATE INDEX idx_cc_tasks_session ON cc_tasks(session_id);
CREATE INDEX idx_cc_tasks_status ON cc_tasks(status);
CREATE INDEX idx_cc_tasks_session_status ON cc_tasks(session_id, status);
```

### 7.2 Task Identity

TodoWrite doesn't provide stable task IDs. We generate them:

```go
func GenerateTaskID(sessionID string, content string, ordinal int) string {
    h := sha256.New()
    h.Write([]byte(sessionID))
    h.Write([]byte(content))
    h.Write([]byte(fmt.Sprintf("%d", ordinal)))
    return fmt.Sprintf("cct-%s", hex.EncodeToString(h.Sum(nil))[:8])
}
```

**Matching algorithm for diffs:**
1. Match by exact content + ordinal
2. Match by content with fuzzy ordinal (task reordering)
3. Treat remaining as new/deleted

### 7.3 Issue Struct Extension

Add field to reference active task session:

```go
// In internal/types/types.go

type Issue struct {
    // ... existing fields ...

    // Task tracking
    ActiveTaskSession string `json:"active_task_session,omitempty"` // Session actively working on this bead
}
```

### 7.4 JSONL Export (Optional)

Task data is high-frequency and typically not valuable for git history. By default, task tables are **not exported to JSONL**.

However, for audit/compliance scenarios, add optional export:

```bash
# Export task history for a bead
bd tasks export --bead bd-f7k2 > tasks.jsonl

# Include tasks in full export
bd export --include-tasks
```

---

## 8. Hook Implementation

### 8.1 PostToolUse Hook Handler

```go
// cmd/bd/tasks_sync.go

func runTasksSync(cmd *cobra.Command, args []string) error {
    // Read hook input from stdin
    input, err := io.ReadAll(os.Stdin)
    if err != nil {
        return fmt.Errorf("reading stdin: %w", err)
    }

    var hookInput struct {
        SessionID  string `json:"session_id"`
        ToolName   string `json:"tool_name"`
        ToolInput  struct {
            Todos []struct {
                Content    string `json:"content"`
                ActiveForm string `json:"activeForm"`
                Status     string `json:"status"`
            } `json:"todos"`
        } `json:"tool_input"`
    }

    if err := json.Unmarshal(input, &hookInput); err != nil {
        return fmt.Errorf("parsing hook input: %w", err)
    }

    // Validate tool name
    if hookInput.ToolName != "TodoWrite" {
        return nil // Not a TodoWrite call, ignore
    }

    sessionID := hookInput.SessionID
    if sessionID == "" {
        sessionID = os.Getenv("CLAUDE_SESSION_ID")
    }

    if sessionID == "" {
        return fmt.Errorf("no session ID available")
    }

    // Signal background agent (or spawn if not running)
    return signalSyncAgent(sessionID, hookInput.ToolInput.Todos)
}
```

### 8.2 Agent Signaling

Use file-based IPC for simplicity:

```go
// internal/tasks/ipc.go

func signalSyncAgent(sessionID string, todos []Task) error {
    // Write update to session-specific file
    updatePath := filepath.Join(os.TempDir(), "bd-tasks", sessionID, "pending.json")

    if err := os.MkdirAll(filepath.Dir(updatePath), 0700); err != nil {
        return err
    }

    data, err := json.Marshal(todos)
    if err != nil {
        return err
    }

    if err := os.WriteFile(updatePath, data, 0600); err != nil {
        return err
    }

    // Ensure agent is running
    return ensureAgentRunning(sessionID)
}

func ensureAgentRunning(sessionID string) error {
    pidPath := filepath.Join(os.TempDir(), "bd-tasks", sessionID, "agent.pid")

    // Check if agent is already running
    if pid, err := readPID(pidPath); err == nil {
        if process, err := os.FindProcess(pid); err == nil {
            if err := process.Signal(syscall.Signal(0)); err == nil {
                return nil // Agent is running
            }
        }
    }

    // Spawn new agent
    cmd := exec.Command("bd", "tasks", "agent", "--session", sessionID)
    cmd.Start()

    return writePID(pidPath, cmd.Process.Pid)
}
```

### 8.3 SessionEnd Handler

```go
// cmd/bd/tasks_finalize.go

func runTasksFinalize(cmd *cobra.Command, args []string) error {
    sessionID, _ := cmd.Flags().GetString("session")
    if sessionID == "" {
        sessionID = os.Getenv("CLAUDE_SESSION_ID")
    }

    store, err := getStore()
    if err != nil {
        return err
    }

    // Mark session as ended
    _, err = store.UnderlyingDB().Exec(`
        UPDATE session_task_lists
        SET ended_at = CURRENT_TIMESTAMP
        WHERE session_id = ?
    `, sessionID)
    if err != nil {
        return err
    }

    // Signal agent to shutdown
    return signalAgentShutdown(sessionID)
}
```

---

## 9. Sync Agent Architecture

### 9.1 Agent Process

```go
// cmd/bd/tasks_agent.go

func runTasksAgent(cmd *cobra.Command, args []string) error {
    sessionID, _ := cmd.Flags().GetString("session")

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    // Handle signals
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
    go func() {
        <-sigCh
        cancel()
    }()

    agent := &SyncAgent{
        sessionID:   sessionID,
        updateDir:   filepath.Join(os.TempDir(), "bd-tasks", sessionID),
        debounceMs:  500,
        idleTimeout: 30 * time.Minute,
    }

    return agent.Run(ctx)
}

func (a *SyncAgent) Run(ctx context.Context) error {
    store, err := getStore()
    if err != nil {
        return err
    }

    // Ensure session record exists
    if err := a.ensureSession(store); err != nil {
        return err
    }

    ticker := time.NewTicker(100 * time.Millisecond)
    defer ticker.Stop()

    idleTimer := time.NewTimer(a.idleTimeout)
    defer idleTimer.Stop()

    var pendingSync bool
    var debounceTimer *time.Timer

    for {
        select {
        case <-ctx.Done():
            // Final sync
            if pendingSync {
                a.performSync(store)
            }
            return nil

        case <-ticker.C:
            // Check for updates
            if a.hasUpdates() {
                pendingSync = true
                idleTimer.Reset(a.idleTimeout)

                if debounceTimer == nil {
                    debounceTimer = time.AfterFunc(
                        time.Duration(a.debounceMs)*time.Millisecond,
                        func() {
                            if err := a.performSync(store); err != nil {
                                log.Errorf("sync error: %v", err)
                            }
                            pendingSync = false
                            debounceTimer = nil
                        },
                    )
                }
            }

        case <-idleTimer.C:
            // Idle timeout - shutdown
            log.Infof("agent idle timeout, shutting down")
            return nil
        }
    }
}
```

### 9.2 Sync Algorithm

```go
func (a *SyncAgent) performSync(store storage.Store) error {
    // Read pending update
    updatePath := filepath.Join(a.updateDir, "pending.json")
    data, err := os.ReadFile(updatePath)
    if err != nil {
        return err
    }

    var newTasks []Task
    if err := json.Unmarshal(data, &newTasks); err != nil {
        return err
    }

    // Clear pending file
    os.Remove(updatePath)

    // Get current state from DB
    currentTasks, err := a.getCurrentTasks(store)
    if err != nil {
        return err
    }

    // Compute diff
    toInsert, toUpdate, toDelete := a.diffTasks(currentTasks, newTasks)

    // Apply changes in transaction
    db := store.UnderlyingDB()
    tx, err := db.Begin()
    if err != nil {
        return err
    }
    defer tx.Rollback()

    for _, task := range toInsert {
        _, err := tx.Exec(`
            INSERT INTO cc_tasks (id, session_id, ordinal, content, active_form, status)
            VALUES (?, ?, ?, ?, ?, ?)
        `, task.ID, a.sessionID, task.Ordinal, task.Content, task.ActiveForm, task.Status)
        if err != nil {
            return err
        }
    }

    for _, task := range toUpdate {
        _, err := tx.Exec(`
            UPDATE cc_tasks
            SET status = ?, active_form = ?, updated_at = CURRENT_TIMESTAMP,
                completed_at = CASE WHEN ? = 'completed' AND completed_at IS NULL
                               THEN CURRENT_TIMESTAMP ELSE completed_at END
            WHERE id = ?
        `, task.Status, task.ActiveForm, task.Status, task.ID)
        if err != nil {
            return err
        }
    }

    for _, taskID := range toDelete {
        _, err := tx.Exec(`DELETE FROM cc_tasks WHERE id = ?`, taskID)
        if err != nil {
            return err
        }
    }

    // Update last sync timestamp
    _, err = tx.Exec(`
        UPDATE session_task_lists SET last_sync_at = CURRENT_TIMESTAMP
        WHERE session_id = ?
    `, a.sessionID)
    if err != nil {
        return err
    }

    return tx.Commit()
}
```

---

## 10. User Experience

### 10.1 Setup Flow

```bash
# One-time setup (extends existing bd setup claude)
$ bd setup claude

Installing Claude Code hooks...
  ✓ SessionStart hook (bd prime)
  ✓ PreCompact hook (bd prime)
  ✓ PostToolUse hook (bd tasks sync) [NEW]
  ✓ SessionEnd hook (bd tasks finalize) [NEW]

Task tracking enabled. Use 'bd tasks link <bead-id>' to associate sessions with beads.
```

### 10.2 During Claude Code Session

**Automatic (transparent to user):**
1. Claude Code creates tasks via TodoWrite
2. PostToolUse hook fires, signals sync agent
3. Agent debounces and persists to beads DB
4. User can query status in another terminal

**Manual bead association:**
```bash
# In another terminal, or via Claude Code
$ bd tasks link bd-f7k2
Linked session abc123 to bead bd-f7k2 ("Implement user authentication")

# Claude Code can also do this
> Please link this session to the auth bead
(Claude runs: bd tasks link bd-f7k2)
```

### 10.3 Querying Task State

```bash
# View tasks for current session
$ bd tasks list
Session: abc123 → bd-f7k2 ("Implement user authentication")
Synced: 30 seconds ago

  ✓ Research existing auth patterns in codebase
  ✓ Design JWT token structure
  → Implementing token validation middleware
  ○ Adding refresh token rotation
  ○ Writing integration tests
  ○ Updating API documentation

Progress: 2/6 completed, 1 in progress

# View tasks for a specific bead
$ bd tasks list --bead bd-f7k2
Active sessions working on bd-f7k2:
  abc123 (started 45 min ago): 2/6 tasks completed

# JSON output for programmatic use
$ bd tasks list --session abc123 --json
{
  "session_id": "abc123",
  "bead_id": "bd-f7k2",
  "started_at": "2026-01-25T10:30:00Z",
  "last_sync_at": "2026-01-25T11:14:30Z",
  "tasks": [
    {"content": "Research existing auth patterns", "status": "completed", ...},
    ...
  ]
}
```

### 10.4 Enhanced bd show

```bash
$ bd show bd-f7k2

bd-f7k2: Implement user authentication
Status: in_progress | Priority: P1 | Type: feature

Active work:
  Session abc123 (Claude Code)
  Started: 45 minutes ago | Last activity: 30 seconds ago
  Progress: 2/6 tasks (33%)
    ✓ Research existing auth patterns in codebase
    ✓ Design JWT token structure
    → Implementing token validation middleware
    ○ Adding refresh token rotation
    ○ Writing integration tests
    ○ Updating API documentation

Description:
  Add JWT-based authentication with refresh tokens...

Dependencies: [none]
Blocked by: [none]
```

### 10.5 Context Injection Enhancement

Extend `bd prime` to include active task context:

```markdown
## Current Session Context

You are working on: bd-f7k2 "Implement user authentication"

Your current tasks:
  ✓ Research existing auth patterns in codebase
  ✓ Design JWT token structure
  → Implementing token validation middleware (IN PROGRESS)
  ○ Adding refresh token rotation
  ○ Writing integration tests
  ○ Updating API documentation

Continue with the current in-progress task.
```

---

## 11. Migration Strategy

### 11.1 Database Migration

Add migration file: `internal/storage/sqlite/migrations/035_task_tracking_tables.go`

```go
func init() {
    migrations = append(migrations, Migration{
        Version: 35,
        Name:    "task_tracking_tables",
        Up: func(db *sql.DB) error {
            _, err := db.Exec(`
                CREATE TABLE IF NOT EXISTS session_task_lists (
                    session_id TEXT PRIMARY KEY,
                    bead_id TEXT,
                    task_list_id TEXT,
                    started_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
                    last_sync_at TIMESTAMP,
                    ended_at TIMESTAMP,
                    state TEXT,
                    FOREIGN KEY (bead_id) REFERENCES issues(id) ON DELETE SET NULL
                );

                CREATE TABLE IF NOT EXISTS cc_tasks (
                    id TEXT PRIMARY KEY,
                    session_id TEXT NOT NULL,
                    ordinal INTEGER NOT NULL,
                    content TEXT NOT NULL,
                    active_form TEXT,
                    status TEXT NOT NULL DEFAULT 'pending',
                    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
                    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
                    completed_at TIMESTAMP,
                    FOREIGN KEY (session_id) REFERENCES session_task_lists(session_id) ON DELETE CASCADE,
                    UNIQUE(session_id, ordinal)
                );

                CREATE INDEX IF NOT EXISTS idx_session_task_lists_bead ON session_task_lists(bead_id);
                CREATE INDEX IF NOT EXISTS idx_cc_tasks_session ON cc_tasks(session_id);
                CREATE INDEX IF NOT EXISTS idx_cc_tasks_status ON cc_tasks(status);
            `)
            return err
        },
        Down: func(db *sql.DB) error {
            _, err := db.Exec(`
                DROP TABLE IF EXISTS cc_tasks;
                DROP TABLE IF EXISTS session_task_lists;
            `)
            return err
        },
    })
}
```

### 11.2 Hook Migration

For existing users, provide hook update command:

```bash
$ bd setup claude --update-hooks
Updating Claude Code hooks...
  ✓ PostToolUse hook added
  ✓ SessionEnd hook added
Done. Task tracking is now enabled.
```

### 11.3 Rollback Plan

If issues arise:
1. Remove PostToolUse and SessionEnd hooks from settings.json
2. Run `bd db migrate down 35` to remove tables
3. Task data is self-contained, no impact on core bead functionality

---

## 12. Security Considerations

### 12.1 Data Sensitivity

- **Task content may contain sensitive information** (code snippets, file paths, business logic)
- Tasks are stored locally in SQLite (same as beads)
- Not exported to JSONL by default (not committed to git)
- File permissions follow beads database (0600)

### 12.2 IPC Security

- Sync agent uses file-based IPC in temp directory
- Session-specific directories with restrictive permissions (0700)
- PID files prevent unauthorized agent spawning

### 12.3 Session Spoofing

- Session IDs come from `CLAUDE_SESSION_ID` env var set by Claude Code
- Hooks run in Claude Code's process context
- No additional validation needed (trust Claude Code)

### 12.4 Resource Limits

- Debouncing prevents excessive writes (500ms minimum between syncs)
- Idle timeout terminates abandoned agents (30 min)
- Maximum task count per session: 1000 (configurable)

---

## 13. Testing Strategy

### 13.1 Unit Tests

```go
// internal/tasks/sync_test.go

func TestTaskDiff(t *testing.T) {
    tests := []struct {
        name     string
        current  []Task
        new      []Task
        wantIns  int
        wantUpd  int
        wantDel  int
    }{
        {
            name:    "empty to new tasks",
            current: nil,
            new:     []Task{{Content: "Task 1", Status: "pending"}},
            wantIns: 1,
        },
        {
            name:    "status update",
            current: []Task{{ID: "t1", Content: "Task 1", Status: "pending"}},
            new:     []Task{{Content: "Task 1", Status: "completed"}},
            wantUpd: 1,
        },
        // ... more cases
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            ins, upd, del := diffTasks(tt.current, tt.new)
            assert.Len(t, ins, tt.wantIns)
            assert.Len(t, upd, tt.wantUpd)
            assert.Len(t, del, tt.wantDel)
        })
    }
}
```

### 13.2 Integration Tests

```go
// cmd/bd/tasks_test.go

func TestTaskSyncEndToEnd(t *testing.T) {
    // Setup test database
    store := setupTestStore(t)
    defer store.Close()

    sessionID := "test-session-123"

    // Simulate hook input
    hookInput := `{
        "session_id": "test-session-123",
        "tool_name": "TodoWrite",
        "tool_input": {
            "todos": [
                {"content": "Task 1", "activeForm": "Working on Task 1", "status": "in_progress"},
                {"content": "Task 2", "activeForm": "Working on Task 2", "status": "pending"}
            ]
        }
    }`

    // Run sync command
    cmd := NewTasksSyncCmd()
    cmd.SetIn(strings.NewReader(hookInput))
    err := cmd.Execute()
    require.NoError(t, err)

    // Wait for debounce
    time.Sleep(600 * time.Millisecond)

    // Verify tasks in database
    tasks, err := store.GetTasksForSession(sessionID)
    require.NoError(t, err)
    assert.Len(t, tasks, 2)
    assert.Equal(t, "in_progress", tasks[0].Status)
}
```

### 13.3 Hook Integration Tests

```bash
# Test hook installation
$ bd setup claude --dry-run
Would install hooks:
  PostToolUse: bd tasks sync --session "$CLAUDE_SESSION_ID"
  SessionEnd: bd tasks finalize --session "$CLAUDE_SESSION_ID"

# Manual hook trigger test
$ echo '{"session_id":"test","tool_name":"TodoWrite","tool_input":{"todos":[{"content":"Test","status":"pending"}]}}' | bd tasks sync
Synced 1 task for session test
```

---

## 14. Open Questions

### 14.1 Bead Association Strategy

**Question:** How should sessions be associated with beads?

**Options:**
1. **Explicit only:** User must run `bd tasks link <bead-id>`
2. **Infer from context:** Parse bd prime output for current bead
3. **Environment variable:** Set `BD_ACTIVE_BEAD` in session
4. **Prompt detection:** Parse user prompts for bead references

**Recommendation:** Start with explicit linking (option 1), add inference later.

### 14.2 Task Conflict Resolution

**Question:** What happens when TodoWrite sends a completely different task list?

**Options:**
1. **Replace all:** Delete old tasks, insert new ones
2. **Merge by content:** Match tasks by content, update status
3. **Append only:** Never delete, mark orphaned tasks as "abandoned"

**Recommendation:** Option 2 (merge by content) with configurable fallback to option 1.

### 14.3 Cross-Session Task Visibility

**Question:** Should resumed sessions see tasks from previous sessions on the same bead?

**Options:**
1. **No:** Each session is independent
2. **Yes, read-only:** Can see history but not modify
3. **Yes, full access:** Can continue previous tasks

**Recommendation:** Option 2 for V1, explore option 3 for V2.

### 14.4 Git Integration

**Question:** Should task data be included in JSONL exports?

**Options:**
1. **Never:** Tasks are ephemeral, not worth versioning
2. **Optional:** `bd export --include-tasks`
3. **Always:** Full audit trail in git

**Recommendation:** Option 2 (optional export).

### 14.5 Multi-Agent Scenarios

**Question:** How should the system handle multiple sessions working on the same bead?

**Current design:** Multiple sessions can link to the same bead, each tracked separately.

**Future consideration:** Task deduplication, conflict detection, or work partitioning.

---

## 15. Future Considerations

### 15.1 V2: Bi-directional Sync

Sync bead updates back to Claude Code:
- Bead status changes trigger context refresh
- New dependencies reflected in task list
- Human comments surfaced to agent

### 15.2 V2: Automatic Bead Association

Infer bead from session context:
- Parse `bd prime` output
- Detect `bd show`, `bd ready` commands
- NLP matching of task content to bead descriptions

### 15.3 V2: Task-to-Bead Conversion

Automatically create sub-beads from tasks:
```bash
$ bd tasks promote --session abc123 --task "cct-a1b2c3d4"
Created bd-x7y8 "Adding refresh token rotation" as child of bd-f7k2
```

### 15.4 V2: Real-time Notifications

Push task state changes to other clients:
- WebSocket for dashboard
- Desktop notifications
- Slack/Teams integration

### 15.5 V3: Multi-Agent Orchestration

Use task tracking for agent coordination:
- Detect overlapping work
- Automatic task assignment
- Dependency-based sequencing
- Load balancing across agents

---

## Appendix

### A. Claude Code Hook Input Schema

```typescript
// PostToolUse hook input
interface PostToolUseInput {
  session_id: string;
  transcript_path: string;
  cwd: string;
  tool_name: string;        // "TodoWrite" for task tracking
  tool_input: any;          // Tool-specific input
  tool_result: string;      // Tool output (success/failure)
  hook_event_name: "PostToolUse";
}

// TodoWrite tool input
interface TodoWriteInput {
  todos: Array<{
    content: string;        // Imperative form: "Run tests"
    activeForm: string;     // Progressive form: "Running tests"
    status: "pending" | "in_progress" | "completed";
  }>;
}
```

### B. Example Task Sync Flow

```
Time    Event                           Action
─────────────────────────────────────────────────────────────────
T+0s    Claude creates tasks            TodoWrite(todos=[A,B,C pending])
T+0.1s  PostToolUse fires               bd tasks sync receives input
T+0.1s  Sync agent signaled             Write to pending.json
T+0.6s  Debounce expires                Agent syncs to SQLite
T+5s    Claude starts task A            TodoWrite(A=in_progress, B,C pending)
T+5.1s  PostToolUse fires               Signal agent
T+5.6s  Debounce expires                Agent updates A status
T+30s   Claude completes A              TodoWrite(A=completed, B=in_progress, C pending)
T+30.6s Debounce expires                Agent updates A,B status
...
T+300s  Session ends                    SessionEnd hook fires
T+300s  bd tasks finalize               Mark session ended, stop agent
```

### C. Configuration Options

```yaml
# .beads/config.yaml
tasks:
  enabled: true
  sync:
    debounce_ms: 500          # Debounce window
    idle_timeout_min: 30      # Agent idle timeout
    max_tasks_per_session: 1000
  storage:
    export_to_jsonl: false    # Include in git-tracked export
    retention_days: 30        # Auto-delete old sessions
  hooks:
    auto_install: true        # Install on bd setup claude
```

### D. Related Beads Fields

Existing fields that interact with task tracking:

| Field | Type | Description |
|-------|------|-------------|
| `ClosedBySession` | string | Session that closed this bead |
| `Status` | string | `in_progress` when actively worked |
| `LastActivity` | timestamp | Agent heartbeat (for molecules) |
| `AgentState` | string | Agent status (for agent-beads) |

### E. File Locations

| Resource | Path |
|----------|------|
| SQLite database | `.beads/beads.db` |
| Task tables | `cc_tasks`, `session_task_lists` (in beads.db) |
| Sync agent IPC | `/tmp/bd-tasks/{session_id}/` |
| Claude Code hooks | `~/.claude/settings.json` |
| Migration file | `internal/storage/sqlite/migrations/035_*.go` |

---

## Revision History

| Version | Date | Author | Changes |
|---------|------|--------|---------|
| 1.0 | 2026-01-25 | Claude | Initial draft |
