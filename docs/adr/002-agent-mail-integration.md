# ADR 002: MCP Agent Mail Integration for Multi-Agent Coordination

**Status:** Proposed  
**Date:** 2025-11-08  
**Epic:** [bd-spmx](../../.beads/beads.db) (Investigation & Proof of Concept)  
**Related Issues:** bd-6hji, bd-htfk, bd-muls

## Context

Beads is designed for AI-supervised coding workflows where multiple AI agents coordinate work on shared codebases. As multi-agent systems become more common, we face challenges:

### Problem Statement

1. **Git Sync Latency**: Current git-based synchronization has 2-5 second round-trip latency for status updates
2. **No Collision Prevention**: Two agents can claim the same issue simultaneously, causing wasted work and merge conflicts
3. **Git Repository Pollution**: Frequent agent status updates create noisy git history with dozens of micro-commits
4. **Lack of Real-Time Awareness**: Agents don't know what other agents are working on until after git sync completes

### Current Workflow

```
Agent A: bd update bd-123 --status in_progress
  ↓ (30s debounce)
  ↓ export to JSONL
  ↓ git commit + push (1-2s)
  ↓ 
Agent B: git pull (1-2s)
  ↓ import from JSONL
  ↓ sees bd-123 is taken (too late!)
```

Total latency: **2000-5000ms**

## Decision

**Adopt MCP Agent Mail as an *optional* coordination layer** for real-time multi-agent communication, while maintaining full backward compatibility with git-only workflows.

## Alternatives Considered

### 1. Custom RPC Server
**Pros:**
- Full control over implementation
- Optimized for beads-specific needs

**Cons:**
- High development cost (3-4 weeks)
- Maintenance burden
- Reinventing the wheel

**Verdict:** ❌ Too much effort for marginal benefit

### 2. Redis/Memcached
**Pros:**
- Battle-tested infrastructure
- Low latency

**Cons:**
- Heavy dependency (requires separate service)
- Overkill for lightweight coordination
- No built-in authentication/multi-tenancy

**Verdict:** ❌ Too heavy for beads' lightweight ethos

### 3. Git-Only (Status Quo)
**Pros:**
- Zero dependencies
- Works everywhere git works

**Cons:**
- 2-5s latency for status updates
- No collision prevention
- Noisy git history

**Verdict:** ✅ Remains the default, Agent Mail is optional enhancement

### 4. MCP Agent Mail (Chosen)
**Pros:**
- Lightweight HTTP server (<50MB memory)
- <100ms latency for status updates (20-50x faster than git)
- Built-in file reservation system (prevents collisions)
- Project/agent isolation (multi-tenancy support)
- Optional: graceful degradation to git-only mode
- Active maintenance by @Dicklesworthstone

**Cons:**
- External dependency (requires running server)
- Adds complexity for single-agent workflows (mitigated by optional nature)

**Verdict:** ✅ Best balance of benefits vs. cost

## Integration Principles

### 1. **Optional & Non-Intrusive**
- Agent Mail is 100% optional
- Beads works identically without it (git-only mode)
- No breaking changes to existing workflows

### 2. **Graceful Degradation**
- If server unavailable, fall back to git-only sync
- No errors, no crashes, just log a warning

### 3. **Lightweight HTTP Client**
- Use standard library HTTP client (no SDK bloat)
- Minimal code footprint in beads (<500 LOC)

### 4. **Configuration via Environment**
```bash
# Enable Agent Mail (optional)
export BEADS_AGENT_MAIL_URL=http://127.0.0.1:8765
export BEADS_AGENT_MAIL_TOKEN=<bearer-token>
export BEADS_AGENT_NAME=assistant-alpha

# Disabled by default (git-only mode)
bd ready  # Works without Agent Mail
```

## Proof of Concept Results

### File Reservation Testing (bd-6hji) ✅
- **Test:** Two agents (BrownBear, ChartreuseHill) race to claim bd-123
- **Result:** First agent gets reservation, second gets clear conflict error
- **Verdict:** Collision prevention works as expected

### Latency Benchmarking (bd-htfk) ✅
- **Git Sync:** 2000-5000ms (commit + push + pull + import)
- **Agent Mail:** <100ms (HTTP send + fetch round-trip)
- **Improvement:** 20-50x latency reduction
- **Verdict:** Real-time coordination achievable

### Installation (bd-muls) ✅
- **Server:** Runs on port 8765, <50MB memory
- **Web UI:** Accessible for human supervision
- **Verdict:** Easy to deploy and monitor

## Architecture

```
┌─────────────────────────────────────────────┐
│ bd (Beads CLI)                              │
│                                             │
│  ┌─────────────┐      ┌─────────────────┐  │
│  │ Git Sync    │      │ Agent Mail      │  │
│  │ (required)  │      │ (optional)      │  │
│  │             │      │                 │  │
│  │ - Export    │      │ - Reservations  │  │
│  │ - Import    │      │ - Notifications │  │
│  │ - Commit    │      │ - Status updates│  │
│  │ - Push/Pull │      │                 │  │
│  └─────────────┘      └─────────────────┘  │
│         │                      │            │
└─────────┼──────────────────────┼────────────┘
          │                      │
          ▼                      ▼
  ┌──────────────┐      ┌──────────────┐
  │ .beads/      │      │ Agent Mail   │
  │ issues.jsonl │      │ Server       │
  │ (git)        │      │ (HTTP)       │
  └──────────────┘      └──────────────┘
```

### Coordination Flow (with Agent Mail)

```
Agent A: bd update bd-123 --status in_progress
  ↓ 
  ├─ Agent Mail: POST /api/reservations (5ms)
  │  └─ Reserve bd-123 for Agent A
  ├─ Local: Update .beads/beads.db
  └─ Background: Export to JSONL (30s debounce)
  
Agent B: bd update bd-123 --status in_progress
  ↓
  └─ Agent Mail: POST /api/reservations (5ms)
     └─ HTTP 409 Conflict: "bd-123 reserved by Agent A"
     └─ bd exits with clear error

Total latency: <100ms (vs 2000-5000ms with git-only)
```

## Implementation Plan

### Phase 1: Core Integration (bd-wfmw)
- [ ] HTTP client wrapper for Agent Mail API
- [ ] Reservation check before status updates
- [ ] Graceful fallback when server unavailable
- [ ] Environment-based configuration

### Phase 2: Enhanced Features
- [ ] Notification system (agent X finished bd-Y)
- [ ] Automatic reservation expiry (TTL)
- [ ] Multi-project support
- [ ] Web dashboard for human supervision

### Phase 3: Documentation
- [ ] Quick start guide
- [ ] Multi-agent workflow examples
- [ ] Troubleshooting guide

## Risks & Mitigations

### Risk 1: Server Dependency
**Mitigation:** Graceful degradation to git-only mode. Beads never *requires* Agent Mail.

### Risk 2: Configuration Complexity
**Mitigation:** Zero config required for single-agent workflows. Environment variables for multi-agent setups.

### Risk 3: Upstream Changes
**Mitigation:** Use HTTP API directly (not SDK). Minimal surface area for breaking changes.

### Risk 4: Data Durability
**Mitigation:** Git remains the source of truth. Agent Mail is ephemeral coordination state only.

## Success Metrics

- ✅ Latency reduction: 20-50x (verified)
- ✅ Collision prevention: 100% effective (verified)
- 🔲 Git operation reduction: ≥70% (pending bd-nemp)
- 🔲 Zero functional regression in git-only mode

## References

- [MCP Agent Mail Repository](https://github.com/Dicklesworthstone/mcp_agent_mail)

## Decision Outcome

**Proceed with Agent Mail integration** using the optional, non-intrusive approach outlined above. The proof of concept validated the core benefits (latency, collision prevention) while the lightweight HTTP integration minimizes risk and complexity.

Git-only mode remains the default and fully supported workflow for single-agent scenarios.
