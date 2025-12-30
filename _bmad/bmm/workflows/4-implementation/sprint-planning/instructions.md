# Sprint Planning - Sprint Status Generator

<critical>The workflow execution engine is governed by: {project-root}/\_bmad/core/tasks/workflow.xml</critical>
<critical>You MUST have already loaded and processed: {project-root}/\_bmad/bmm/workflows/4-implementation/sprint-planning/workflow.yaml</critical>

## 🔧 Beads Preflight Check (REQUIRED)

**Before proceeding**, verify Beads CLI is available:

```bash
_bmad/bin/bd version
```

If this fails, HALT and inform user: "Beads CLI not available. Run the BMAD installer to provision it."

Also verify Beads is initialized:

```bash
if [ ! -d ".beads" ]; then
  _bmad/bin/bd init --quiet
fi
```

---

## 📚 Document Discovery - Full Epic Loading

**Strategy**: Sprint planning needs ALL epics and stories to build complete status tracking.

**Epic Discovery Process:**

1. **Search for whole document first** - Look for `epics.md`, `bmm-epics.md`, or any `*epic*.md` file
2. **Check for sharded version** - If whole document not found, look for `epics/index.md`
3. **If sharded version found**:
   - Read `index.md` to understand the document structure
   - Read ALL epic section files listed in the index (e.g., `epic-1.md`, `epic-2.md`, etc.)
   - Process all epics and their stories from the combined content
   - This ensures complete sprint status coverage
4. **Priority**: If both whole and sharded versions exist, use the whole document

**Fuzzy matching**: Be flexible with document names - users may use variations like `epics.md`, `bmm-epics.md`, `user-stories.md`, etc.

<workflow>

<step n="1" goal="Parse epic files and extract all work items">
<action>Communicate in {communication_language} with {user_name}</action>
<action>Look for all files matching `{epics_pattern}` in {epics_location}</action>
<action>Could be a single `epics.md` file or multiple `epic-1.md`, `epic-2.md` files</action>

<action>For each epic file found, extract:</action>

- Epic numbers from headers like `## Epic 1:` or `## Epic 2:`
- Story IDs and titles from patterns like `### Story 1.1: User Authentication`
- **Beads IDs** if present (e.g., `[proj-a3f8]` in header)
- Convert story format from `Epic.Story: Title` to kebab-case key: `epic-story-title`

**Story ID Conversion Rules:**

- Original: `### Story 1.1: User Authentication [proj-a3f8.1]`
- Replace period with dash: `1-1`
- Convert title to kebab-case: `user-authentication`
- Final key: `1-1-user-authentication`
- **Extract Beads ID**: `proj-a3f8.1`

<action>Build complete inventory of all epics and stories from all epic files</action>
</step>

  <step n="0.5" goal="Discover and load project documents">
    <invoke-protocol name="discover_inputs" />
    <note>After discovery, these content variables are available: {epics_content} (all epics loaded - uses FULL_LOAD strategy)</note>
  </step>

<step n="1.5" goal="Sync Beads graph with epic files">
<action>For each epic/story extracted, ensure Beads issues exist:</action>

**Check existing Beads issues:**

```bash
_bmad/bin/bd list --json --type epic
_bmad/bin/bd list --json --label "bmad:story"
```

**For epics without Beads IDs in the document:**

```bash
_bmad/bin/bd create "Epic: {epic_title}" \
  --type epic \
  --label "bmad:stage:backlog"
```

Record the returned Beads ID.

**For stories without Beads IDs:**

```bash
_bmad/bin/bd create "{story_title}" \
  --parent {epic_beads_id} \
  --type task \
  --label "bmad:story" \
  --label "bmad:stage:backlog"
```

Record the returned Beads ID.

**Sequential blockers** (if not already set):

```bash
# Each story N.M blocked by story N.(M-1)
_bmad/bin/bd dep add {story_id} {previous_story_id} --type blocks
```

<action>After sync, update epic files to include Beads IDs if they were missing</action>
</step>

<step n="2" goal="Build sprint status structure (derived from Beads)">
<action>Query Beads for current state:</action>

```bash
_bmad/bin/bd list --json --type epic
_bmad/bin/bd list --json --label "bmad:story"
```

<action>For each epic found, create entries in this order:</action>

1. **Epic entry** - Key: `epic-{num}`, Status from Beads label, Beads ID
2. **Story entries** - Key: `{epic}-{story}-{title}`, Status from Beads label, Beads ID
3. **Retrospective entry** - Key: `epic-{num}-retrospective`, Default status: `optional`

**Example structure (now includes Beads IDs):**

```yaml
development_status:
  epic-1:
    status: backlog
    beads_id: proj-a3f8
  1-1-user-authentication:
    status: backlog
    beads_id: proj-a3f8.1
  1-2-account-management:
    status: backlog
    beads_id: proj-a3f8.2
  epic-1-retrospective:
    status: optional
```

**Note:** The `sprint-status.yaml` is now a **derived view** of Beads state. Beads is the source of truth.
</step>

<step n="3" goal="Apply intelligent status detection (Beads-first)">
<action>For each story, detect current status from Beads:</action>

**Query Beads for story status:**

```bash
_bmad/bin/bd show {beads_story_id} --json
```

Extract the `bmad:stage:*` label to determine BMAD status:

- `bmad:stage:backlog` → `backlog`
- `bmad:stage:ready-for-dev` → `ready-for-dev`
- `bmad:stage:in-progress` → `in-progress`
- `bmad:stage:review` → `review`
- `bmad:stage:done` or `status: closed` → `done`

**Story file detection (secondary check):**

- Check: `{story_location_absolute}/{story-key}.md` (e.g., `stories/1-1-user-authentication.md`)
- If exists but Beads shows `backlog` → update Beads to `ready-for-dev`:
  ```bash
  _bmad/bin/bd-stage {beads_id} ready-for-dev
  ```

**Beads is authoritative:** If Beads and files disagree, trust Beads and report the discrepancy.

**Status Flow Reference:**

- Epic: `backlog` → `in-progress` → `done`
- Story: `backlog` → `ready-for-dev` → `in-progress` → `review` → `done`
- Retrospective: `optional` ↔ `done`
  </step>

<step n="4" goal="Generate sprint status file (derived view)">
<action>Create or update {status_file} with:</action>

**IMPORTANT:** This file is now a **derived view** of Beads state. Beads is the source of truth.

**File Structure:**

```yaml
# generated: {date}
# project: {project_name}
# project_key: {project_key}
# tracking_system: beads
# story_location: {story_location}
# beads_prefix: {beads_issue_prefix}

# ═══════════════════════════════════════════════════════════════════════════
# BEADS INTEGRATION NOTE
# ═══════════════════════════════════════════════════════════════════════════
# This file is a DERIVED VIEW of Beads issue state.
# Beads is the authoritative source of truth for task status.
# To update status, use: _bmad/bin/bd update <beads_id> --status <status>
# To query ready work: _bmad/bin/bd ready --label "bmad:stage:ready-for-dev"
# ═══════════════════════════════════════════════════════════════════════════

# STATUS DEFINITIONS:
# ==================
# Epic Status:
#   - backlog: Epic not yet started
#   - in-progress: Epic actively being worked on
#   - done: All stories in epic completed
#
# Story Status:
#   - backlog: Story only exists in epic file
#   - ready-for-dev: Story file created in stories folder
#   - in-progress: Developer actively working on implementation
#   - review: Ready for code review (via Dev's code-review workflow)
#   - done: Story completed
#
# Retrospective Status:
#   - optional: Can be completed but not required
#   - done: Retrospective has been completed

generated: { date }
project: { project_name }
project_key: { project_key }
tracking_system: beads
story_location: { story_location }
beads_prefix: { beads_issue_prefix }

development_status:
  # All epics, stories, and retrospectives in order
  # Each entry now includes beads_id for cross-reference
```

<action>Write the complete sprint status YAML to {status_file}</action>
<action>CRITICAL: Each entry MUST include its `beads_id` for cross-reference</action>
<action>Ensure all items are ordered: epic, its stories, its retrospective, next epic...</action>
</step>

<step n="5" goal="Validate and report">
<action>Perform validation checks:</action>

- [ ] Every epic in epic files has a Beads issue
- [ ] Every story in epic files has a Beads issue
- [ ] Beads hierarchy matches document hierarchy (epic → stories)
- [ ] Sequential blockers are in place for stories
- [ ] Every epic has a corresponding retrospective entry in {status_file}
- [ ] All status values match Beads labels
- [ ] File is valid YAML syntax

<action>Verify Beads graph:</action>

```bash
_bmad/bin/bd list --json --type epic
_bmad/bin/bd ready --json
```

<action>Count totals (from Beads):</action>

- Total epics: {{epic_count}}
- Total stories: {{story_count}}
- Epics in-progress: {{in_progress_count}}
- Stories ready for dev: {{ready_for_dev_count}}
- Stories done: {{done_count}}

<action>Display completion summary to {user_name} in {communication_language}:</action>

**Sprint Status Generated Successfully**

- **File Location:** {status_file} (derived view)
- **Beads Database:** `.beads/issues.jsonl` (source of truth)
- **Total Epics:** {{epic_count}}
- **Total Stories:** {{story_count}}
- **Epics In Progress:** {{epics_in_progress_count}}
- **Stories Ready for Dev:** {{ready_for_dev_count}}
- **Stories Completed:** {{done_count}}

**Next Steps:**

1. Review the generated {status_file}
2. **Use Beads for work discovery:** `_bmad/bin/bd ready --label "bmad:stage:ready-for-dev"`
3. Agents will update Beads status as they work
4. Re-run this workflow to refresh the derived view

</step>

</workflow>

## Additional Documentation

### Status State Machine

**Epic Status Flow:**

```
backlog → in-progress → done
```

- **backlog**: Epic not yet started
- **in-progress**: Epic actively being worked on (stories being created/implemented)
- **done**: All stories in epic completed

**Story Status Flow:**

```
backlog → ready-for-dev → in-progress → review → done
```

- **backlog**: Story only exists in epic file
- **ready-for-dev**: Story file created (e.g., `stories/1-3-plant-naming.md`)
- **in-progress**: Developer actively working
- **review**: Ready for code review (via Dev's code-review workflow)
- **done**: Completed

**Retrospective Status:**

```
optional ↔ done
```

- **optional**: Ready to be conducted but not required
- **done**: Finished

### Guidelines

1. **Epic Activation**: Mark epic as `in-progress` when starting work on its first story
2. **Sequential Default**: Stories are typically worked in order, but parallel work is supported
3. **Parallel Work Supported**: Multiple stories can be `in-progress` if team capacity allows
4. **Review Before Done**: Stories should pass through `review` before `done`
5. **Learning Transfer**: SM typically creates next story after previous one is `done` to incorporate learnings
