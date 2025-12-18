# Initialize Project for Long-Running Development

## description:
First-session setup: initialize beads, create feature backlog, set up verification, baseline commit.

---

## Initializer Protocol

Based on [Anthropic's long-running agent patterns](https://www.anthropic.com/engineering/effective-harnesses-for-long-running-agents).

> "The initializer establishes foundational infrastructure for multi-session development."

### Step 1: Verify Prerequisites

```bash
pwd
git status
bd --version
```

Check:
- In correct project directory
- Git repository exists
- Beads CLI installed

### Step 2: Initialize Beads

```bash
bd init --quiet
```

This creates:
- `.beads/` directory with JSONL storage
- Git hooks for auto-sync
- Merge driver configuration

### Step 3: Gather Project Context

Read and understand:
- README.md - project overview
- Any PRD, requirements, or spec documents
- Existing issues (GitHub, Linear, etc.)
- Current codebase structure

### Step 4: Create Feature Backlog

**Anthropic insight:** 200+ granular features work better than 20 large ones.

For each major feature area:

1. **Create an Epic:**
```bash
bd create "Feature Area Name" \
  -t epic \
  -p 1 \
  -d "High-level description of this feature area" \
  --json
```

2. **Decompose into Granular Tasks (10-20 per epic):**
```bash
bd create "Specific task name" \
  -d "What needs to be done. Acceptance: how to verify it works." \
  -t task \
  -p 2 \
  --json
```

**Task Guidelines:**
- Each task should be completable in one session
- Include acceptance criteria in description
- Use format: "Description. Acceptance: [how to verify]"
- Add dependencies between sequential tasks

### Step 5: Set Up Verification

Ensure test infrastructure exists:

```bash
# Check for test framework
ls tests/ || ls test/ || ls __tests__/

# If missing, note in a task
bd create "Set up test framework" \
  -d "Initialize pytest/vitest/playwright for E2E testing. Acceptance: can run test suite." \
  -p 1 \
  --json
```

### Step 6: Create Baseline Commit

```bash
git add .beads/
git commit -m "feat: initialize beads task tracking

- Created [N] epics for major feature areas
- Decomposed into [M] granular tasks
- Set up dependency graph for sequential work"
```

### Step 7: Push and Verify

```bash
git push
bd ready
```

---

## Feature Decomposition Template

For each feature, create tasks following this pattern:

```
Epic: User Authentication
├── Set up auth database schema
│   Acceptance: migrations run, tables exist
├── Create user registration endpoint
│   Acceptance: POST /register returns 201, user in DB
├── Create login endpoint
│   Acceptance: POST /login returns JWT token
├── Add password hashing
│   Acceptance: passwords not stored in plaintext
├── Create auth middleware
│   Acceptance: protected routes reject invalid tokens
├── Add logout endpoint
│   Acceptance: POST /logout invalidates session
├── Write registration E2E test
│   Acceptance: playwright test passes
├── Write login E2E test
│   Acceptance: playwright test passes
└── Add rate limiting to auth endpoints
    Acceptance: returns 429 after N attempts
```

**Note:** Each task has clear acceptance criteria for verification.

---

## Output Format

```
Project Initialized: [project name]

Created:
  - [N] epics (major feature areas)
  - [M] tasks (granular work items)
  - [K] dependencies (sequential relationships)

Ready to start:
  [.proj-xxx] [P1] Set up test framework
  [.proj-yyy] [P2] Create database schema
  ...

Baseline commit: abc1234

Run `/beads-start` to begin first task.
```

---

## Quick Initialization (Minimal)

For simple projects:

```bash
bd init --quiet
bd create "Project MVP" -t epic -p 1 -d "Minimum viable product" --json
# Add tasks as you discover them
bd sync
git add .beads/ && git commit -m "feat: initialize beads"
```
