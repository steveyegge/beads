# OpenClaw + Claude Integration Specification

**Status:** Design Phase  
**Date:** 2026-01-30  
**Scope:** Running OpenClaw with Claude Code/Codex as the AI engine  
**Goal:** Self-hosted AI assistant on WhatsApp/Telegram/Discord using Claude models

---

## How OpenClaw Works (Architecture)

### Core Components

```
┌─────────────────────────────────────────────────────────┐
│                    OPENCLAW GATEWAY                      │
│                  (Single daemon process)                 │
├─────────────────────────────────────────────────────────┤
│                                                          │
│  Messaging Channels Layer (owns all connections)        │
│  ├─ WhatsApp (via Baileys library)                      │
│  ├─ Telegram (via grammY)                               │
│  ├─ Discord (via discord.js)                            │
│  ├─ Slack (via Bolt)                                    │
│  └─ Signal, iMessage, Matrix, etc.                      │
│                                                          │
│  Agent Loop (processes messages)                        │
│  ├─ Route inbound message to agent                      │
│  ├─ Agent calls tools (bash, browser, canvas, etc.)     │
│  ├─ Model generates response                            │
│  └─ Send back to channel                                │
│                                                          │
│  WebSocket API Server (control plane)                   │
│  ├─ CLI clients connect here                            │
│  ├─ macOS/iOS/Android nodes connect here                │
│  ├─ Web UI (Control UI, Dashboard) connects here        │
│  └─ Automations webhook triggers                        │
│                                                          │
│  Tool Execution Engine                                  │
│  ├─ Bash (with optional sandboxing)                     │
│  ├─ Browser (Playwright, can capture screenshots)       │
│  ├─ Canvas (Live HTML rendering)                        │
│  ├─ Cron (scheduled tasks)                              │
│  ├─ Nodes (voice wake, camera, location)                │
│  └─ Sessions (multi-turn context)                       │
│                                                          │
└─────────────────────────────────────────────────────────┘
```

### Message Flow

```
1. User sends WhatsApp message
   ↓
2. Gateway receives via Baileys → stores in inbox
   ↓
3. Inbound listener extracts:
   - Text + media (images, audio, documents)
   - Quoted replies (context)
   - Sender E.164 phone number
   ↓
4. Pairing check (if unknown sender → get approval code)
   ↓
5. Route to agent (based on sender, group, etc.)
   ↓
6. Agent loop starts:
   - Load session context
   - System prompt + tools + model
   - Call Claude (or OpenAI, or local model)
   - Claude decides to call tools (bash, browser, etc.)
   - Execute tools in sandbox or host
   ↓
7. Claude generates response (text + optional media)
   ↓
8. Response chunked + sent back to WhatsApp
   ↓
9. User receives response in WhatsApp
```

### Key Concept: Agents vs. Channels

```
AGENT (defines AI behavior):
  - System prompt + model choice
  - Tools available (sandbox, bash, browser, etc.)
  - Memory/session context
  - Skills (workflows, custom logic)

CHANNEL (defines communication):
  - WhatsApp, Telegram, Discord, etc.
  - Inbound/outbound message routing
  - Media handling (images, audio, etc.)
  - Pairing/allowlist (security)

ROUTING:
  - Channel receives message
  - Looks up agent via config (sender → agent mapping)
  - Passes to agent
  - Agent processes, returns response
  - Channel sends response back
```

### What OpenClaw Uses From Claude

```
REQUIRED:
  ✓ Claude model (via API key or OAuth)
    - claude-opus-4-1 (recommended for agentic tasks)
    - claude-opus-3.5-sonnet (faster, cheaper)
  ✓ Tools (bash, browser, etc. provided by OpenClaw)

WHAT CLAUDE DOES:
  ✓ Receives system prompt (OpenClaw defines it)
  ✓ Sees user message + conversation context
  ✓ Decides which tools to call
  ✓ Reads tool output
  ✓ Generates response
  
WHAT OPENCLAW DOES:
  ✓ Manages Claude API calls
  ✓ Implements tools (bash, browser, canvas, etc.)
  ✓ Routes messages between channels and Claude
  ✓ Maintains session state
  ✓ Handles pairing + security
```

---

## Part 1: Getting OpenClaw Running (Quick Start)

### 1.1 Install OpenClaw

```bash
# macOS / Linux / WSL2
curl -fsSL https://openclaw.ai/install.sh | bash

# Or with pnpm
npm install -g pnpm
pnpm add -g openclaw
```

### 1.2 Run Onboarding Wizard

```bash
openclaw onboard
```

**Choices during wizard:**
```
1. Gateway: Local (127.0.0.1:18789)
2. Auth: Anthropic API key (or oauth.json from Claude Code)
3. Models: claude-opus-4-1 (recommended)
4. Channels: WhatsApp
   └─ Login: openclaw channels login
   └─ Scan QR via WhatsApp → Settings → Linked Devices
5. Security: DM pairing (default, safe)
6. Daemon: Yes (install systemd/launchd service)
```

### 1.3 Start Gateway

```bash
# If daemon installed:
openclaw gateway status
# Should show: running

# Manual start (foreground):
openclaw gateway start
```

### 1.4 Test It

```bash
# Send yourself a message on WhatsApp
# Message: "Hi, what's the date?"

# Should respond with date + time in WhatsApp

# Or test via CLI:
openclaw message send --agent default --text "Hello"
```

---

## Part 2: Integration with Claude Code/Codex

### 2.1 Share Credentials Between Claude Code and OpenClaw

**Option A: Reuse Claude Code OAuth (Recommended)**

```bash
# In Claude Code, you have:
~/.claude/auth/oauth.json  (Anthropic OAuth)

# OpenClaw can read the same:
openclaw configure --import-oauth ~/.claude/auth/oauth.json

# Or set ANTHROPIC_API_KEY:
export ANTHROPIC_API_KEY="sk-ant-..."
openclaw gateway start
```

**Option B: Use Separate API Key**

```bash
# Get API key from https://console.anthropic.com
# Set in OpenClaw config:
openclaw configure --section providers
# Choose: anthropic
# Paste: API key
```

### 2.2 Skills for OpenClaw (In Claude Code)

Skills in OpenClaw work like superpowers skills. Create:

```
~/.claude/skills/openclaw-*
├── SKILL.md  (when to use, how OpenClaw works, commands)
└── RESOURCES/
    ├── WHATSAPP_SETUP.md  (WhatsApp pairing guide)
    ├── TOOLS_REFERENCE.md (bash, browser, canvas, nodes)
    └── AGENT_CONFIG.md    (system prompt, routing, memory)
```

**Skill 1: openclaw-gateway**
- Purpose: Manage OpenClaw daemon (start, stop, logs, health)
- Commands: `openclaw gateway start|stop|status|logs`
- When to use: Troubleshooting, checking if running

**Skill 2: openclaw-whatsapp**
- Purpose: Configure WhatsApp integration
- Commands: `openclaw channels login`, `openclaw pairing approve`
- When to use: Setting up WhatsApp, approving new senders

**Skill 3: openclaw-agents**
- Purpose: Understand agent configuration
- Contents: How to set system prompts, route channels to agents, multi-agent setup
- When to use: Customizing AI behavior

**Skill 4: openclaw-tools**
- Purpose: Reference for all tools available
- Contents: bash (command execution), browser (web access), canvas (live HTML), nodes (media/voice)
- When to use: Understanding what the agent can do

### 2.3 What Skills Need (The Spec)

If we create skills for OpenClaw, they need:

```
Each skill file includes:

1. FRONTMATTER (YAML)
   ---
   name: openclaw-<feature>
   description: "Brief description"
   categories: ["Integration", "OpenClaw", "Claude"]
   ---

2. OVERVIEW (2-3 sentences)
   What this skill helps with

3. WHEN TO USE (bullet list)
   - Specific scenarios
   - Common problems

4. PROCESS (numbered steps)
   Step-by-step for setting up or troubleshooting

5. COMMANDS (bash code blocks)
   Real commands with example output

6. INTEGRATION (how it fits)
   - Works with: session-recovery, multi-agent-sync
   - Hooks into: gateway startup, channel setup

7. TROUBLESHOOTING (Q&A format)
   Common errors + solutions

8. RELATED (links to other docs)
   RESOURCES/ files, OpenClaw docs, etc.
```

---

## Part 3: Running OpenClaw in Claude Code

### 3.1 Workflow: Claude Code ↔ OpenClaw

```
Claude Code Session:
  1. Run openclaw gateway (manual or already running)
  2. Use skill: openclaw-gateway to check status
  3. Create WhatsApp skill setup (via skill: openclaw-whatsapp)
  4. Test: openclaw message send --text "test"

OpenClaw Running:
  5. User sends WhatsApp message
  6. OpenClaw → Claude API → Response
  7. Response sent back to WhatsApp
  8. Claude Code can monitor via: openclaw status
```

### 3.2 System Requirements

```
Hardware:
  ✓ Any machine (laptop, homelab, VPS)
  ✓ Needs internet (for Claude API + WhatsApp)
  ✓ Stays on 24/7 if you want 24/7 WhatsApp responses

Software:
  ✓ Node ≥22 (required)
  ✓ WhatsApp account (separate number recommended)
  ✓ Anthropic API key (or oauth.json)
  ✓ Optional: Brave Search API (for web search)

Networking:
  ✓ Localhost: just works (127.0.0.1:18789)
  ✓ Remote: SSH tunnel or Tailscale Serve
```

### 3.3 Example: Personal Assistant Setup

```bash
# 1. Install OpenClaw
curl -fsSL https://openclaw.ai/install.sh | bash

# 2. Run wizard (choose: WhatsApp, claude-opus-4-1)
openclaw onboard

# 3. Login WhatsApp
openclaw channels login
# Scan QR code

# 4. Add your number to allowlist
# Edit: ~/.openclaw/credentials/whatsapp/config.json
# Add: "allowFrom": ["+1234567890"]  (your number)

# 5. Start gateway daemon
openclaw gateway start

# 6. Test from WhatsApp
# Send message: "What are the top 5 trending topics today?"
# Wait ~5 seconds
# Claude responds via WhatsApp

# 7. Try tools
# "Can you take a screenshot of my desktop?"
# OpenClaw executes: screenshot tool → sends image to WhatsApp
```

---

## Part 4: Multi-Agent Setup (Optional But Powerful)

### 4.1 Route Different Channels to Different Agents

```json
{
  "agents": {
    "main": {
      "system": "You are a general assistant...",
      "model": "claude-opus-4-1",
      "tools": ["bash", "browser", "canvas"]
    },
    "coding": {
      "system": "You are a software engineer...",
      "model": "claude-opus-4-1",
      "tools": ["bash", "browser"]
    }
  },
  "channels": {
    "whatsapp": {
      "accounts": {
        "personal": {
          "routes": {
            "+1234567890": "main",  // Personal number → main agent
            "+1-coding-friend": "coding"  // Coding friend → coding agent
          }
        }
      }
    }
  }
}
```

### 4.2 Multi-Agent Use Cases

```
Use Case 1: Personal Assistant
  Route your number → general agent (all tools)

Use Case 2: Coding Help
  Route coder friend's number → coding agent (bash + browser only)

Use Case 3: Teams
  Route team group → team agent (sandboxed, limited tools)

Use Case 4: Work + Personal
  Work number → work agent (email, calendar tools)
  Personal number → personal agent (general)
```

---

## Part 5: Skills vs. Tools vs. Models (Terminology)

### Understanding the Layers

```
LAYERS (bottom → top):

Layer 1: MODEL
  Claude (API) ← you provide API key
  Does: Think, reason, decide actions
  You control via: System prompt

Layer 2: TOOLS
  bash, browser, canvas, cron, nodes (camera, voice)
  Do: Execute what Claude decides
  You control via: Tool policies, sandbox settings

Layer 3: AGENTS
  Combination of: Model + Tools + System prompt + Memory
  Do: Manage one conversation thread
  You control via: config.json agent definitions

Layer 4: CHANNELS
  WhatsApp, Telegram, Discord, etc.
  Do: Send/receive messages on platforms
  You control via: config.json channel definitions

Layer 5: SKILLS (Claude Code)
  Documentation + guides for using OpenClaw
  Do: Help Claude Code users understand OpenClaw
  You create via: SKILL.md files in ~/.claude/skills/

EXAMPLE:
  Channel (WhatsApp) → Agent (main) → Model (Claude) + Tools (bash, browser)
  Help user understand: Skill (openclaw-agents)
```

---

## Part 6: Creating the Spec in mvp-ideas

### 6.1 Files to Create

If we want to run OpenClaw in Claude projects, create specs:

```
specbeads/specs/
├── OPENCLAW_ARCHITECTURE_GUIDE.md
│   └─ How it works, message flow, agent routing
├── OPENCLAW_SETUP_SPEC.md
│   └─ Install, config, WhatsApp pairing, testing
├── OPENCLAW_CLAUDE_SKILLS_SPEC.md
│   └─ 4 skills to create for Claude Code
└── OPENCLAW_INTEGRATION_SPEC.md
    └─ How OpenClaw + multi-agent-sync + shadowbook work together
```

### 6.2 Skills to Create (In Claude Code)

After OpenClaw is running, create these in `~/.claude/skills/`:

```
.claude/skills/
├── openclaw-gateway/SKILL.md
│   └─ Manage daemon, check health, troubleshoot
├── openclaw-whatsapp/SKILL.md
│   └─ Setup WhatsApp, pairing, allowlist, media
├── openclaw-agents/SKILL.md
│   └─ Configure agents, routing, system prompts
└── openclaw-tools/SKILL.md
    └─ Reference: bash, browser, canvas, cron, nodes
```

---

## Part 7: How It Fits Together

### OpenClaw + Multi-Agent-Sync + Shadowbook

```
FULL STACK:

┌─────────────────────────────────────────┐
│        OpenClaw (WhatsApp)               │
│  ├─ Gateway (daemon)                    │
│  ├─ Claude model + tools                │
│  ├─ Multi-agent routing                 │
│  └─ Skills (claude-integrated)          │
└──────────┬──────────────────────────────┘
           │
┌──────────▼──────────────────────────────┐
│     Multi-Agent-Sync (Skills Mgmt)       │
│  ├─ Audit: All agents have same skills  │
│  ├─ Validate: No broken skills          │
│  ├─ Sync: Copy between agents           │
│  └─ Heal: Fix frontmatter errors        │
└──────────┬──────────────────────────────┘
           │
┌──────────▼──────────────────────────────┐
│      Shadowbook (Spec Tracking)          │
│  ├─ Track narratives (specs)            │
│  ├─ Link to beads (issues/work)         │
│  ├─ Detect spec drift                   │
│  └─ Pre-flight checks (via preflight)   │
└─────────────────────────────────────────┘

WORKFLOW:
1. Shadowbook: bd create "OpenClaw agent setup" --spec-id specs/openclaw.md
2. Work: Set up OpenClaw, create skills
3. Multi-Agent-Sync: /multi-agent-sync audit (ensure all agents synced)
4. Test: Send WhatsApp message, get Claude response
5. Close: bd close <id> when done
```

---

## Part 8: Decision: What To Do Next

### Option A: Create Full Spec (4 files)
```
├── OPENCLAW_ARCHITECTURE_GUIDE.md (this info)
├── OPENCLAW_SETUP_SPEC.md (install + WhatsApp setup)
├── OPENCLAW_CLAUDE_SKILLS_SPEC.md (create 4 skills)
└── OPENCLAW_INTEGRATION_SPEC.md (integrate with multi-agent-sync + shadowbook)

Time: 1-2 sessions to create specs
Then: 2-3 sessions to execute (install, setup, test skills)
```

### Option B: Quick Start (Just Run It)
```
1. curl -fsSL https://openclaw.ai/install.sh | bash
2. openclaw onboard
3. openclaw channels login
4. Send WhatsApp message, get Claude response
5. Done

Time: 30 minutes
No specs needed, but skills would be useful for reference
```

### Option C: Integrate with Your Projects
```
1. Run OpenClaw on VPS (always-on)
2. Use multi-agent-sync to sync OpenClaw skills to Claude Code
3. Use shadowbook to track OpenClaw setup work
4. Create specs for future reference

Time: 3-4 sessions
```

---

## Recommendation

**Do Option B + Option A:**

1. **This session:** Install OpenClaw, get WhatsApp working (30 min)
2. **Next session:** Create OPENCLAW_SETUP_SPEC.md (reference)
3. **Later:** Create 4 skills for Claude Code (openclaw-gateway, openclaw-whatsapp, openclaw-agents, openclaw-tools)
4. **Eventually:** Integrate with multi-agent-sync + shadowbook

**Benefits:**
- Get hands-on experience with OpenClaw first
- Then document what you learned in specs
- Create reusable skills for Claude Code
- Integrate with existing multi-agent-sync and shadowbook workflow

---

## What You Need (Minimum)

```
REQUIRED:
  ✓ Anthropic API key (or oauth.json from Claude Code)
  ✓ Node ≥22
  ✓ WhatsApp account (separate number recommended)
  ✓ 30 minutes setup time

OPTIONAL:
  ✓ Spare/old phone (for dedicated WhatsApp number)
  ✓ eSIM service (local, recommended)
  ✓ Tailscale account (for remote access)
  ✓ Brave Search API key (for web search in agent)
```

---

**Document Version:** 1.0  
**Status:** Ready for execution  
**Updated:** 2026-01-30
