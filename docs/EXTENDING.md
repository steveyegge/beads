# metadata.json Fields Reference

`metadata.json` is the per-project backend configuration file stored in `.beads/`.
It is parsed by `internal/configfile` and controls storage behaviour, Dolt connection
settings, and daemon lifecycle.

All fields are optional with `omitempty`. Adding a new field to an existing
`metadata.json` does not require migration — missing fields receive their documented
defaults.

## Fields

| Field | Type | Default | Description |
|---|---|---|---|
| `database` | string | `"beads.db"` | Legacy SQLite path; ignored by Dolt backend |
| `dolt_mode` | string | `"embedded"` | `"embedded"` (in-process) or `"server"` (external dolt sql-server) |
| `dolt_server_host` | string | `"127.0.0.1"` | Dolt server hostname (server mode) |
| `dolt_server_port` | int | `3307` | Dolt server port (server mode) |
| `dolt_server_socket` | string | `""` | Unix socket path overriding host/port (server mode) |
| `dolt_server_user` | string | `"root"` | MySQL user (server mode) |
| `dolt_database` | string | `"beads"` | SQL database name (server mode) |
| `dolt_server_tls` | bool | `false` | Enable TLS (required for Hosted Dolt) |
| `dolt_data_dir` | string | `""` | Custom Dolt data directory; use `BEADS_DOLT_DATA_DIR` env var for absolute paths |
| `dolt_remotesapi_port` | int | `8080` | Dolt remotesapi port for federation |
| `project_id` | string | _(generated)_ | UUID identifying this project; guards against cross-project data leakage |
| `global_dolt_database` | string | `""` | Global database name in shared-server mode |
| `deletions_retention_days` | int | `3` | How long deletion records are kept |
| `stale_closed_issues_days` | int | `0` | Stale-check threshold for closed issues; `0` disables |
| `daemon_mode` | string | `"off"` | `"off"` / `"auto"` / `"always"` — bdd daemon dispatch mode (be-oyer9z) |
| `daemon_idle_seconds` | int | `300` | Daemon idle timeout before self-exit |
| `daemon_max_lifetime_seconds` | int | `3600` | Hard ceiling on daemon lifetime (1 h) |
