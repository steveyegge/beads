---
sidebar_position: 1
title: Architecture Overview
description: Understanding Beads' three-layer data model
---

# Architecture Overview

This document explains how Beads' three-layer architecture works: Git, JSONL, and SQLite.

## The Three Layers

Beads uses a layered architecture where each layer serves a specific purpose:

```
Git Repository (Source of Truth)
    ↓ sync
JSONL Files (Portable, Mergeable)
    ↓ rebuild
SQLite Database (Fast Queries)
```

### Layer 1: Git Repository

Git is the ultimate source of truth. All issue data lives in the repository alongside your code.

**Why Git?**
- Issues travel with the code
- No external service dependency
- Full history via Git log
- Works offline

### Layer 2: JSONL Files

JSONL (JSON Lines) files store issue data in an append-only format.

**Location:** `.beads/*.jsonl`

**Why JSONL?**
- Human-readable
- Git-mergeable (append-only reduces conflicts)
- Portable across systems
- Can be recovered from Git history

### Layer 3: SQLite Database

SQLite provides fast local queries without network latency.

**Location:** `.beads/beads.db`

**Why SQLite?**
- Instant queries (no network)
- Complex filtering and sorting
- Derived from JSONL (rebuildable)

## Data Flow

### Write Path
```
User runs bd create
    → SQLite updated
    → JSONL appended
    → Git commit (on sync)
```

### Read Path
```
User runs bd list
    → SQLite queried
    → Results returned immediately
```

### Sync Path
```
User runs bd sync
    → Git pull
    → JSONL merged
    → SQLite rebuilt if needed
    → Git push
```

## The Daemon

The Beads daemon (`bd daemon`) handles background synchronization:

- Watches for file changes
- Triggers sync on changes
- Keeps SQLite in sync with JSONL
- Manages lock files

:::tip
The daemon is optional but recommended for multi-agent workflows.
:::

## Recovery Model

Because Git is the source of truth, recovery is straightforward:

1. **Lost SQLite?** → Rebuild from JSONL
2. **Lost JSONL?** → Recover from Git history
3. **Conflicts?** → Git merge, then rebuild

See [Recovery](/recovery) for specific procedures.

## Design Decisions

### Why not just SQLite?

SQLite alone doesn't travel with Git or merge well across branches.

### Why not just JSONL?

JSONL is slow for complex queries. SQLite provides indexed lookups.

### Why not a server?

Beads is designed for offline-first, local-first development. No server means no downtime, no latency, no vendor lock-in.
