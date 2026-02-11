# Release Candidate Playbook

## Clean-Room Init Workflow

Run a clean-room test to remove all friction from a new beads initialization workflow.

Start from scratch, create a new project, run `bd init`, and exercise all core `bd` commands. Record every error and warning as issues. When done, work through the list by handing off fixes to sub-agents, opening a worktree fix branch per issue.

### Before Running Commands

- Confirm the OS/shell and use correct syntax (PowerShell vs Bash).
- Avoid overwriting the test binary with a mismatched build; if building, ensure the target OS matches the environment.
- Note that some commands may exit non-zero on warnings (ex: test data detection). Distinguish warnings from hard failures.

### In The Workflow

- Run `bd doctor` and `bd doctor --fix --yes` and capture any failures or misleading suggestions.
- If a suggestion is context-specific (ex: plugin install in CLI-only mode), capture it as friction.
- If a warning is expected for a brand-new repo (ex: missing upstream), document it and evaluate whether the copy should be softened.
