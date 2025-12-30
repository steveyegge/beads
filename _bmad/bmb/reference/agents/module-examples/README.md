# Module Agent Examples

Reference examples for module-integrated agents.

## About Module Agents

Module agents integrate with BMAD module workflows (BMM, CIS, BMB). They:

- Orchestrate multi-step workflows
- Use `_bmad` path variables
- Reference module-specific configurations
- Can be bundled into web bundlers with the other agents
- Participate in party mode with the modules other agents

## Examples

### security-engineer.agent.yaml (BMM Module)

**Sam** - Application Security Specialist

Demonstrates:

- Security-focused workflows (threat modeling, code review)
- OWASP compliance checking
- Integration with core party-mode workflow

### trend-analyst.agent.yaml (CIS Module)

**Nova** - Trend Intelligence Expert

Demonstrates:

- Creative/innovation workflows
- Trend analysis and opportunity mapping
- Integration with core brainstorming workflow

## Important Note

These are **hypothetical reference agents**. The workflows they reference (threat-model, trend-scan, etc.) may not exist. They serve as examples of proper module agent structure.

## Using as Templates

When creating module agents:

1. Copy relevant example
2. Update metadata (id, name, title, icon, module)
3. Rewrite persona for your domain
4. Replace menu with actual available workflows
5. Remove hypothetical workflow references
