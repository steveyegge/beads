# Documentation Drift Prevention With Doc Recipes

> Status: Draft
> Date: 2026-03-11
> Scope: `docs/`, `website/docs/`, CLI reference generation

## Why This Exists

Prompt-only regeneration works, but it is fragile under command churn, model changes, and missing context. Beads is currently in this state:

- We already have CLI doc generation scripts (`scripts/generate-cli-docs.sh`) and some refs also carry targeted stale-reference checkers (`scripts/check-doc-flags.sh`).
- We already have a live CLI dump in `bd help --all`.
- We have active branch work adding richer generators (`bd help --doc`, `bd help --list`) in `feat/help-doc-generator`.
- We have an active command-surface churn branch (`origin/feature/help-all`) where docs can drift quickly.

This design turns ad hoc prompts into deterministic recipes that agents can execute repeatedly and safely.

## Goals

1. Make doc regeneration deterministic and reviewable.
2. Keep facts anchored to machine-verifiable evidence.
3. Support branch-aware generation while help-doc branches are in flight.
4. Allow agents to propose patches with confidence scoring and evidence traces.
5. Preserve human editorial control over intent, pedagogy, and tone.

## Non-Goals

1. Fully autonomous merge of doc changes with no human review.
2. Perfect semantic correctness checks for narrative docs in v1.
3. Replacing existing docs instantly; this is incremental.

## Core Concept: Doc Recipe

A doc recipe is structured metadata (YAML) that defines:

1. Source-of-truth inputs (commands, files, refs).
2. Generation strategy (template/transform mode).
3. Validation assertions.
4. Branch compatibility rules.
5. Required outputs.

This replaces "remember this prompt" with "execute this recipe".

## Recipe Schema (v0)

```yaml
version: 0
id: cli-reference
owners:
  - "@dev-tools"
applies_to:
  - "docs/CLI_REFERENCE.md"
  - "website/docs/cli-reference/*.md"

sources:
  commands:
    - name: combined-help
      run: "bd help --all"
  files:
    - "cmd/bd/**/*.go"
  refs:
    base: "main"
    watch:
      - "feat/help-doc-generator"
      - "origin/feature/help-all"

capabilities:
  optional_commands:
    list: "bd help --list"
    single_doc: "bd help --doc <command>"

generation:
  mode: "hybrid"
  outputs:
    - "docs/CLI_REFERENCE.md"
    - "website/docs/cli-reference/*.md"

validation:
  assertions:
    - "All top-level commands in help output are documented"
    - "No removed command references (sync/import) unless marked historical"
    - "Flag examples match live help text"
```

## Execution Model

### Phase 1: Evidence Collection

1. Capture command evidence (`bd help --all`, optionally `--list`/`--doc`).
2. Capture code evidence (changed command files since baseline ref).
3. Normalize outputs into machine-readable snapshots (stored in CI artifacts).

### Phase 2: Branch-Aware Capability Detection

At runtime, detect available help features:

1. If `bd help --list` and `bd help --doc` exist, generate per-command pages from live command tree.
2. If only `bd help --all` exists, generate combined reference and perform coverage checks.

This keeps one recipe compatible with both current `main` and the in-flight help-doc branch.

### Phase 3: Generation

1. Regenerate deterministic sections first (command listings, flags, usage blocks).
2. Preserve hand-authored narrative sections outside marked generated blocks.
3. Emit patch plus evidence report (which command/flag changed, where).

### Phase 4: Validation

1. Run recipe-declared shell checks (for example `check-doc-flags.sh` when present on that ref).
2. Run recipe assertions (coverage, removed commands, optional example sanity checks).
3. Fail CI on factual drift for critical docs.

## Branch Integration Design

### Branch: `feat/help-doc-generator`

Incorporate directly as optional-capability path:

1. Use `bd help --list` for command enumeration.
2. Use `bd help --doc <command>` for page generation.
3. Keep `bd help --all` as fallback and canonical combined output.

Design note: the current branch includes hard-coded sidebar positions and legacy command entries. Recipe validation should treat ordering metadata as non-authoritative and command presence as authoritative.

### Branch: `origin/feature/help-all`

Treat as command-churn canary while active:

1. Include branch in recipe `refs.watch`.
2. Nightly drift job compares command/flag deltas against `main`.
3. Open doc-update issues automatically when deltas exceed threshold.

This catches drift early without forcing immediate merge coupling.

## CI Pipeline Shape

1. `docs-drift-fast` (PR): run checkers only on touched doc/code areas.
2. `docs-drift-full` (nightly): execute full recipe matrix across watched refs.
3. `docs-drift-report` (artifact): publish evidence snapshots and proposed patch stats.

## Agent Workflow

1. Load recipe.
2. Gather evidence and command capabilities.
3. Produce deterministic updates.
4. Run recipe validations.
5. Emit patch with evidence summary (changed commands/flags/sections).

## Rollout Plan

1. Pilot with CLI reference only (`docs/CLI_REFERENCE.md` and `website/docs/cli-reference/*`).
2. Extend to config and troubleshooting docs using schema-based evidence.
3. Add weekly full-matrix drift scan over watched refs.
4. Enforce blocking CI gates for high-risk docs once false positives are low.

## Open Questions

1. Should sidebar ordering be fully generated or hand-curated with lint checks?
2. Should recipe artifacts be committed snapshots or CI-only artifacts?
3. Which doc classes should be hard-fail versus advisory in CI?
