---
id: gitlab
title: bd gitlab
sidebar_position: 999
---

<!-- AUTO-GENERATED: do not edit manually -->
Generated from `bd help --doc gitlab` (bd version 0.59.0)

## bd gitlab

Commands for syncing issues between beads and GitLab.

Configuration can be set via 'bd config' or environment variables:
  gitlab.url / GITLAB_URL         - GitLab instance URL
  gitlab.token / GITLAB_TOKEN     - Personal access token
  gitlab.project_id / GITLAB_PROJECT_ID - Project ID or path

```
bd gitlab
```

### bd gitlab projects

List GitLab projects that the configured token has access to.

```
bd gitlab projects
```

### bd gitlab status

Display current GitLab configuration and sync status.

```
bd gitlab status
```

### bd gitlab sync

Synchronize issues between beads and GitLab.

By default, performs bidirectional sync:
- Pulls new/updated issues from GitLab to beads
- Pushes local beads issues to GitLab

Use --pull-only or --push-only to limit direction.

```
bd gitlab sync [flags]
```

**Flags:**

```
      --dry-run         Show what would be synced without making changes
      --prefer-gitlab   On conflict, use GitLab version
      --prefer-local    On conflict, keep local beads version
      --prefer-newer    On conflict, use most recent version (default)
      --pull-only       Only pull issues from GitLab
      --push-only       Only push issues to GitLab
```

