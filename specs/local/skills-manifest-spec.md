# Skills Manifest Drift Detection (Local Spec)

## Goal
Detect skill drift between agents by recording a single unified manifest of skills and their content hashes, then using Shadowbook spec drift to flag changes.

## Why
Skills live outside this repo (Codex, Claude). If one agent updates skills and another agent does not, tasks can diverge silently. A manifest turns that mismatch into a spec change that Shadowbook can detect.

## Scope
- Track Codex and Claude skills in one unified manifest.
- Do **not** copy full skill files into the repo.
- Use Shadowbook’s existing `bd spec scan` for drift detection.

## Non-Goals
- Auto-install missing skills.
- Auto-merge skill changes.
- Cross-tool compatibility checks beyond recording source.

## Manifest Format
File: `specs/skills/manifest.json`

Example structure:
```
{
  "version": 1,
  "generated_at": "2026-01-29T00:00:00Z",
  "skills": [
    {
      "name": "writing-clearly-and-concisely",
      "source": "codex",
      "tier": "must-have",
      "path": "~/.codex/skills/writing-clearly-and-concisely/SKILL.md",
      "sha256": "<hex>",
      "bytes": 12345
    },
    {
      "name": "superpowers:brainstorming",
      "source": "codex",
      "tier": "must-have",
      "path": "~/.codex/superpowers/skills/brainstorming/SKILL.md",
      "sha256": "<hex>",
      "bytes": 4567
    },
    {
      "name": "<claude-skill>",
      "source": "claude",
      "tier": "optional",
      "path": "~/.claude/skills/<name>.md",
      "sha256": "<hex>",
      "bytes": 8910
    }
  ]
}
```

Notes:
- `source` is required and must be one of: `codex`, `claude`.
- `tier` is required and must be one of: `must-have`, `optional`. Personal skills are **not** included in the manifest.
- `path` is informational only.
- Hash uses raw file bytes, not normalized text.

## Workflow
1) Generate the manifest from local skill directories.
2) Save to `specs/skills/manifest.json`.
3) Run `bd spec scan`.
4) If the manifest changes, `bd list --spec-changed` will surface drifted issues.

## Drift Semantics
- Any skill add/remove/edit in the manifest changes the manifest hash.
- `must-have` skills: missing or mismatched versions are flagged as **errors** by `bd spec scan`.
- `optional` skills: missing or mismatched versions are flagged as **warnings** by `bd spec scan`.
- Personal skills (excluded from manifest): no drift detection; each agent maintains them independently.
- Drift resolution is manual (install/match skills, then regenerate manifest and ack).

## Tracking Policy
- `specs/skills/manifest.json` is tracked in git to enable cross-agent drift detection.
- The manifest must not include skill contents; only names, sources, paths, hashes, and sizes.

## Skill Tier Guidelines
- **must-have**: Skills critical to tasks completing correctly or producing consistent output (e.g., brainstorming, code review). Drift is a blocker.
- **optional**: Skills that improve workflow but don't block task execution (e.g., specialized helpers). Drift is a courtesy warning.
- **personal**: Skills installed for individual preference; not shared. Examples: custom shortcuts, experimental tools, single-user helpers.

To mark a skill as personal, simply exclude it from the manifest generator's scan output.

## Open Questions
- Where Claude skills are stored on disk (path varies by setup).
- Whether to include tool-specific version metadata in the manifest.

## Next Steps (Implementation)
- Add a small generator script to produce `specs/skills/manifest.json`.
- Add a `bd` helper command or make target to run the generator + `bd spec scan`.
