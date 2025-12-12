# beads-log-event.ps1 - Central event logging utility for Beads-First applications (PowerShell)
#
# Usage: .\beads-log-event.ps1 -EventCode <code> [-IssueId <id>] [-Details <text>]
#
# Event codes follow the taxonomy defined in events/EVENT_TAXONOMY.md
#
# Examples:
#   .\beads-log-event.ps1 -EventCode sk.bootup.activated
#   .\beads-log-event.ps1 -EventCode bd.issue.create -IssueId bd-0001 -Details "InitApp epic created"

param(
    [Parameter(Mandatory=$true)]
    [string]$EventCode,
    
    [Parameter(Mandatory=$false)]
    [string]$IssueId = "none",
    
    [Parameter(Mandatory=$false)]
    [string]$Details = ""
)

$ErrorActionPreference = "Stop"

# Timestamp in ISO 8601 format
$Timestamp = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")

# Agent and session identification
$AgentId = if ($env:BEADS_AGENT_ID) { $env:BEADS_AGENT_ID } else { $env:USERNAME }
$SessionId = if ($env:BEADS_SESSION_ID) { $env:BEADS_SESSION_ID } else { [int](Get-Date -UFormat %s) }

# Find project root (look for .beads directory)
function Find-ProjectRoot {
    $dir = Get-Location
    while ($dir.Path -ne [System.IO.Path]::GetPathRoot($dir.Path)) {
        if (Test-Path (Join-Path $dir.Path ".beads")) {
            return $dir.Path
        }
        $dir = Split-Path $dir.Path -Parent | Get-Item
    }
    return (Get-Location).Path  # Fallback
}

$ProjectRoot = Find-ProjectRoot
$LogDir = Join-Path $ProjectRoot ".beads"
$LogFile = Join-Path $LogDir "events.log"

# Ensure log directory exists
if (-not (Test-Path $LogDir)) {
    New-Item -ItemType Directory -Path $LogDir -Force | Out-Null
}

# Format: TIMESTAMP|EVENT_CODE|ISSUE_ID|AGENT_ID|SESSION_ID|DETAILS
$LogEntry = "$Timestamp|$EventCode|$IssueId|$AgentId|$SessionId|$Details"

# Append to log file
Add-Content -Path $LogFile -Value $LogEntry -Encoding UTF8

# Echo for visibility (can be suppressed with BEADS_QUIET=1)
if ($env:BEADS_QUIET -ne "1") {
    Write-Host "[BEADS EVENT] $EventCode | $IssueId | $Details" -ForegroundColor Cyan
}
