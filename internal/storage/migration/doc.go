// Package migration implements lossless data copy from a Dolt-backed bd
// instance to a freshly-initialized Postgres backend.
//
// Scope is fixed by ADR be-l7t.5 / builder bead be-6fk.5:
//
//   - Eight lossless tables copied byte-for-byte: issues, wisps, dependencies,
//     wisp_dependencies, labels, wisp_labels, comments, wisp_comments.
//   - Eight configuration carryover tables (best-effort): config, metadata,
//     custom_statuses, custom_types, child_counters, issue_counter,
//     issue_snapshots, compaction_snapshots.
//   - Audit trail (events, wisp_events) NOT copied; counts are surfaced as a
//     single stderr warning per FR-9.
//   - Atomic single-transaction COPY on the Postgres side.
//
// The package consumes the source bd store through storage.RawDBAccessor
// (Dolt and EmbeddedDolt both expose it) and writes to the destination via
// pgx/v5 CopyFrom + parameterized INSERT. The destination must already have
// the bd schema applied (via storage.Open) — this package never runs DDL.
//
// Reverse direction (Postgres → Dolt) is intentionally out of scope.
package migration
