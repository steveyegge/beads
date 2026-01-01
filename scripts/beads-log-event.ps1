# beads-log-event.ps1 - Log events to .beads/events.log
# Usage: .\scripts\beads-log-event.ps1 -EventCode <code> [-IssueId <id>] [-Description <desc>]
#
# Event format: timestamp|event_code|issue_id|user|unix_timestamp|description

param(
    [Parameter(Mandatory=$true)]
    [string]$EventCode,

    [string]$IssueId = "none",

    [string]$Description = ""
)

# Ensure .beads directory exists
if (-not (Test-Path ".beads")) {
    Write-Error ".beads directory not found"
    exit 1
}

# Get timestamp in ISO 8601 format
$Timestamp = Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ"
$UnixTs = [int][double]::Parse((Get-Date -UFormat %s))

# Get user (use git config or fallback to unknown)
try {
    $User = git config user.name 2>$null
    if (-not $User) { $User = "unknown" }
} catch {
    $User = "unknown"
}

# Escape pipe characters in description
$DescriptionSafe = $Description -replace '\|', '-'

# Build log line
$LogLine = "${Timestamp}|${EventCode}|${IssueId}|${User}|${UnixTs}|${DescriptionSafe}"

# Append to events log
Add-Content -Path ".beads/events.log" -Value $LogLine

Write-Host "Logged: ${EventCode} $(if ($IssueId -ne 'none') { "($IssueId)" })"
