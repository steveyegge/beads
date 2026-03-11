---
id: export
title: bd export
sidebar_position: 220
---

<!-- AUTO-GENERATED: do not edit manually -->
Generated from `bd help --doc export` (bd version 0.59.0)

## bd export

Export all issues to JSONL (newline-delimited JSON) format.

Each line is a complete JSON object representing one issue, including its
labels, dependencies, and comment count. The output is compatible with
'bd import' for round-trip backup and restore.

By default, exports only regular issues (excluding infrastructure beads
like agents, rigs, roles, and messages). Use --all to include everything.

EXAMPLES:
  bd export                          # Export to stdout
  bd export -o backup.jsonl          # Export to file
  bd export --all -o full.jsonl      # Include infra + templates + gates
  bd export --scrub -o clean.jsonl   # Exclude test/pollution records

```
bd export [flags]
```

**Flags:**

```
      --all             Include all records (infra, templates, gates)
      --include-infra   Include infrastructure beads (agents, rigs, roles, messages)
  -o, --output string   Output file path (default: stdout)
      --scrub           Exclude test/pollution records
```

