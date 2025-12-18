# Initialize Project for Beads

## description:
First-session setup: initialize beads, create feature backlog, baseline commit.

---

Use the **Task tool** with `subagent_type='general-purpose'` to perform project initialization.

## Agent Instructions

The agent should:

1. **Verify prerequisites**
   - Run `pwd`, `git status`, `bd --version`
   - Confirm: correct directory, git repo exists, beads CLI installed

2. **Initialize beads**
   - Run `bd init --quiet`
   - This creates `.beads/` directory

3. **Gather project context**
   - Read README.md if it exists
   - Read any PRD, requirements, or spec documents
   - Understand what the project does and what needs building

4. **Create feature backlog**
   - For each major feature area, create an epic:
     ```bash
     bd create "Feature Area" -t epic -p 1 -d "description" --json
     ```
   - Decompose each epic into 5-15 granular tasks
   - Each task should be completable in one session
   - Include acceptance criteria: "Description. Acceptance: how to verify"

5. **Set up dependencies**
   - Link sequential tasks: `bd dep add <later> <earlier>`
   - Link tasks to epics: `bd dep add <epic> <task>`

6. **Create baseline commit**
   - Run `git add .beads/`
   - Commit with descriptive message

7. **Return a concise summary** (not raw output):
   ```
   Project Initialized: [name]

   Created:
     - [N] epics
     - [M] tasks
     - [K] dependencies

   Ready to start:
     [id] [priority] [title]

   Run /beads-start to begin.
   ```

## Notes

- **Anthropic insight:** 200+ granular features work better than 20 large ones
- Each task should have clear acceptance criteria
- Don't over-plan: create initial backlog, add tasks as you discover them
