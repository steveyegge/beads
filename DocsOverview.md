# Beads Documentation Overview

Documentation organized by scope and objective.

---

## 📘 **Getting Started** (New Users)
| File | Purpose |
|------|---------|
| [README.md](README.md) | Project overview, what beads is |
| [docs/QUICKSTART.md](docs/QUICKSTART.md) | 5-minute setup guide |
| [docs/INSTALLING.md](docs/INSTALLING.md) | Installation methods (brew, go install, etc.) |
| [docs/UNINSTALLING.md](docs/UNINSTALLING.md) | Clean removal |
| [docs/FAQ.md](docs/FAQ.md) | Common questions |

---

## 🤖 **AI Agent Integration** (Primary Use Case)
| File | Purpose |
|------|---------|
| [AGENTS.md](AGENTS.md) | **Master guide** for AI agents using beads |
| [AGENT_INSTRUCTIONS.md](AGENT_INSTRUCTIONS.md) | Detailed dev procedures for agents |
| [docs/BEADS_HARNESS_PATTERN.md](docs/BEADS_HARNESS_PATTERN.md) | Pattern for agent harnesses |

### Claude-Specific
| File | Purpose |
|------|---------|
| [CLAUDE.md](CLAUDE.md) | Claude auto-loaded instructions |
| [docs/CLAUDE.md](docs/CLAUDE.md) | Claude architecture guide |
| [docs/CLAUDE_INTEGRATION.md](docs/CLAUDE_INTEGRATION.md) | Claude integration design |
| [docs/AIDER_INTEGRATION.md](docs/AIDER_INTEGRATION.md) | Aider integration |

### Copilot-Specific
| File | Purpose |
|------|---------|
| [.github/copilot-instructions.md](.github/copilot-instructions.md) | Copilot auto-loaded instructions |
| [docs/COPILOT.md](docs/COPILOT.md) | Copilot architecture guide |
| [docs/COPILOT_INTEGRATION.md](docs/COPILOT_INTEGRATION.md) | Copilot integration design |

---

## 📬 **Agent Mail** (Multi-Agent Coordination)
| File | Purpose |
|------|---------|
| [docs/AGENT_MAIL.md](docs/AGENT_MAIL.md) | Full Agent Mail documentation |
| [docs/AGENT_MAIL_QUICKSTART.md](docs/AGENT_MAIL_QUICKSTART.md) | 5-minute Agent Mail setup |
| [docs/AGENT_MAIL_DEPLOYMENT.md](docs/AGENT_MAIL_DEPLOYMENT.md) | Production deployment |
| [docs/AGENT_MAIL_MULTI_WORKSPACE_SETUP.md](docs/AGENT_MAIL_MULTI_WORKSPACE_SETUP.md) | Multi-workspace config |

---

## 🔧 **CLI Reference** (Command Documentation)
| File | Purpose |
|------|---------|
| [docs/CLI_REFERENCE.md](docs/CLI_REFERENCE.md) | Complete command reference |
| [commands/](commands/) | Per-command docs (create, update, close, etc.) |

**Key commands/ files:**
- `create.md`, `update.md`, `close.md`, `delete.md` - Issue lifecycle
- `ready.md`, `blocked.md`, `list.md`, `show.md` - Querying
- `dep.md`, `epic.md` - Dependencies & hierarchies
- `sync.md`, `import.md`, `export.md` - Data sync
- `daemon.md`, `daemons.md` - Background processes
- `init.md`, `prime.md` - Setup & session start

---

## 🏗️ **Architecture & Internals**
| File | Purpose |
|------|---------|
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | System design overview |
| [docs/INTERNALS.md](docs/INTERNALS.md) | Implementation details |
| [docs/ADAPTIVE_IDS.md](docs/ADAPTIVE_IDS.md) | Hash-based ID system |
| [docs/COLLISION_MATH.md](docs/COLLISION_MATH.md) | ID collision probability |
| [docs/DAEMON.md](docs/DAEMON.md) | Daemon architecture |
| [docs/DELETIONS.md](docs/DELETIONS.md) | Deletion tracking system |
| [docs/EXCLUSIVE_LOCK.md](docs/EXCLUSIVE_LOCK.md) | Locking mechanism |
| [docs/ROUTING.md](docs/ROUTING.md) | Request routing |

---

## 🔀 **Git Integration**
| File | Purpose |
|------|---------|
| [docs/GIT_INTEGRATION.md](docs/GIT_INTEGRATION.md) | How beads uses git |
| [docs/PROTECTED_BRANCHES.md](docs/PROTECTED_BRANCHES.md) | Working with protected branches |
| [docs/WORKTREES.md](docs/WORKTREES.md) | Git worktree support |

---

## 🌐 **Multi-Repo & Team Workflows**
| File | Purpose |
|------|---------|
| [docs/MULTI_REPO_AGENTS.md](docs/MULTI_REPO_AGENTS.md) | Agents across multiple repos |
| [docs/MULTI_REPO_HYDRATION.md](docs/MULTI_REPO_HYDRATION.md) | Cross-repo data sync |
| [docs/MULTI_REPO_MIGRATION.md](docs/MULTI_REPO_MIGRATION.md) | Migrating multi-repo setups |
| [docs/HUMAN_WORKFLOW.md](docs/HUMAN_WORKFLOW.md) | Human-centric workflows |

---

## ⚙️ **Configuration & Customization**
| File | Purpose |
|------|---------|
| [docs/CONFIG.md](docs/CONFIG.md) | Configuration options |
| [docs/LABELS.md](docs/LABELS.md) | Label system |
| [docs/EXTENDING.md](docs/EXTENDING.md) | Extending beads |
| [docs/PLUGIN.md](docs/PLUGIN.md) | Plugin architecture |

---

## 🧪 **Testing & Performance**
| File | Purpose |
|------|---------|
| [docs/TESTING.md](docs/TESTING.md) | Test suite documentation |
| [docs/README_TESTING.md](docs/README_TESTING.md) | Testing README docs |
| [docs/PERFORMANCE_TESTING.md](docs/PERFORMANCE_TESTING.md) | Perf benchmarks |
| [docs/LINTING.md](docs/LINTING.md) | Code linting |

---

## 🚀 **Development & Contributing**
| File | Purpose |
|------|---------|
| [CONTRIBUTING.md](CONTRIBUTING.md) | How to contribute |
| [RELEASING.md](RELEASING.md) | Release process |
| [docs/RELEASING.md](docs/RELEASING.md) | Detailed release docs |
| [docs/ERROR_HANDLING.md](docs/ERROR_HANDLING.md) | Error patterns |
| [docs/ATTRIBUTION.md](docs/ATTRIBUTION.md) | Credits |
| [SECURITY.md](SECURITY.md) | Security policy |

---

## 🐛 **Troubleshooting**
| File | Purpose |
|------|---------|
| [docs/TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md) | Common issues & fixes |
| [docs/ANTIVIRUS.md](docs/ANTIVIRUS.md) | AV false positives |
| [docs/ADVANCED.md](docs/ADVANCED.md) | Advanced usage/edge cases |

---

## 📂 **Examples** (Working Code)
| Directory | Purpose |
|-----------|---------|
| `examples/python-agent/` | Python agent integration |
| `examples/go-agent/` | Go agent integration |
| `examples/bash-agent/` | Bash scripting |
| `examples/claude-desktop-mcp/` | Claude Desktop MCP setup |
| `examples/github-import/` | Import from GitHub Issues |
| `examples/jira-import/` | Import from Jira |
| `examples/team-workflow/` | Team collaboration patterns |
| `examples/contributor-workflow/` | OSS contributor setup |
| `examples/protected-branch/` | Protected branch handling |
| `examples/git-hooks/` | Git hook examples |
| `examples/startup-hooks/` | Session startup automation |

---

## 📊 **Summary by Reader Type**

| You Are... | Start With |
|------------|-----------|
| **New user** | README → QUICKSTART → CLI_REFERENCE |
| **AI agent developer (Claude)** | AGENTS.md → CLAUDE_INTEGRATION → examples/ |
| **AI agent developer (Copilot)** | AGENTS.md → COPILOT_INTEGRATION → examples/ |
| **Contributor** | CONTRIBUTING → AGENT_INSTRUCTIONS → TESTING |
| **Multi-agent coordinator** | AGENT_MAIL_QUICKSTART → MULTI_REPO_AGENTS |
| **Troubleshooting** | TROUBLESHOOTING → FAQ → DAEMON |
