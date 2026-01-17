# External Tracker Integrations

Beads can sync issues bidirectionally with external issue trackers. This allows you to:
- Import existing issues from your team's tracker into beads for local work
- Export beads issues back to the tracker for team visibility
- Keep both systems in sync with conflict resolution

## Supported Trackers

| Tracker | Command | Status |
|---------|---------|--------|
| [Linear](#linear) | `bd linear` | Production |
| [Jira](#jira) | `bd jira` | Production |
| [Azure DevOps](#azure-devops) | `bd azuredevops` / `bd ado` | Production |

## Common Concepts

All tracker integrations share the same sync model:

### Sync Modes

| Mode | Flag | Description |
|------|------|-------------|
| Pull | `--pull` | Import issues from tracker into beads |
| Push | `--push` | Export beads issues to tracker |
| Bidirectional | (no flags) | Pull then push with conflict resolution |

### Conflict Resolution

When the same issue is modified in both beads and the external tracker:

| Flag | Behavior |
|------|----------|
| (default) | Newer timestamp wins |
| `--prefer-local` | Always use beads version |
| `--prefer-<tracker>` | Always use tracker version |

### Common Flags

```bash
--dry-run       # Preview changes without making them
--create-only   # Only create new issues, don't update existing
--state <val>   # Filter by state: open, closed, all (default: all)
```

---

## Linear

Sync issues with [Linear](https://linear.app/).

### Configuration

```bash
# Required
bd config set linear.api_key "YOUR_API_KEY"
bd config set linear.team_id "TEAM_UUID"

# Optional - sync only one project
bd config set linear.project_id "PROJECT_UUID"
```

**Environment variables** (alternative to config):
- `LINEAR_API_KEY` - Linear API key
- `LINEAR_TEAM_ID` - Linear team ID (UUID)

### Commands

```bash
# Sync operations
bd linear sync --pull              # Import from Linear
bd linear sync --push              # Export to Linear
bd linear sync                     # Bidirectional sync
bd linear sync --dry-run           # Preview without changes
bd linear sync --prefer-local      # Local wins on conflicts
bd linear sync --prefer-linear     # Linear wins on conflicts

# Status
bd linear status                   # Show sync status
bd linear status --json            # JSON output
```

### Field Mapping

Linear fields are mapped to beads fields with sensible defaults. Override with config:

**Priority mapping** (Linear 0-4 to Beads 0-4):
```bash
bd config set linear.priority_map.0 4    # No priority -> Backlog
bd config set linear.priority_map.1 0    # Urgent -> Critical
bd config set linear.priority_map.2 1    # High -> High
bd config set linear.priority_map.3 2    # Medium -> Medium
bd config set linear.priority_map.4 3    # Low -> Low
```

**State mapping** (Linear state type to beads status):
```bash
bd config set linear.state_map.backlog open
bd config set linear.state_map.unstarted open
bd config set linear.state_map.started in_progress
bd config set linear.state_map.completed closed
bd config set linear.state_map.canceled closed
```

**Label to issue type mapping**:
```bash
bd config set linear.label_type_map.bug bug
bd config set linear.label_type_map.feature feature
bd config set linear.label_type_map.epic epic
```

---

## Jira

Sync issues with [Jira](https://www.atlassian.com/software/jira) (Cloud or Server).

### Configuration

```bash
# Required
bd config set jira.url "https://company.atlassian.net"
bd config set jira.project "PROJ"
bd config set jira.api_token "YOUR_TOKEN"
bd config set jira.username "your_email@company.com"  # For Jira Cloud
```

**Environment variables** (alternative to config):
- `JIRA_API_TOKEN` - Jira API token
- `JIRA_USERNAME` - Jira username/email

### Commands

```bash
# Sync operations
bd jira sync --pull                # Import from Jira
bd jira sync --push                # Export to Jira
bd jira sync                       # Bidirectional sync
bd jira sync --dry-run             # Preview without changes
bd jira sync --prefer-local        # Local wins on conflicts
bd jira sync --prefer-jira         # Jira wins on conflicts

# Status
bd jira status                     # Show sync status
bd jira status --json              # JSON output
```

### Getting a Jira API Token

1. Go to [Atlassian Account Settings](https://id.atlassian.com/manage-profile/security/api-tokens)
2. Click "Create API token"
3. Give it a name (e.g., "beads sync")
4. Copy the token and configure beads

---

## Azure DevOps

Sync work items with [Azure DevOps](https://dev.azure.com/).

### Configuration

```bash
# Required
bd config set azuredevops.organization "myorg"
bd config set azuredevops.project "myproject"
bd config set azuredevops.pat "YOUR_PERSONAL_ACCESS_TOKEN"
```

**Environment variables** (alternative to config):
- `AZURE_DEVOPS_PAT` - Azure DevOps Personal Access Token
- `AZURE_DEVOPS_ORGANIZATION` - Azure DevOps organization name
- `AZURE_DEVOPS_PROJECT` - Azure DevOps project name

### Commands

```bash
# Sync operations
bd azuredevops sync --pull         # Import work items from Azure DevOps
bd azuredevops sync --push         # Export issues to Azure DevOps
bd azuredevops sync                # Bidirectional sync
bd azuredevops sync --dry-run      # Preview without changes
bd azuredevops sync --prefer-local # Local wins on conflicts
bd azuredevops sync --prefer-ado   # Azure DevOps wins on conflicts

# Short alias
bd ado sync --pull                 # Same as bd azuredevops sync --pull

# Status
bd azuredevops status              # Show sync status
bd ado status                      # Short alias
bd ado status --json               # JSON output

# List projects
bd azuredevops projects            # List available projects
bd ado projects                    # Short alias
bd ado projects --json             # JSON output
```

### Getting a Personal Access Token (PAT)

1. Go to Azure DevOps → User Settings → Personal Access Tokens
2. Click "New Token"
3. Give it a name (e.g., "beads sync")
4. Set expiration as needed
5. Select scopes:
   - **Work Items**: Read & Write
   - **Project and Team**: Read (for listing projects)
6. Click "Create" and copy the token

### Finding Your Project Name

If you're not sure which project to use:

```bash
# First, configure PAT and organization
bd config set azuredevops.pat "YOUR_PAT"
bd config set azuredevops.organization "myorg"

# List available projects
bd ado projects

# Output:
# Available Azure DevOps Projects
# ================================
#
# Name (use for azuredevops.project)        State         Visibility
# ----------------------------------------  ------------  ----------
# MyProject                                 wellFormed    private
# AnotherProject                            wellFormed    private

# Then configure the project
bd config set azuredevops.project "MyProject"
```

### Work Item Type Mapping

Azure DevOps work item types are mapped to beads issue types:

| Azure DevOps | Beads |
|--------------|-------|
| Bug | bug |
| User Story | feature |
| Task | task |
| Epic | epic |
| Feature | feature |
| Issue | task |

### Example Workflow

```bash
# 1. Configure Azure DevOps connection
bd config set azuredevops.organization "contoso"
bd config set azuredevops.pat "$AZURE_DEVOPS_PAT"

# 2. Find and set your project
bd ado projects
bd config set azuredevops.project "WebApp"

# 3. Check connection status
bd ado status

# 4. Import existing work items
bd ado sync --pull --dry-run      # Preview first
bd ado sync --pull                # Import

# 5. Work locally with beads
bd ready                          # Find work
bd update bd-abc --status in_progress
# ... do work ...
bd close bd-abc --reason "Done"

# 6. Sync back to Azure DevOps
bd ado sync --push
```

---

## Sync Best Practices

### Initial Import

When first connecting to a tracker, start with a pull-only dry run:

```bash
bd <tracker> sync --pull --dry-run
```

Review the output, then import:

```bash
bd <tracker> sync --pull
```

### Regular Workflow

For daily use, bidirectional sync keeps both systems current:

```bash
bd <tracker> sync
```

### Avoiding Conflicts

To minimize conflicts:
1. Claim issues before working (`bd update <id> --status in_progress`)
2. Sync frequently (`bd <tracker> sync`)
3. Use `--prefer-local` or `--prefer-<tracker>` if you have a clear source of truth

### External References

After pushing a beads issue to a tracker, the `external_ref` field stores the tracker URL:

```bash
bd show bd-abc --json | jq .external_ref
# "https://dev.azure.com/contoso/WebApp/_workitems/edit/123"
```

This enables:
- Linking back to the original issue
- Preventing duplicate creation on subsequent syncs
- Tracking which issues have been exported

---

## Troubleshooting

### Authentication Errors

If you see authentication errors:

1. Verify credentials are set:
   ```bash
   bd config get <tracker>.api_key  # or .pat, .api_token
   ```

2. Check environment variables are exported:
   ```bash
   echo $LINEAR_API_KEY
   echo $JIRA_API_TOKEN
   echo $AZURE_DEVOPS_PAT
   ```

3. Verify token permissions match required scopes

### No Issues Synced

If sync reports 0 issues:

1. Check project/team configuration
2. Verify you have access to the project
3. Try with `--state all` to include closed issues
4. Use `--dry-run` to see what would be synced

### Conflict Resolution Issues

If conflicts aren't resolving as expected:

1. Use `--dry-run` to preview resolution
2. Explicitly set resolution strategy (`--prefer-local` or `--prefer-<tracker>`)
3. Check `updated_at` timestamps on conflicting issues

## See Also

- [CLI_REFERENCE.md](CLI_REFERENCE.md) - Full CLI command reference
- [SYNC.md](SYNC.md) - Git-based sync architecture (internal beads sync)
- [CONFIG.md](CONFIG.md) - Configuration system
