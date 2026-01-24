---
id: vikunja
title: Vikunja
sidebar_position: 5
---

# Vikunja Integration

Sync issues bidirectionally between beads and [Vikunja](https://vikunja.io/).

## Setup

### 1. Get API Token

Create an API token in Vikunja at **Settings > API Tokens** with the following scopes:

| Scope | Permissions | Purpose |
|-------|-------------|---------|
| `projects` | `read_all`, `read_one` | List and read projects |
| `tasks` | `read_all`, `create`, `read_one`, `update` | Read/create/update tasks |
| `tasks_relations` | `create`, `delete` | Sync task relations (optional) |

**Minimal token permissions (JSON):**
```json
{
  "projects": ["read_all", "read_one"],
  "tasks": ["read_all", "create", "read_one", "update"]
}
```

**With relation sync:**
```json
{
  "projects": ["read_all", "read_one"],
  "tasks": ["read_all", "create", "read_one", "update"],
  "tasks_relations": ["create", "delete"]
}
```

### 2. Configure beads

```bash
bd config set vikunja.api_url "https://your-vikunja-instance.com/api/v1"
bd config set vikunja.api_token "YOUR_API_TOKEN"
```

Or use environment variables:

```bash
export VIKUNJA_API_URL="https://your-vikunja-instance.com/api/v1"
export VIKUNJA_API_TOKEN="YOUR_API_TOKEN"
```

### 3. Select Project

List available projects:

```bash
bd vikunja projects
```

Configure the project to sync:

```bash
bd config set vikunja.project_id "123"
```

## Usage

### Import from Vikunja

```bash
bd vikunja sync --pull
```

### Export to Vikunja

```bash
bd vikunja sync --push
```

### Bidirectional Sync

```bash
bd vikunja sync
```

### Preview Changes

```bash
bd vikunja sync --dry-run
```

### Check Status

```bash
bd vikunja status
```

## Sync Options

| Flag | Description |
|------|-------------|
| `--pull` | Import tasks from Vikunja |
| `--push` | Export issues to Vikunja |
| `--dry-run` | Preview without making changes |
| `--create-only` | Only create new issues, don't update |
| `--state` | Filter by state: open, closed, all |
| `--type` | Only sync specific issue types |
| `--exclude-type` | Exclude specific issue types |
| `--prefer-local` | Prefer local version on conflicts |
| `--prefer-vikunja` | Prefer Vikunja version on conflicts |

## Configuration

| Key | Description | Default |
|-----|-------------|---------|
| `vikunja.api_url` | Vikunja API base URL | Required |
| `vikunja.api_token` | API token for authentication | Required |
| `vikunja.project_id` | Project ID to sync | Required |
| `vikunja.id_mode` | ID generation: "hash" or "db" | "hash" |
| `vikunja.hash_length` | Hash ID length (3-8) | 6 |
| `vikunja.last_sync` | Last sync timestamp (auto-managed) | - |

## Field Mapping

### Priority

| Vikunja | Beads |
|---------|-------|
| 0 (Unset) | 4 (Backlog) |
| 1 (Low) | 3 (Low) |
| 2 (Medium) | 2 (Medium) |
| 3 (High) | 1 (High) |
| 4 (Urgent) | 0 (Critical) |

### Status

| Vikunja | Beads |
|---------|-------|
| `done: false` | Open |
| `done: true` | Closed |

### Relations

| Vikunja | Beads |
|---------|-------|
| blocking, blocked | blocks |
| subtask, parenttask | parent-child |
| related | related |
| duplicateof, duplicates | duplicates |
| precedes, follows | blocks |

## Custom Mappings

Override defaults with config:

```bash
# Custom priority mapping
bd config set vikunja.priority_map.0 2  # Unset -> Medium

# Custom label to type mapping
bd config set vikunja.label_type_map.enhancement feature

# Custom relation mapping
bd config set vikunja.relation_map.blocking depends_on
```

## Examples

### Initial Import

```bash
# List projects to find IDs
bd vikunja projects

# Configure
bd config set vikunja.project_id 42

# Import all tasks
bd vikunja sync --pull

# Verify
bd list
```

### Ongoing Sync

```bash
# Pull latest from Vikunja
bd vikunja sync --pull

# Work locally...
bd create "New task"
bd update bd-123 --status closed

# Push changes back
bd vikunja sync --push
```

### Filtered Sync

```bash
# Only sync bugs and features
bd vikunja sync --push --type=bug,feature

# Exclude wisps from sync
bd vikunja sync --push --exclude-type=wisp
```

## Troubleshooting

### Authentication Failed

```bash
# Verify token
bd vikunja status

# Check API URL ends with /api/v1
bd config get vikunja.api_url
```

### No Projects Found

```bash
# Ensure token has project access
bd vikunja projects

# Check token permissions in Vikunja UI
```

### Sync Conflicts

When both local and Vikunja have changes:

```bash
# Preview conflicts
bd vikunja sync --dry-run

# Force local version
bd vikunja sync --prefer-local

# Or accept Vikunja version
bd vikunja sync --prefer-vikunja
```

## See Also

- [Sync Documentation](/core-concepts/sync)
- [Configuration](/reference/config)
