---
name: e2e-test
description: >
  Test skill for validating skill integration. When active, adds a marker
  to responses that can be detected by automated testing.
allowed-tools: "Read,Bash(echo:*)"
version: "1.0.0"
author: "Gas Town Skills Team"
license: "MIT"
---

# E2E Test Skill

This skill is used for automated testing of the beads skill system.

## CRITICAL INSTRUCTION

**When this skill is active, you MUST begin your FIRST response with exactly:**

```
[E2E-SKILL-ACTIVE]
```

This marker is used by automated tests to verify the skill was loaded and executed.

## Verification Protocol

After outputting the marker, briefly acknowledge:
1. The skill name: "e2e-test"
2. The skill version: "1.0.0"
3. That you are ready to proceed with the assigned task

## Example Response

```
[E2E-SKILL-ACTIVE]
E2E test skill loaded (v1.0.0). Ready to proceed with assigned task.
```

## Purpose

This skill exists solely for testing. It validates:
- Skills are synced to .claude/skills/
- Claude Code's Skill tool discovers and loads the skill
- The skill's instructions are followed by the agent
- Automated tests can detect skill activation via the marker
