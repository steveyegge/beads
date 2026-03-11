---
id: config
title: bd config
sidebar_position: 420
---

<!-- AUTO-GENERATED: do not edit manually -->
Generated from `bd help --doc config` (bd version 0.59.0)

## bd config

Manage configuration settings for external integrations and preferences.

Configuration is stored per-project in the beads database and is version-control-friendly.

Common namespaces:
  - jira.*            Jira integration settings
  - linear.*          Linear integration settings
  - github.*          GitHub integration settings
  - custom.*          Custom integration settings
  - status.*          Issue status configuration
  - doctor.suppress.* Suppress specific bd doctor warnings (GH#1095)

Custom Status States:
  You can define custom status states for multi-step pipelines using the
  status.custom config key. Statuses should be comma-separated.

  Example:
    bd config set status.custom "awaiting_review,awaiting_testing,awaiting_docs"

  This enables issues to use statuses like 'awaiting_review' in addition to
  the built-in statuses (open, in_progress, blocked, deferred, closed).

Suppressing Doctor Warnings:
  Suppress specific bd doctor warnings by check name slug:
    bd config set doctor.suppress.pending-migrations true
    bd config set doctor.suppress.git-hooks true
  Check names are converted to slugs: "Git Hooks" → "git-hooks".
  Only warnings are suppressed (errors and passing checks always show).
  To unsuppress: bd config unset doctor.suppress.<slug>

Examples:
  bd config set jira.url "https://company.atlassian.net"
  bd config set jira.project "PROJ"
  bd config set status.custom "awaiting_review,awaiting_testing"
  bd config set doctor.suppress.pending-migrations true
  bd config get jira.url
  bd config list
  bd config unset jira.url

```
bd config
```

### bd config get

Get a configuration value

```
bd config get <key>
```

### bd config list

List all configuration

```
bd config list
```

### bd config set

Set a configuration value

```
bd config set <key> <value>
```

### bd config unset

Delete a configuration value

```
bd config unset <key>
```

### bd config validate

Validate sync-related configuration settings.

Checks:
  - sync.mode is a valid value (dolt-native)
  - federation.sovereignty is valid (T1, T2, T3, T4, or empty)
  - federation.remote is set when sync.mode requires it
  - Remote URL format is valid (dolthub://, gs://, s3://, file://)
  - routing.mode is valid (auto, maintainer, contributor, explicit)

Examples:
  bd config validate
  bd config validate --json

```
bd config validate
```

