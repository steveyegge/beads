# In-Repo Validators

This guide explains how to set up and customize beads validation scripts for your repositories.

## Overview

An in-repo validator is a script that lives in your repository's `.beads/` directory and validates that beads is properly configured and functioning. Validators are useful for:

- Quick health checks during development
- CI/CD pipeline validation
- Onboarding new team members
- Verifying agent compliance with beads patterns

## Quick Start

1. Copy the template to your repo:
   ```powershell
   # From beads repo
   Copy-Item scripts/validate-beads.ps1.template /path/to/your-repo/.beads/validate.ps1
   ```

2. Edit the project name:
   ```powershell
   $PROJECT_NAME = "YourProject"  # Change this line
   ```

3. Force-add to git (since .beads/ is typically gitignored):
   ```bash
   git add -f .beads/validate.ps1
   git commit -m "Add beads validator"
   ```

4. Run the validator:
   ```powershell
   .\.beads\validate.ps1
   ```

## Core Checks

The template includes these standard checks:

| Check | Description |
|-------|-------------|
| `.beads directory exists` | Basic beads presence |
| `Database exists` | SQLite database created |
| `bd command available` | CLI is in PATH |
| `bd info succeeds` | Can read database info |
| `bd list succeeds` | Can query issues |
| `bd ready succeeds` | Can find unblocked work |
| `issues.jsonl exists` | Sync has been run |
| `bd doctor passes` | No database corruption |

## Customization

### Adding Project-Specific Checks

Edit the "PROJECT-SPECIFIC CHECKS" section in your validator:

```powershell
# ============================================================================
# PROJECT-SPECIFIC CHECKS - Add your custom validations here
# ============================================================================

# Check for correct issue prefix
try {
    $output = bd --no-daemon info --json 2>&1 | Out-String
    $info = Get-JsonFromOutput $output '{'
    Write-Check "Issue prefix is 'myproject'" ($info.config.issue_prefix -eq "myproject")
} catch {
    Write-Check "Issue prefix check" $false
}
```

### Common Customizations

**1. Verify Issue Prefix**
```powershell
$expectedPrefix = "myapp"
Write-Check "Correct prefix" ($info.config.issue_prefix -eq $expectedPrefix)
```

**2. Check Minimum Issue Count**
```powershell
$count = (bd --no-daemon list --json | ConvertFrom-Json).Count
Write-Check "Has tracked issues" ($count -ge 1)
```

**3. Verify No Stale In-Progress**
```powershell
$inProgress = bd --no-daemon list --status in_progress --json | ConvertFrom-Json
Write-Check "No abandoned work" ($inProgress.Count -le 1)
```

**4. Check Blocked Issues Have Dependencies**
```powershell
$blocked = bd --no-daemon blocked 2>&1
Write-Check "Blocked issues exist" ($blocked -match "blocked")
```

**5. Verify Git Hooks Installed**
```powershell
$hookPath = ".git/hooks/post-commit"
Write-Check "Post-commit hook exists" (Test-Path $hookPath)
```

## Usage Options

```powershell
# Basic validation
.\.beads\validate.ps1

# Verbose output (shows details for each check)
.\.beads\validate.ps1 -Verbose

# Create and close a test issue (validates full lifecycle)
.\.beads\validate.ps1 -CreateTestIssue
```

## CI/CD Integration

Add to your pipeline:

```yaml
# GitHub Actions example
- name: Validate beads integration
  run: powershell -ExecutionPolicy Bypass -File .beads/validate.ps1
  shell: pwsh
```

```yaml
# Azure Pipelines example
- task: PowerShell@2
  inputs:
    filePath: '.beads/validate.ps1'
    pwsh: true
```

## Troubleshooting

### Validator Not Found
The `.beads/` directory is gitignored by default. Force-add your validator:
```bash
git add -f .beads/validate.ps1
```

### Permission Denied
Run with execution policy bypass:
```powershell
powershell -ExecutionPolicy Bypass -File .beads/validate.ps1
```

### bd Command Not Found
Ensure beads is installed and in PATH:
```bash
# Check installation
bd --version

# If not found, reinstall
go install github.com/steveyegge/beads/cmd/bd@latest
```

### JSON Parse Errors
The validator extracts JSON from output that may contain warnings. If parsing fails, check that `bd` commands work manually:
```powershell
bd --no-daemon info --json
bd --no-daemon list --json
```

## Example: ActionableLogLines Validator

Here's the customized validator from the ActionableLogLines project:

```powershell
$PROJECT_NAME = "ActionableLogLines"

# ... core checks ...

# Project-specific: Verify ALLP prefix
try {
    $output = bd --no-daemon info --json 2>&1 | Out-String
    $info = Get-JsonFromOutput $output '{'
    Write-Check "Uses ActionableLogLines prefix" `
        ($info.config.issue_prefix -eq "ActionableLogLines")
} catch {
    Write-Check "Prefix check" $false
}

# Project-specific: Has release epic
try {
    $output = bd --no-daemon list --type epic --json 2>&1 | Out-String
    $epics = Get-JsonFromOutput $output '['
    Write-Check "Has epic for release planning" ($epics.Count -ge 1)
} catch {
    Write-Check "Epic check" $false
}
```

## Related Documentation

- [TESTING.md](TESTING.md) - External repository testing
- [BEADS_HARNESS_PATTERN.md](BEADS_HARNESS_PATTERN.md) - Agent workflow patterns
- [CLI_REFERENCE.md](CLI_REFERENCE.md) - Complete command reference
