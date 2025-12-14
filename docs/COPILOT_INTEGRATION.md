# Copilot Integration Design

Design decisions for GitHub Copilot integration with beads.

## Integration Approach

**Recommended: CLI + VS Code Tasks**

Beads uses a universal CLI approach that works across all AI assistants:
- `.github/copilot-instructions.md` for auto-loaded context
- VS Code tasks for session automation
- Direct CLI commands with `--json` flags

This approach is identical to Claude Code integration, ensuring consistent behavior across assistants.

## Why CLI Over Custom Extensions?

1. **Universal** - Same workflow for Copilot, Claude, Cursor, Windsurf, etc.
2. **Maintainable** - One codebase, not per-platform plugins
3. **Debuggable** - Shell commands are transparent and testable
4. **Context-efficient** - ~1-2k tokens vs large extension schemas

## VS Code Task Integration

Beads provides VS Code tasks in `.vscode/tasks.json`:

```json
{
  "label": "Beads: Session Startup",
  "type": "shell",
  "command": "bd prime",
  "runOptions": { "runOn": "folderOpen" }
},
{
  "label": "Beads: Ready",
  "type": "shell", 
  "command": "bd ready"
},
{
  "label": "Beads: Sync",
  "type": "shell",
  "command": "bd sync && git status"
}
```

These tasks:
- Auto-run on folder open (session startup)
- Provide quick access to common operations
- Work with any AI assistant in VS Code

## Instruction File Strategy

### What goes in `.github/copilot-instructions.md`:
- Project overview (what beads is)
- Critical rules (use bd, not TODO lists)
- Essential commands (ready, create, close, sync)
- Tech stack summary
- Link to detailed docs

### What stays in separate docs:
- Architecture details → `docs/COPILOT.md`
- Integration design → `docs/COPILOT_INTEGRATION.md`
- Full workflows → `AGENTS.md`
- CLI reference → `docs/CLI_REFERENCE.md`

**Rationale**: Copilot context windows vary by model. Keep instructions concise; let the agent read detailed docs when needed.

## Copilot-Specific Considerations

### Context Window Management

Optimize instructions for smaller context windows:
- Concise instructions (avoid verbose explanations)
- Code examples over prose
- Links to detailed docs rather than inline content

### No MCP Equivalent

Copilot doesn't have MCP protocol. For "tool-like" behavior:
- Use `run_in_terminal` tool with `bd` commands
- Let Copilot read `--json` output and act on it
- VS Code tasks for common operations

### When to Use bd vs Other Tracking

**Use bd when:**
- Work spans multiple sessions
- Tasks have dependencies or blockers
- Need to resume work after days/weeks

**Don't need bd for:**
- Single-session, linear tasks
- Simple checklists that complete immediately

**When in doubt**: Use bd. Better to have persistent context.

## Comparison with Claude Integration

| Feature | Claude | Copilot |
|---------|--------|---------|
| Auto-loaded file | `CLAUDE.md` | `.github/copilot-instructions.md` |
| Tool protocol | MCP | Terminal commands |
| Session hooks | SessionStart | VS Code tasks |
| Skills system | `.claude/skills/` | Not available |

Despite these differences, the **workflow is identical**:
1. `bd ready` - find work
2. `bd update` - claim work
3. Implement changes
4. `bd close` - complete work
5. `bd sync` - persist to git

## File Organization

```
beads/
├── .github/
│   └── copilot-instructions.md  # Auto-loaded by Copilot
├── docs/
│   ├── COPILOT.md               # Architecture guide
│   ├── COPILOT_INTEGRATION.md   # This file
│   ├── CLAUDE.md                # Claude architecture guide
│   └── CLAUDE_INTEGRATION.md    # Claude design decisions
├── CLAUDE.md                    # Claude auto-loaded (repo root)
└── AGENTS.md                    # Universal workflow (both use this)
```

## Testing Integration

Verify the integration works:

```bash
# 1. Open repo in VS Code with Copilot
# 2. Check that session startup task ran
# 3. Ask Copilot: "What issues are ready to work on?"
# 4. Verify it runs `bd ready --json`
# 5. Ask Copilot to create an issue
# 6. Verify it uses bd create with --description
```

## Future Considerations

### Copilot Prompt Files
When Copilot supports `.github/prompts/*.md`, consider:
- Moving workflow guidance to prompt files
- Creating task-specific prompts (debugging, testing, etc.)

### Copilot Extensions
When Copilot Extensions mature:
- Evaluate native tool integration
- Compare context efficiency vs CLI approach
- Maintain CLI as fallback for universality
