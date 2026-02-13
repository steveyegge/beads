# Team Workflow Example

This example demonstrates how to use beads for team collaboration with shared repositories.

## Problem

When working as a team on a shared repository, you want to:
- Track issues collaboratively
- Keep everyone in sync via git
- Handle protected main branches
- Maintain clean git history

## Solution

Use `bd init --team` to set up team collaboration with sync-branch settings, then publish issue updates with `bd sync --full` (or git hooks).

## Setup

### Step 1: Initialize Team Workflow

```bash
# In your shared repository
cd my-project

# Run the team setup wizard
bd init --team
```

The wizard will:
1. ✅ Detect your git configuration
2. ✅ Ask if main branch is protected
3. ✅ Configure sync branch (if needed)
4. ✅ Enable team mode
5. ✅ Record legacy auto-sync preference (daemon removed; use `bd sync --full`)

### Step 2: Protected Branch Configuration

If your main branch is protected (GitHub/GitLab), the wizard will:
- Create a separate `beads-metadata` branch for issue updates
- Configure beads to target this branch when you run `bd sync --full`
- Set up periodic PR workflow for merging to main

### Step 3: Team Members Join

Other team members just need to:

```bash
# Clone the repository
git clone https://github.com/org/project.git
cd project

# Initialize beads (auto-imports existing issues)
bd init

# Start working!
bd ready
```

## How It Works

### Direct Commits (No Protected Branch)

If main isn't protected:

```bash
# Create issue
bd create "Implement feature X" -p 1

# Sync issue updates (pull → merge → export → commit → push)
bd sync --full

# View team's issues
bd list
```

### Protected Branch Workflow

If main is protected:

```bash
# Create issue
bd create "Implement feature X" -p 1

# Sync issue updates to beads-metadata
bd sync --full

# Periodically: merge beads-metadata to main via PR
```

## Configuration

The wizard configures:

```yaml
team:
  enabled: true
  sync_branch: beads-metadata  # or main if not protected

sync:
  branch: beads-metadata       # used by bd sync --full
```

### Manual Configuration

```bash
# Enable team mode
bd config set team.enabled true

# Set sync branch
bd config set team.sync_branch beads-metadata

# Set sync branch for bd sync --full
bd config set sync.branch beads-metadata
```

## Example Workflows

### Scenario 1: Unprotected Main

```bash
# Alice creates an issue
bd create "Fix authentication bug" -p 1

# Sync changes to main
bd sync --full

# Bob syncs changes
bd sync --full
bd list  # Sees Alice's issue

# Bob claims it
bd update bd-abc --status in_progress

# Bob syncs his update
bd sync --full
# Alice syncs and sees Bob is working on it
bd sync --full
```

### Scenario 2: Protected Main

```bash
# Alice creates an issue
bd create "Add new API endpoint" -p 1

# Sync changes to beads-metadata
bd sync --full

# Bob syncs beads-metadata
bd sync --full
bd list  # Sees Alice's issue

# Later: merge beads-metadata to main via PR
git checkout main
git pull origin main
git merge beads-metadata
# Create PR, get approval, merge
```

## Team Workflows

### Daily Standup

```bash
# See what everyone's working on
bd list --status in_progress

# See what's ready for work
bd ready

# See recently closed issues
bd list --status closed --limit 10
```

### Sprint Planning

```bash
# Create sprint issues
bd create "Implement user auth" -p 1
bd create "Add profile page" -p 1
bd create "Fix responsive layout" -p 2

# Assign to team members
bd update bd-abc --assignee alice
bd update bd-def --assignee bob

# Track dependencies
bd dep add bd-def bd-abc --type blocks
```

### PR Integration

```bash
# Create issue for PR work
bd create "Refactor auth module" -p 1

# Work on it
bd update bd-abc --status in_progress

# Open PR with issue reference
git push origin feature-branch
# PR title: "feat: refactor auth module (bd-abc)"

# Close when PR merges
bd close bd-abc --reason "PR #123 merged"
```

## Sync Strategies

### Full Sync (Recommended)

Run a full sync when you want to publish issue changes and pull updates:

```bash
bd sync --full
```

Benefits:
- ✅ Always in sync
- ✅ Single command workflow
- ✅ Includes pull/merge/export/commit/push

### Export-Only Sync

Export issues without touching git history:

```bash
bd sync  # Export only (commit/push manually)
```

Benefits:
- ✅ Full control
- ✅ Batch updates
- ✅ Review before push

## Conflict Resolution

Hash-based IDs prevent most conflicts. If conflicts occur:

```bash
# During git pull/merge
git pull origin beads-metadata
# CONFLICT in .beads/issues.jsonl

# Option 1: Accept remote
git checkout --theirs .beads/issues.jsonl
bd import -i .beads/issues.jsonl

# Option 2: Accept local
git checkout --ours .beads/issues.jsonl
bd import -i .beads/issues.jsonl

# Option 3: Use beads-merge tool (recommended)
# See docs/GIT_INTEGRATION.md for merge conflict resolution

git add .beads/issues.jsonl
git commit
```

## Protected Branch Best Practices

### For Protected Main:

1. **Create beads-metadata branch**
   ```bash
   git checkout -b beads-metadata
   git push origin beads-metadata
   ```

2. **Configure protection rules**
   - Allow direct pushes to beads-metadata
   - Require PR for main

3. **Periodic PR workflow**
   ```bash
   # Once per day/sprint
   git checkout main
   git pull origin main
   git checkout beads-metadata
   git pull origin beads-metadata
   git checkout main
   git merge beads-metadata
   # Create PR, get approval, merge
   ```

4. **Keep beads-metadata clean**
   ```bash
   # After PR merges
   git checkout beads-metadata
   git rebase main
   git push origin beads-metadata --force-with-lease
   ```

## Common Questions

### Q: How do team members see each other's issues?

A: Issues are stored in `.beads/issues.jsonl` which is version-controlled. Pull from git to sync.

```bash
git pull
bd list  # See everyone's issues
```

### Q: What if two people create issues at the same time?

A: Hash-based IDs prevent collisions. Even if created simultaneously, they get different IDs.

### Q: How do I disable auto-sync?

A: The current CLI runs in direct mode (no daemon). Sync when you're ready:

```bash
bd sync --full   # full sync
# or
bd sync          # export only (commit/push manually)
```

### Q: Can we use different sync branches per person?

A: Not recommended. Use a single shared branch for consistency. If needed:

```bash
bd config set sync.branch my-custom-branch
```

### Q: What about CI/CD integration?

A: Add to your CI pipeline:

```bash
# In .github/workflows/main.yml
- name: Sync beads issues
  run: |
    bd sync --full --no-push
    git push origin beads-metadata
```

## Troubleshooting

### Issue: Sync not publishing updates

Check sync status and branch:

```bash
bd sync --status
bd config get sync.branch
```

Run a full sync:

```bash
bd sync --full
```

### Issue: Merge conflicts in JSONL

Use beads-merge or resolve manually (see [GIT_INTEGRATION.md](../../docs/GIT_INTEGRATION.md)):

```bash
git checkout --theirs .beads/issues.jsonl
bd import -i .beads/issues.jsonl
git add .beads/issues.jsonl
git commit
```

### Issue: Issues not syncing

Manually sync:

```bash
bd sync --full
```

Check for conflicts:

```bash
bd sync --status
bd validate --checks=conflicts
```

## See Also

- [Protected Branch Setup](../protected-branch/)
- [Contributor Workflow](../contributor-workflow/)
- [Multi-Repo Migration Guide](../../docs/MULTI_REPO_MIGRATION.md)
- [Git Integration Guide](../../docs/GIT_INTEGRATION.md)
