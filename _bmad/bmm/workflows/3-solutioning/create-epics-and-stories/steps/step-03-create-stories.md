---
name: 'step-03-create-stories'
description: 'Generate all epics with their stories following the template structure'

# Path Definitions
workflow_path: '{project-root}/_bmad/bmm/workflows/3-solutioning/create-epics-and-stories'

# File References
thisStepFile: '{workflow_path}/steps/step-03-create-stories.md'
nextStepFile: '{workflow_path}/steps/step-04-final-validation.md'
workflowFile: '{workflow_path}/workflow.md'
outputFile: '{output_folder}/epics.md'

# Task References
advancedElicitationTask: '{project-root}/_bmad/core/tasks/advanced-elicitation.xml'
partyModeWorkflow: '{project-root}/_bmad/core/workflows/party-mode/workflow.md'

# Template References
epicsTemplate: '{workflow_path}/templates/epics-template.md'
---

# Step 3: Generate Epics and Stories

## STEP GOAL:

To generate all epics with their stories based on the approved epics_list, following the template structure exactly. **Each epic and story will also be created as Beads issues** with appropriate hierarchy and dependencies.

## BEADS INTEGRATION (REQUIRED):

Before proceeding, verify Beads is available:

```bash
_bmad/bin/bd version
```

If this fails, HALT and inform user to run the BMAD installer.

## MANDATORY EXECUTION RULES (READ FIRST):

### Universal Rules:

- 🛑 NEVER generate content without user input
- 📖 CRITICAL: Read the complete step file before taking any action
- 🔄 CRITICAL: Process epics sequentially
- 📋 YOU ARE A FACILITATOR, not a content generator

### Role Reinforcement:

- ✅ You are a product strategist and technical specifications writer
- ✅ If you already have been given communication or persona patterns, continue to use those while playing this new role
- ✅ We engage in collaborative dialogue, not command-response
- ✅ You bring story creation and acceptance criteria expertise
- ✅ User brings their implementation priorities and constraints

### Step-Specific Rules:

- 🎯 Generate stories for each epic following the template exactly
- 🚫 FORBIDDEN to deviate from template structure
- 💬 Each story must have clear acceptance criteria
- 🚪 ENSURE each story is completable by a single dev agent
- 🔗 **CRITICAL: Stories MUST NOT depend on future stories within the same epic**

## EXECUTION PROTOCOLS:

- 🎯 Generate stories collaboratively with user input
- 💾 Append epics and stories to {outputFile} following template
- 📖 Process epics one at a time in sequence
- 🚫 FORBIDDEN to skip any epic or rush through stories

## STORY GENERATION PROCESS:

### 1. Load Approved Epic Structure

Load {outputFile} and review:

- Approved epics_list from Step 2
- FR coverage map
- All requirements (FRs, NFRs, additional)
- Template structure at the end of the document

### 2. Explain Story Creation Approach

**STORY CREATION GUIDELINES:**

For each epic, create stories that:

- Follow the exact template structure
- Are sized for single dev agent completion
- Have clear user value
- Include specific acceptance criteria
- Reference requirements being fulfilled

**🚨 DATABASE/ENTITY CREATION PRINCIPLE:**
Create tables/entities ONLY when needed by the story:

- ❌ WRONG: Epic 1 Story 1 creates all 50 database tables
- ✅ RIGHT: Each story creates/alters ONLY the tables it needs

**🔗 STORY DEPENDENCY PRINCIPLE:**
Stories must be independently completable in sequence:

- ❌ WRONG: Story 1.2 requires Story 1.3 to be completed first
- ✅ RIGHT: Each story can be completed based only on previous stories
- ❌ WRONG: "Wait for Story 1.4 to be implemented before this works"
- ✅ RIGHT: "This story works independently and enables future stories"

**STORY FORMAT (from template):**

```
### Story {N}.{M}: {story_title}

As a {user_type},
I want {capability},
So that {value_benefit}.

**Acceptance Criteria:**

**Given** {precondition}
**When** {action}
**Then** {expected_outcome}
**And** {additional_criteria}
```

**✅ GOOD STORY EXAMPLES:**

_Epic 1: User Authentication_

- Story 1.1: User Registration with Email
- Story 1.2: User Login with Password
- Story 1.3: Password Reset via Email

_Epic 2: Content Creation_

- Story 2.1: Create New Blog Post
- Story 2.2: Edit Existing Blog Post
- Story 2.3: Publish Blog Post

**❌ BAD STORY EXAMPLES:**

- Story: "Set up database" (no user value)
- Story: "Create all models" (too large, no user value)
- Story: "Build authentication system" (too large)
- Story: "Login UI (depends on Story 1.3 API endpoint)" (future dependency!)
- Story: "Edit post (requires Story 1.4 to be implemented first)" (wrong order!)

### 3. Process Epics Sequentially

For each epic in the approved epics_list:

#### A. Create Beads Epic Issue

First, create the Beads epic issue:

```bash
_bmad/bin/bd create "Epic: {epic_title}" \
  --type epic \
  --label "bmad:stage:backlog"
```

Record the Beads Epic ID (e.g., `proj-a3f8`) for use when creating child stories.

**Sequential Epic Blockers:** If this is NOT the first epic, add a blocker:

```bash
# Epic N is blocked by Epic N-1 (ensures epics are worked in order)
_bmad/bin/bd dep add {this_epic_id} {previous_epic_id} --type blocks
```

#### B. Epic Overview

Display:

- Epic number and title
- **Beads Epic ID** (from step A)
- Epic goal statement
- FRs covered by this epic
- Any NFRs or additional requirements relevant

#### C. Story Breakdown

Work with user to break down the epic into stories:

- Identify distinct user capabilities
- Ensure logical flow within the epic
- Size stories appropriately

#### D. Generate Each Story

For each story in the epic:

1. **Story Title**: Clear, action-oriented
2. **User Story**: Complete the As a/I want/So that format
3. **Acceptance Criteria**: Write specific, testable criteria

**AC Writing Guidelines:**

- Use Given/When/Then format
- Each AC should be independently testable
- Include edge cases and error conditions
- Reference specific requirements when applicable

#### E. Collaborative Review

After writing each story:

- Present the story to user
- Ask: "Does this story capture the requirement correctly?"
- "Is the scope appropriate for a single dev session?"
- "Are the acceptance criteria complete and testable?"

#### E. Create Beads Issue for Story

When story is approved, create the Beads issue:

```bash
# Create story issue as child of epic
_bmad/bin/bd create "{story_title}" \
  --parent {epic_beads_id} \
  --type task \
  --label "bmad:story" \
  --label "bmad:stage:backlog"
```

**Sequential Blockers:** If this is NOT the first story in the epic, add a blocker so stories are worked in order:

```bash
# Story N.M is blocked by Story N.(M-1)
_bmad/bin/bd dep add {this_story_id} {previous_story_id} --type blocks
```

Record the Beads Story ID (e.g., `proj-a3f8.1`) for inclusion in the document.

#### F. Append to Document

When story is approved:

- Append it to {outputFile} following template structure
- Use correct numbering (Epic N, Story M)
- **Include Beads Story ID** in the story header (e.g., `### Story 1.1: User Login [proj-a3f8.1]`)
- Maintain proper markdown formatting

### 4. Epic Completion

After all stories for an epic are complete:

- Display epic summary with **Beads Epic ID**
- Show count of stories created with their **Beads Story IDs**
- Verify all FRs for the epic are covered
- Confirm Beads issues created: `_bmad/bin/bd list --parent {epic_beads_id} --json`
- Get user confirmation to proceed to next epic

### 5. Repeat for All Epics

Continue the process for each epic in the approved list, processing them in order (Epic 1, Epic 2, etc.).

### 6. Final Document Completion

After all epics and stories are generated:

- Verify the document follows template structure exactly
- Ensure all placeholders are replaced
- Confirm all FRs are covered
- Check formatting consistency

## TEMPLATE STRUCTURE COMPLIANCE:

The final {outputFile} must follow this structure exactly:

1. **Overview** section with project name
2. **Requirements Inventory** with all three subsections populated
3. **FR Coverage Map** showing requirement to epic mapping
4. **Epic List** with approved epic structure
5. **Epic sections** for each epic (N = 1, 2, 3...)
   - Epic title and goal
   - All stories for that epic (M = 1, 2, 3...)
     - Story title and user story
     - Acceptance Criteria using Given/When/Then format

### 7. Present FINAL MENU OPTIONS

After all epics and stories are complete:

Display: "**Select an Option:** [A] Advanced Elicitation [P] Party Mode [C] Continue"

#### Menu Handling Logic:

- IF A: Execute {advancedElicitationTask}
- IF P: Execute {partyModeWorkflow}
- IF C: Save content to {outputFile}, update frontmatter, then only then load, read entire file, then execute {nextStepFile}
- IF Any other comments or queries: help user respond then [Redisplay Menu Options](#7-present-final-menu-options)

#### EXECUTION RULES:

- ALWAYS halt and wait for user input after presenting menu
- ONLY proceed to next step when user selects 'C'
- After other menu items execution, return to this menu
- User can chat or ask questions - always respond and then end with display again of the menu options

## CRITICAL STEP COMPLETION NOTE

ONLY WHEN [C continue option] is selected and [all epics and stories saved to document following the template structure exactly], will you then load and read fully `{nextStepFile}` to execute and begin final validation phase.

---

## 🚨 SYSTEM SUCCESS/FAILURE METRICS

### ✅ SUCCESS:

- All epics processed in sequence
- Stories created for each epic
- Template structure followed exactly
- All FRs covered by stories
- Stories appropriately sized
- Acceptance criteria are specific and testable
- Document is complete and ready for development
- **Beads epic/story issues created with correct hierarchy**
- **Sequential blockers established between stories**
- **Beads IDs recorded in document**

### ❌ SYSTEM FAILURE:

- Deviating from template structure
- Missing epics or stories
- Stories too large or unclear
- Missing acceptance criteria
- Not following proper formatting
- **Beads issues not created**
- **Missing Beads IDs in document**

**Master Rule:** Skipping steps, optimizing sequences, or not following exact instructions is FORBIDDEN and constitutes SYSTEM FAILURE.
