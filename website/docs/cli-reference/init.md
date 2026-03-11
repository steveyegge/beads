---
id: init
title: bd init
sidebar_position: 400
---

<!-- AUTO-GENERATED: do not edit manually -->
Generated from `bd help --doc init` (bd version 0.59.0)

## bd init

Initialize bd in the current directory by creating a .beads/ directory
and Dolt database. Optionally specify a custom issue prefix.

Dolt is the default (and only supported) storage backend. The legacy SQLite
backend has been removed. Use --backend=sqlite to see migration instructions.

Use --database to specify an existing server database name, overriding the
default prefix-based naming. This is useful when an external tool (e.g. gastown)
has already created the database.

With --stealth: configures per-repository git settings for invisible beads usage:
  • .git/info/exclude to prevent beads files from being committed
  Perfect for personal use without affecting repo collaborators.
  To set up a specific AI tool, run: bd setup <claude|cursor|aider|...> --stealth

Beads requires a running dolt sql-server for database operations. If a server is detected
on port 3307 or 3306, it is used automatically. Set connection details with --server-host,
--server-port, and --server-user. Password should be set via BEADS_DOLT_PASSWORD
environment variable.

```
bd init [flags]
```

**Flags:**

```
      --agents-template string   Path to custom AGENTS.md template (overrides embedded default)
      --backend string           Storage backend (default: dolt). --backend=sqlite prints deprecation notice.
      --contributor              Run OSS contributor setup wizard
      --database string          Use existing server database name (overrides prefix-based naming)
      --destroy-token string     Explicit confirmation token for destructive re-init in non-interactive mode (format: 'DESTROY-<prefix>')
      --force                    Force re-initialization even if database already has issues (may cause data loss)
      --from-jsonl               Import issues from .beads/issues.jsonl instead of git history
  -p, --prefix string            Issue prefix (default: current directory name)
  -q, --quiet                    Suppress output (quiet mode)
      --server                   Use server mode (currently the default; embedded mode returning soon)
      --server-host string       Dolt server host (default: 127.0.0.1)
      --server-port int          Dolt server port (default: 3307)
      --server-user string       Dolt server MySQL user (default: root)
      --setup-exclude            Configure .git/info/exclude to keep beads files local (for forks)
      --shared-server            Enable shared Dolt server mode (all projects share one server at ~/.beads/shared-server/)
      --skip-hooks               Skip git hooks installation
      --stealth                  Enable stealth mode: global gitattributes and gitignore, no local repo tracking
      --team                     Run team workflow setup wizard
```

