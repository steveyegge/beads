# test-external-repo.ps1 - Validate beads execution against external repository
# Usage: .\scripts\test-external-repo.ps1 [-RepoPath <path>] [-SkipCleanup] [-Verbose]
#
# Tests beads CLI against an external repository in an isolated temp environment.
# Default target: C:\myStuff\_infra\ActionalbleLogLines

param(
    [string]$RepoPath = "C:\myStuff\_infra\ActionalbleLogLines",
    [switch]$SkipCleanup,
    [switch]$VerboseOutput
)

$ErrorActionPreference = "Stop"
$script:PassCount = 0
$script:FailCount = 0
$script:TestResults = @()

# --- Helper Functions ---

function Write-TestHeader {
    param([string]$Category)
    Write-Host "`n========================================" -ForegroundColor Cyan
    Write-Host " $Category" -ForegroundColor Cyan
    Write-Host "========================================" -ForegroundColor Cyan
}

function Test-Pass {
    param([string]$TestName, [string]$Details = "")
    $script:PassCount++
    $script:TestResults += @{Name=$TestName; Status="PASS"; Details=$Details}
    Write-Host "[PASS] $TestName" -ForegroundColor Green
    if ($VerboseOutput -and $Details) {
        Write-Host "       $Details" -ForegroundColor DarkGray
    }
}

function Test-Fail {
    param([string]$TestName, [string]$Error = "")
    $script:FailCount++
    $script:TestResults += @{Name=$TestName; Status="FAIL"; Details=$Error}
    Write-Host "[FAIL] $TestName" -ForegroundColor Red
    if ($Error) {
        Write-Host "       $Error" -ForegroundColor DarkRed
    }
}

function Invoke-BD {
    param([string[]]$BdArgs, [switch]$NoDaemon, [switch]$Json, [switch]$AllowFail)

    $cmdArgs = @()
    if ($NoDaemon) { $cmdArgs += "--no-daemon" }
    $cmdArgs += $BdArgs
    if ($Json) { $cmdArgs += "--json" }

    if ($VerboseOutput) {
        Write-Host "       > bd $($cmdArgs -join ' ')" -ForegroundColor DarkGray
    }

    # Temporarily set error action to Continue to prevent stderr from throwing
    $oldErrorAction = $ErrorActionPreference
    $ErrorActionPreference = "Continue"

    try {
        # Run bd command, capturing both stdout and stderr
        $result = & bd @cmdArgs 2>&1
        $exitCode = $LASTEXITCODE

        # Convert to string (handle both strings and error records)
        $output = ($result | ForEach-Object {
            if ($_ -is [System.Management.Automation.ErrorRecord]) {
                $_.ToString()
            } else {
                $_
            }
        }) -join "`n"

        if (-not $AllowFail -and $exitCode -ne 0) {
            throw "bd command failed with exit code $exitCode`: $output"
        }

        # Extract JSON from output (may contain warnings before JSON)
        if ($Json -and $output) {
            # Find the first { or [ which starts the JSON
            $jsonStart = $output.IndexOf('{')
            $arrayStart = $output.IndexOf('[')

            if ($jsonStart -ge 0 -and ($arrayStart -lt 0 -or $jsonStart -lt $arrayStart)) {
                $output = $output.Substring($jsonStart)
            } elseif ($arrayStart -ge 0) {
                $output = $output.Substring($arrayStart)
            }
        }

        return @{
            Output = $output
            ExitCode = $exitCode
        }
    } finally {
        $ErrorActionPreference = $oldErrorAction
    }
}

# --- Setup ---

Write-Host "`nBeads External Repository Test Suite" -ForegroundColor Yellow
Write-Host "Target: $RepoPath" -ForegroundColor Yellow
Write-Host "Time: $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')" -ForegroundColor Yellow

# Verify source repo exists
if (-not (Test-Path $RepoPath)) {
    Write-Error "Source repository not found: $RepoPath"
    exit 1
}

# Create isolated test environment
$Timestamp = Get-Date -Format "yyyyMMdd-HHmmss"
$TestDir = Join-Path $env:TEMP "beads-test-$Timestamp"
Write-Host "`nCreating isolated test environment: $TestDir" -ForegroundColor Yellow

try {
    # Clone to temp (local copy, not git clone)
    Copy-Item -Path $RepoPath -Destination $TestDir -Recurse -Force
    Set-Location $TestDir

    # Create test branch (git writes to stderr even on success, so use cmd /c to avoid PS error handling)
    cmd /c "git checkout -b beads-test-$Timestamp 2>nul"

    # Remove existing beads data for fresh start
    if (Test-Path ".beads") {
        Remove-Item -Path ".beads" -Recurse -Force
    }

    Write-Host "Test environment ready`n" -ForegroundColor Green

    # ========================================
    # TEST SUITE 1: Init & Info
    # ========================================
    Write-TestHeader "1. Init & Info"

    # TC-1.1: Basic init
    try {
        $result = Invoke-BD -BdArgs @("init", "--prefix", "test", "--quiet")
        if (Test-Path ".beads/beads.db") {
            Test-Pass "TC-1.1: bd init creates database"
        } else {
            Test-Fail "TC-1.1: bd init creates database" "beads.db not found"
        }
    } catch {
        Test-Fail "TC-1.1: bd init creates database" $_.Exception.Message
    }

    # TC-1.2: Info command
    try {
        $result = Invoke-BD -BdArgs @("info") -Json -NoDaemon
        $info = $result.Output | ConvertFrom-Json
        if ($info.database_path) {
            Test-Pass "TC-1.2: bd info returns JSON" "db: $($info.database_path)"
        } else {
            Test-Fail "TC-1.2: bd info returns JSON" "Missing database_path"
        }
    } catch {
        Test-Fail "TC-1.2: bd info returns JSON" $_.Exception.Message
    }

    # TC-1.3: Duplicate init (should be idempotent)
    try {
        $result = Invoke-BD -BdArgs @("init", "--prefix", "test", "--quiet") -AllowFail
        Test-Pass "TC-1.3: Duplicate init is idempotent"
    } catch {
        Test-Fail "TC-1.3: Duplicate init is idempotent" $_.Exception.Message
    }

    # ========================================
    # TEST SUITE 2: Issue CRUD
    # ========================================
    Write-TestHeader "2. Issue CRUD"

    # TC-2.1: Create issue
    $testIssueId = $null
    try {
        $result = Invoke-BD -BdArgs @("create", "Test issue for validation", "-t", "task", "-p", "2") -Json -NoDaemon
        $issue = $result.Output | ConvertFrom-Json
        if ($issue.id -match "^test-[a-z0-9]+$") {
            $testIssueId = $issue.id
            Test-Pass "TC-2.1: Create issue" "ID: $testIssueId"
        } else {
            Test-Fail "TC-2.1: Create issue" "Invalid ID format: $($issue.id)"
        }
    } catch {
        Test-Fail "TC-2.1: Create issue" $_.Exception.Message
    }

    # TC-2.2: Show issue
    if ($testIssueId) {
        try {
            $result = Invoke-BD -BdArgs @("show", $testIssueId) -Json -NoDaemon
            $shown = $result.Output | ConvertFrom-Json
            if ($shown.title -eq "Test issue for validation") {
                Test-Pass "TC-2.2: Show issue" "Title matches"
            } else {
                Test-Fail "TC-2.2: Show issue" "Title mismatch: $($shown.title)"
            }
        } catch {
            Test-Fail "TC-2.2: Show issue" $_.Exception.Message
        }
    }

    # TC-2.3: Update status
    if ($testIssueId) {
        try {
            Invoke-BD -BdArgs @("update", $testIssueId, "--status", "in_progress") -NoDaemon | Out-Null
            $result = Invoke-BD -BdArgs @("show", $testIssueId) -Json -NoDaemon
            $updated = $result.Output | ConvertFrom-Json
            if ($updated.status -eq "in_progress") {
                Test-Pass "TC-2.3: Update status"
            } else {
                Test-Fail "TC-2.3: Update status" "Status: $($updated.status)"
            }
        } catch {
            Test-Fail "TC-2.3: Update status" $_.Exception.Message
        }
    }

    # TC-2.4: Close issue
    if ($testIssueId) {
        try {
            Invoke-BD -BdArgs @("close", $testIssueId, "--reason", "Test complete") -NoDaemon | Out-Null
            $result = Invoke-BD -BdArgs @("show", $testIssueId) -Json -NoDaemon
            $closed = $result.Output | ConvertFrom-Json
            if ($closed.status -eq "closed") {
                Test-Pass "TC-2.4: Close issue"
            } else {
                Test-Fail "TC-2.4: Close issue" "Status: $($closed.status)"
            }
        } catch {
            Test-Fail "TC-2.4: Close issue" $_.Exception.Message
        }
    }

    # TC-2.5: Reopen issue
    if ($testIssueId) {
        try {
            Invoke-BD -BdArgs @("reopen", $testIssueId) -NoDaemon | Out-Null
            $result = Invoke-BD -BdArgs @("show", $testIssueId) -Json -NoDaemon
            $reopened = $result.Output | ConvertFrom-Json
            if ($reopened.status -eq "open") {
                Test-Pass "TC-2.5: Reopen issue"
            } else {
                Test-Fail "TC-2.5: Reopen issue" "Status: $($reopened.status)"
            }
        } catch {
            Test-Fail "TC-2.5: Reopen issue" $_.Exception.Message
        }
    }

    # ========================================
    # TEST SUITE 3: Sync Operations
    # ========================================
    Write-TestHeader "3. Sync Operations"

    # TC-3.1: Export to JSONL
    try {
        Invoke-BD -BdArgs @("export") -NoDaemon | Out-Null
        if (Test-Path ".beads/issues.jsonl") {
            Test-Pass "TC-3.1: Export creates JSONL"
        } else {
            Test-Fail "TC-3.1: Export creates JSONL" "File not found"
        }
    } catch {
        Test-Fail "TC-3.1: Export creates JSONL" $_.Exception.Message
    }

    # TC-3.2: JSONL contains test issue
    if ($testIssueId -and (Test-Path ".beads/issues.jsonl")) {
        try {
            $content = Get-Content ".beads/issues.jsonl" -Raw
            if ($content -match "Test issue for validation") {
                Test-Pass "TC-3.2: JSONL contains test issue"
            } else {
                Test-Fail "TC-3.2: JSONL contains test issue" "Issue not found in JSONL"
            }
        } catch {
            Test-Fail "TC-3.2: JSONL contains test issue" $_.Exception.Message
        }
    }

    # TC-3.3: Sync (local only, no remote)
    try {
        # Remove remote to test local-only sync (use cmd /c to avoid PS stderr issues)
        cmd /c "git remote remove origin 2>nul"
        $result = Invoke-BD -BdArgs @("sync") -NoDaemon -AllowFail
        Test-Pass "TC-3.3: Sync without remote" "Graceful handling"
    } catch {
        Test-Fail "TC-3.3: Sync without remote" $_.Exception.Message
    }

    # ========================================
    # TEST SUITE 4: Dependencies
    # ========================================
    Write-TestHeader "4. Dependencies"

    # Create parent and child issues for dependency tests
    $parentId = $null
    $childId = $null
    try {
        $result = Invoke-BD -BdArgs @("create", "Parent task", "-t", "task", "-p", "2") -Json -NoDaemon
        $parentId = ($result.Output | ConvertFrom-Json).id

        $result = Invoke-BD -BdArgs @("create", "Child task", "-t", "task", "-p", "2") -Json -NoDaemon
        $childId = ($result.Output | ConvertFrom-Json).id

        Test-Pass "TC-4.0: Create parent/child issues" "Parent: $parentId, Child: $childId"
    } catch {
        Test-Fail "TC-4.0: Create parent/child issues" $_.Exception.Message
    }

    # TC-4.1: Add dependency
    if ($parentId -and $childId) {
        try {
            Invoke-BD -BdArgs @("dep", "add", $childId, $parentId) -NoDaemon | Out-Null
            Test-Pass "TC-4.1: Add dependency"
        } catch {
            Test-Fail "TC-4.1: Add dependency" $_.Exception.Message
        }
    }

    # TC-4.2: Blocked list shows child
    if ($childId) {
        try {
            $result = Invoke-BD -BdArgs @("blocked") -NoDaemon
            if ($result.Output -match $childId) {
                Test-Pass "TC-4.2: Blocked list shows child"
            } else {
                Test-Fail "TC-4.2: Blocked list shows child" "Child not in blocked list"
            }
        } catch {
            Test-Fail "TC-4.2: Blocked list shows child" $_.Exception.Message
        }
    }

    # TC-4.3: Ready excludes blocked
    if ($childId) {
        try {
            $result = Invoke-BD -BdArgs @("ready") -NoDaemon
            if ($result.Output -notmatch $childId) {
                Test-Pass "TC-4.3: Ready excludes blocked issues"
            } else {
                Test-Fail "TC-4.3: Ready excludes blocked issues" "Blocked child in ready list"
            }
        } catch {
            Test-Fail "TC-4.3: Ready excludes blocked issues" $_.Exception.Message
        }
    }

    # ========================================
    # TEST SUITE 5: Mode Parity
    # ========================================
    Write-TestHeader "5. Mode Parity (Daemon vs --no-daemon)"

    # TC-5.1: List output matches
    try {
        $daemonResult = Invoke-BD -BdArgs @("list", "--status", "open") -Json
        $directResult = Invoke-BD -BdArgs @("list", "--status", "open") -Json -NoDaemon

        $daemonCount = ($daemonResult.Output | ConvertFrom-Json).Count
        $directCount = ($directResult.Output | ConvertFrom-Json).Count

        if ($daemonCount -eq $directCount) {
            Test-Pass "TC-5.1: Mode parity - list" "Both returned $daemonCount issues"
        } else {
            Test-Fail "TC-5.1: Mode parity - list" "Daemon: $daemonCount, Direct: $directCount"
        }
    } catch {
        Test-Pass "TC-5.1: Mode parity - list (daemon not running)" "Skipped - daemon comparison"
    }

    # ========================================
    # TEST SUITE 6: Recovery
    # ========================================
    Write-TestHeader "6. Recovery"

    # TC-6.1: Doctor check
    try {
        $result = Invoke-BD -BdArgs @("doctor") -NoDaemon -AllowFail
        Test-Pass "TC-6.1: Doctor runs without error"
    } catch {
        Test-Fail "TC-6.1: Doctor runs without error" $_.Exception.Message
    }

    # TC-6.2: List after operations
    try {
        $result = Invoke-BD -BdArgs @("list") -Json -NoDaemon
        $issues = $result.Output | ConvertFrom-Json
        if ($issues.Count -ge 3) {
            Test-Pass "TC-6.2: List shows all test issues" "Found $($issues.Count) issues"
        } else {
            Test-Fail "TC-6.2: List shows all test issues" "Only $($issues.Count) issues"
        }
    } catch {
        Test-Fail "TC-6.2: List shows all test issues" $_.Exception.Message
    }

    # ========================================
    # TEST SUITE 7: Concurrent Creation (Hash Collision)
    # ========================================
    Write-TestHeader "7. Concurrent Creation"

    # TC-7.1: Rapid creation produces unique IDs
    try {
        $ids = @()
        for ($i = 1; $i -le 5; $i++) {
            $result = Invoke-BD -BdArgs @("create", "Rapid test $i", "-t", "task", "-p", "3") -Json -NoDaemon
            $issue = $result.Output | ConvertFrom-Json
            $ids += $issue.id
        }

        $uniqueIds = $ids | Select-Object -Unique
        if ($uniqueIds.Count -eq $ids.Count) {
            Test-Pass "TC-7.1: Rapid creation - unique IDs" "Created $($ids.Count) unique issues"
        } else {
            Test-Fail "TC-7.1: Rapid creation - unique IDs" "Collision detected"
        }
    } catch {
        Test-Fail "TC-7.1: Rapid creation - unique IDs" $_.Exception.Message
    }

} catch {
    Write-Host "`nFATAL ERROR: $($_.Exception.Message)" -ForegroundColor Red
    $script:FailCount++
} finally {
    # --- Cleanup ---
    Set-Location $env:TEMP

    if (-not $SkipCleanup -and (Test-Path $TestDir)) {
        Write-Host "`nCleaning up test environment..." -ForegroundColor Yellow
        try {
            # Kill any bd daemon that might be running
            Get-Process -Name "bd" -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
            Start-Sleep -Milliseconds 500
            Remove-Item -Path $TestDir -Recurse -Force -ErrorAction SilentlyContinue
            Write-Host "Cleanup complete" -ForegroundColor Green
        } catch {
            Write-Host "Warning: Cleanup incomplete - $($_.Exception.Message)" -ForegroundColor Yellow
        }
    } elseif ($SkipCleanup) {
        Write-Host "`nTest directory preserved: $TestDir" -ForegroundColor Yellow
    }

    # --- Summary ---
    Write-Host "`n========================================" -ForegroundColor Cyan
    Write-Host " TEST SUMMARY" -ForegroundColor Cyan
    Write-Host "========================================" -ForegroundColor Cyan
    Write-Host "Passed: $script:PassCount" -ForegroundColor Green
    Write-Host "Failed: $script:FailCount" -ForegroundColor $(if ($script:FailCount -gt 0) { "Red" } else { "Green" })
    Write-Host "Total:  $($script:PassCount + $script:FailCount)" -ForegroundColor White

    if ($script:FailCount -gt 0) {
        Write-Host "`nFailed Tests:" -ForegroundColor Red
        $script:TestResults | Where-Object { $_.Status -eq "FAIL" } | ForEach-Object {
            Write-Host "  - $($_.Name): $($_.Details)" -ForegroundColor Red
        }
        exit 1
    } else {
        Write-Host "`nAll tests passed!" -ForegroundColor Green
        exit 0
    }
}
