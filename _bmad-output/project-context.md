---
project_name: 'Beads Documentation Strategy'
user_name: 'Ubuntu'
date: '2025-12-30'
sections_completed: ['technology_stack', 'language_rules', 'framework_rules', 'testing_rules', 'code_quality', 'workflow_rules', 'critical_rules']
status: 'complete'
rule_count: 48
optimized_for_llm: true
---

# Project Context for AI Agents

_This file contains critical rules and patterns that AI agents must follow when implementing code in this project. Focus on unobvious details that agents might otherwise miss._

---

## Technology Stack & Versions

### Go CLI (beads/bd)
- **Go**: 1.24.0 (toolchain 1.24.11) - REQUIRED
- **SQLite**: ncruces/go-sqlite3 v0.30.4 (WASM-based, no CGO)
- **CLI Framework**: spf13/cobra v1.10.2 + viper v1.21.0
- **TUI**: charmbracelet/huh v0.8.0 + lipgloss v1.1.0
- **AI Integration**: anthropic-sdk-go v1.19.0

### Documentation Site (website/)
- **Node.js**: >=20.0 REQUIRED (upstream requirement)
- **Package manager**: npm or yarn (CI uses npm)
- **Docusaurus**: 3.9.2 with classic preset
- **React**: 19.0.0
- **TypeScript**: ~5.6.2
- **Note**: Bun may work locally but is not officially supported

### Critical Version Constraints
- Go 1.24+ required (uses toolchain directive)
- Node 20+ enforced in package.json engines
- Docusaurus v4 future flag enabled (`future: { v4: true }`)
- SQLite via WASM eliminates CGO dependency (pure Go builds)

### AI Agent Instructions
- Go code: use `go build`, `go test` commands
- Website: always use `npm install` / `npm run build`
- Never assume Bun compatibility in automation

## Language-Specific Rules

### Go Code Rules

**Error Handling:**
- Always check errors EXCEPT for documented exclusions in `.golangci.yml`:
  - `(*sql.DB).Close`, `(*sql.Rows).Close`, `(*sql.Tx).Rollback`
  - `os.RemoveAll`, `os.Remove`, `os.Setenv`, `os.Chdir`, `os.MkdirAll`
  - `(*os.File).Close`, `fmt.Sscanf`
- Deferred closes don't need error checks (G104 excluded)

**Security (gosec) Exclusions:**
- G304 (file inclusion via variable) - allowed in tests and specific paths
- G306 (file permissions) - 0644 for JSONL/logs, 0700 for git hooks
- G204 (subprocess) - allowed for validated git/CLI commands
- G201 (SQL formatting) - allowed for IN clause expansion with placeholders

**Package Organization:**
- `cmd/bd/` - CLI commands (one file per command)
- `internal/` - Private packages (types, storage, git, etc.)
- Test files: `*_test.go` in same directory

**Naming Conventions:**
- Files: lowercase with underscores (`daemon_sync.go`)
- Packages: single lowercase word (`types`, `storage`, `git`)
- Interfaces: `-er` suffix (`Storage`, `Importer`)

**Struct Organization:**
- Group fields with comments: `// ===== Section Name =====`
- JSON tags required for exported fields
- Use `omitempty` except for valid zero values (e.g., Priority 0)

### TypeScript Rules (website/)

**Configuration:**
- Extends `@docusaurus/tsconfig`
- `baseUrl: "."` for imports
- Exclude: `.docusaurus`, `build`

**Import Patterns:**
- Use Docusaurus type imports: `import type {Config} from '@docusaurus/types'`
- Prism themes: `import {themes as prismThemes} from 'prism-react-renderer'`

## Framework-Specific Rules

### Cobra CLI Framework (Go)

**Command Structure:**
- Each command in separate file: `cmd/bd/<command>.go`
- Use `cobra.Command` with `RunE` for error returns
- Short description: one line, no period
- Long description: multi-line with examples

**Flag Patterns:**
- Persistent flags on root command
- Local flags on specific commands
- Use viper for config binding

**Subcommand Organization:**
- Group related commands: `bd dep add`, `bd dep remove`
- Use `AddCommand()` in init functions

### Docusaurus Framework (website/)

**Configuration (docusaurus.config.ts):**
- `routeBasePath: '/'` - docs as homepage
- `future: { v4: true }` - v4 compatibility enabled
- `onBrokenLinks: 'warn'` - non-blocking broken links

**Content Organization (Diátaxis):**
| Directory | Category | Purpose |
|-----------|----------|---------|
| `getting-started/` | Tutorial | Learning-oriented |
| `workflows/`, `recovery/` | How-to | Task-oriented |
| `cli-reference/`, `reference/` | Reference | Information-oriented |
| `core-concepts/`, `architecture/` | Explanation | Understanding-oriented |

**File Naming:**
- Lowercase kebab-case: `getting-started.md`, `cli-reference.md`
- Index files for sections: `index.md`

**AI Discovery Meta Tags:**
- `ai-terms`: points to llms-full.txt
- `llms-full`: full documentation path
- Token budget: <50K tokens for llms-full.txt

### Charmbracelet TUI (Go)

**Usage Patterns:**
- `huh` for interactive forms
- `lipgloss` for styled terminal output
- Keep TUI optional - support non-interactive mode

## Testing Rules

### Two-Tier Testing Strategy

**Fast Tests (CI - every PR):**
- Run with `-short` flag: `go test -short ./...`
- Skip slow integration tests
- Target: ~2 seconds

**Full Tests (nightly + pre-commit):**
- Run without flags: `go test ./...`
- Include git operations and multi-clone scenarios
- Target: ~14 seconds

### Test File Organization

**Naming:**
- Unit tests: `<file>_test.go` in same directory
- Integration tests: `*_integration_test.go`
- Slow tests must check: `if testing.Short() { t.Skip("slow test") }`

**Structure:**
- Table-driven tests with `[]struct` and `t.Run()`
- Descriptive test names explaining what is being tested
- Clean up resources in test teardown

### Coverage Requirements

**Thresholds (from ci.yml):**
- Minimum: 42% (CI fails below)
- Warning: 55% (CI warns below)
- Always run with race detection: `go test -race ./...`

### Test Patterns

```go
func TestFeature(t *testing.T) {
    tests := []struct {
        name    string
        input   InputType
        want    OutputType
        wantErr bool
    }{
        {"valid case", validInput, expected, false},
        {"error case", badInput, nil, true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // test logic
        })
    }
}
```

### Linter Exclusions for Tests

- `errcheck` excluded for cleanup: `Close`, `Rollback`, `RemoveAll`, etc.
- `gosec` G304/G306 excluded for test fixtures

## Code Quality & Style Rules

### Go Linting (golangci-lint)

**Enabled Linters:**
- `errcheck` - error return checking
- `gosec` - security issues
- `misspell` - spelling (US locale)
- `unconvert` - unnecessary conversions
- `unparam` - unused parameters

**Run locally:** `golangci-lint run ./...`

**Note:** ~100 baseline warnings exist (documented false positives). Focus on avoiding NEW issues.

### Go Code Style

**Formatting:**
- Use `gofmt` (auto-runs in most editors)
- Follow [Effective Go](https://golang.org/doc/effective_go) guidelines
- Keep functions small and focused

**Comments:**
- Add comments for exported functions and types
- Use `// ===== Section =====` for struct field grouping
- No redundant comments on obvious code

### Documentation Content Style (Diátaxis)

**Anti-marketing Rule:**
- Reference and How-to: NO promotional language
- Only factual, operational content

**Admonition Types:**
| Type | Use For |
|------|---------|
| `:::tip` | Helpful shortcuts |
| `:::note` | Prerequisites, versions |
| `:::warning` | Reversible problems |
| `:::danger` | Data loss / irreversible |
| `:::info` | Technical background |

### CLI Example Format (for docs)

```markdown
## Command: bd [command]

**Prerequisites:**
- [Required state or setup]

**Usage:**
$ bd [command] [options]

**Expected output:**
[Exact output]

**Recovery:** See [Recovery Runbook](/recovery/[topic])
```

### Recovery Section Format

```markdown
## Recovery: [Problem Name]

### Symptoms
- [Observable symptom]

### Diagnosis
$ [diagnostic command]

### Solution
1. [Step with command]
2. [Verification step]

### Prevention
[How to avoid]
```

## Development Workflow Rules

### PR-Based Workflow

**Branch:** `docs/docusaurus-site` → PR to `main`

**Commit Pattern:**
- `docs: add recovery runbook for sync failures`
- `fix: correct URL in docusaurus.config.ts`

**PR Checklist:**
- [ ] Content follows Diátaxis category
- [ ] CLI examples have Prerequisites section
- [ ] No broken links (`npm run build` passes)
- [ ] llms-full.txt under 50K tokens

### Local Commands

```bash
# Website (if npm available)
cd website && npm run build

# llms.txt regeneration
./scripts/generate-llms-full.sh
```

### Files NOT Committed

- `_bmad/`, `_bmad-output/` — BMAD framework (local only)
- `node_modules/` — dependencies

## Critical Don't-Miss Rules

### DANGER: Never Use `bd doctor --fix` (Priority 0)

**Epic 2 Research Finding:** Analysis of 54 GitHub issues revealed that `bd doctor --fix` frequently causes MORE damage than the original problem.

**What happens:**
- Automated fixes delete "circular" dependencies that are actually valid
- False positive detection removes legitimate parent-child relationships
- Recovery after `--fix` is harder than recovery from original issue

**Safe alternatives:**
```bash
# SAFE: Diagnose only (no changes)
bd doctor

# SAFE: Check blocked issues
bd blocked

# SAFE: Inspect specific issue
bd show <issue-id>

# RECOVERY: If bd doctor --fix damaged data
git checkout HEAD~1 -- .beads/issues.jsonl
bd sync --import-only
```

**Rule:** Always diagnose manually, never auto-fix. If `bd doctor` reports issues, investigate each one before taking action.

### URL Fix Required (Priority 1)

**3 files contain wrong URLs (joyshmitz → steveyegge):**
- `website/docusaurus.config.ts` lines 15-18
- `scripts/generate-llms-full.sh` line 18
- `website/static/llms.txt` lines 48-51

### Token Budget

- `llms-full.txt` must stay under **50K tokens** (~37,500 words)
- Current: ~18K tokens
- Validate: `wc -w < website/static/llms-full.txt`

### Content Completeness

**Every CLI command doc MUST have:**
1. Prerequisites
2. Usage (exact command)
3. Expected output
4. Error case + Recovery link

### Content Anti-Patterns

**NEVER in Reference/How-to:**
- Marketing language ("amazing", "powerful")
- Examples without Prerequisites

**NEVER in Recovery docs:**
- More than 5 solution steps
- Missing Symptoms/Diagnosis

### CI/CD Gaps (Not Yet Implemented)

- Link checker — add to deploy-docs.yml
- Token count validation — add to CI
- Readability scoring — Growth phase

### Docusaurus Gotchas

- `onBrokenLinks: 'warn'` — broken links don't fail build
- `routeBasePath: '/'` — docs ARE homepage
- Prism languages: `bash`, `json`, `toml`, `go`

### llms.txt Structure (llmstxt.org spec)

```
# Beads
> One-line description
## Quick Start
## Core Concepts
## CLI Reference
## Optional
```

---

## Usage Guidelines

**For AI Agents:**
- Read this file before implementing any code
- Follow ALL rules exactly as documented
- When in doubt, prefer the more restrictive option
- Update this file if new patterns emerge

**For Humans:**
- Keep this file lean and focused on agent needs
- Update when technology stack changes
- Review quarterly for outdated rules
- Remove rules that become obvious over time

---

_Last Updated: 2025-12-30_
