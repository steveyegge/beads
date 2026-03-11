---
id: github
title: bd github
sidebar_position: 999
---

<!-- AUTO-GENERATED: do not edit manually -->
Generated from `bd help --doc github` (bd version 0.59.0)

## bd github

Commands for syncing issues between beads and GitHub.

Configuration can be set via 'bd config' or environment variables:
  github.token / GITHUB_TOKEN           - Personal access token
  github.owner / GITHUB_OWNER           - Repository owner
  github.repo / GITHUB_REPO             - Repository name
  github.repository / GITHUB_REPOSITORY - Combined "owner/repo" format
  github.url / GITHUB_API_URL           - Custom API URL (GitHub Enterprise)

```
bd github
```

### bd github repos

List GitHub repositories that the configured token has access to.

```
bd github repos
```

### bd github status

Display current GitHub configuration and sync status.

```
bd github status
```

### bd github sync

Synchronize issues between beads and GitHub.

By default, performs bidirectional sync:
- Pulls new/updated issues from GitHub to beads
- Pushes local beads issues to GitHub

Use --pull-only or --push-only to limit direction.

```
bd github sync [flags]
```

**Flags:**

```
      --dry-run         Show what would be synced without making changes
      --prefer-github   On conflict, use GitHub version
      --prefer-local    On conflict, keep local beads version
      --prefer-newer    On conflict, use most recent version (default)
      --pull-only       Only pull issues from GitHub
      --push-only       Only push issues to GitHub
```

