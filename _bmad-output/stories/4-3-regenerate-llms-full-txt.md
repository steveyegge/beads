# Story 4.3: Regenerate llms-full.txt

Status: done

<!-- ═══════════════════════════════════════════════════════════════════════════ -->
<!-- BEADS TRACKING -->
<!-- ═══════════════════════════════════════════════════════════════════════════ -->
<!-- Beads is the source of truth for task status. Update via bd commands. -->

**Beads IDs:**

- Epic: `bd-907`
- Story: `bd-907.3`

**Quick Commands:**

- View tasks: `bd list --parent bd-907.3`
- Find ready work: `bd ready --parent bd-907.3`
- Mark task done: `bd close <task_id>`

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a **documentation maintainer**,
I want **updated llms-full.txt with new content**,
So that **AI agents have complete documentation context**.

## Acceptance Criteria

1. **Given** new recovery and architecture docs exist
   **When** I run `scripts/generate-llms-full.sh`
   **Then** llms-full.txt includes all new documentation

2. **And** recovery section is included

3. **And** architecture section is included

4. **And** file is under 50K tokens (~37,500 words) (NFR2)

5. **And** URL references use steveyegge.github.io

## Tasks / Subtasks

<!-- ═══════════════════════════════════════════════════════════════════════════ -->
<!-- BEADS IS AUTHORITATIVE: Task status is tracked in Beads, not checkboxes.   -->
<!-- The checkboxes below are for reference only. Use bd commands to update.    -->
<!-- To view current status: bd list --parent bd-907.3                          -->
<!-- ═══════════════════════════════════════════════════════════════════════════ -->

- [x] **Task 1: Check if regeneration is needed** (AC: 1) `bd-907.3.1`
  - [x] Subtask 1.1: Run pre-flight check (see Dev Notes) to detect stale content
  - [x] Subtask 1.2: Verify recovery + architecture document paths present in llms-full.txt
  - [x] Subtask 1.3: Check current word count vs 37,500 limit
  - [x] Subtask 1.4: **Decision point:** If content is current, skip to Task 3. If stale, proceed to Task 2.

- [x] **Task 2: Run regeneration script** (AC: 1, 2, 3) `bd-907.3.2`
  - [x] Subtask 2.1: Execute `./scripts/generate-llms-full.sh`
  - [x] Subtask 2.2: Verify script completes without errors
  - [x] Subtask 2.3: Capture output file size and line count

- [x] **Task 3: Validate recovery content inclusion** (AC: 2) `bd-907.3.3`
  - [x] Subtask 3.1: Verify all 5 recovery document paths present (index + 4 runbooks)
  - [x] Subtask 3.2: Verify content depth — actual recovery commands present (e.g., `bd doctor --fix`)
  - [x] Subtask 3.3: Confirm recovery runbook structure includes diagnosis + solution steps

- [x] **Task 4: Validate architecture content inclusion** (AC: 3) `bd-907.3.4`
  - [x] Subtask 4.1: Verify architecture document path present
  - [x] Subtask 4.2: Confirm key content: "three-layer architecture", "SQLite", "JSONL"

- [x] **Task 5: Validate token budget and URLs** (AC: 4, 5) `bd-907.3.5`
  - [x] Subtask 5.1: Word count < 37,500 (`wc -w`)
  - [x] Subtask 5.2: Zero joyshmitz URLs (`grep joyshmitz` returns empty)
  - [x] Subtask 5.3: Report final statistics (see Success Metrics)

## Dev Notes

### Pre-Flight Check (CRITICAL — Run First)

```bash
# Check if llms-full.txt is older than source docs
find website/docs -name "*.md" -newer website/static/llms-full.txt | head -5

# If output is empty → llms-full.txt is CURRENT, skip to Task 3
# If output lists files → regeneration IS needed, proceed to Task 2
```

### File Info

| Property | Value |
|----------|-------|
| File | `website/static/llms-full.txt` |
| Script | `scripts/generate-llms-full.sh` |
| Current | ~15,885 words (~21K tokens) |
| Budget | 37,500 words (~50K tokens) |
| Headroom | **42% used** — ample room |

Script includes `recovery` in processing order (line 46). No modification needed.

### Verification Commands

```bash
# Full validation suite (run after regeneration OR to verify current state)
wc -w website/static/llms-full.txt && \
  grep -c "docs/recovery" website/static/llms-full.txt && \
  grep -c "docs/architecture" website/static/llms-full.txt && \
  grep joyshmitz website/static/llms-full.txt || echo "URLs valid"
```

### Content Depth Verification

```bash
# Verify recovery commands present (bd doctor --fix is the key recovery command)
grep -A50 "docs/recovery/database-corruption.md" website/static/llms-full.txt | grep -c "bd doctor --fix"
# Expected: ≥1

# Verify architecture key content
grep -c "three-layer\|SQLite\|JSONL" website/static/llms-full.txt
# Expected: ≥3
```

### Success Metrics

| Metric | Expected | Validation |
|--------|----------|------------|
| Word count | 15,000-37,500 | `wc -w` |
| Document paths | ≥20 | `grep -c "<document path"` |
| Recovery paths | 5 | `grep -c "docs/recovery"` |
| Architecture paths | 1 | `grep -c "docs/architecture"` |
| joyshmitz URLs | 0 | `grep -c joyshmitz` |

### Rollback

```bash
# If regeneration produces unexpected results
git checkout HEAD -- website/static/llms-full.txt
```

### References

- [architecture.md#CI/CD Quality Gates] - Token validation CI step
- [project-context.md#Token Budget] - Token budget rules
- [scripts/generate-llms-full.sh] - Generation script
- [Story 4.2] - Previous story (llms.txt recovery section)

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5 (claude-opus-4-5-20251101)

### Debug Log References

N/A

### Completion Notes List

- Pre-flight check: llms-full.txt was regenerated in Epic 3 commit (a29fe1cd) at 18:43
- Current state validation confirmed file is up-to-date (no newer source docs found)
- All 5 recovery document paths verified (index + 4 runbooks)
- Recovery content depth validated (`bd doctor --fix` command present in database-corruption section)
- Architecture key content verified: three-layer (2), SQLite (39), JSONL (71) matches
- Final validation passed: 15,885 words (42% of 37,500 budget), 38 document paths, 0 joyshmitz URLs

### File List

- `website/static/llms-full.txt` - Verified current (107,749 bytes, regenerated in Epic 3)

### Code Review Fixes Applied

- [CR-H1] Removed false `git checkout HEAD` validation claim (command doesn't exist in docs)
- [CR-H1] Updated Dev Notes Content Depth Verification to use actual command (`bd doctor --fix`)
- [CR-M2] Corrected Completion Notes to reflect that file was already current from Epic 3
