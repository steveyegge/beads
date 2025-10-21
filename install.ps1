# Beads (bd) Windows installer
# Usage:
#   irm https://raw.githubusercontent.com/steveyegge/beads/main/install.ps1 | iex

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$Script:SkipGoInstall = $env:BEADS_INSTALL_SKIP_GOINSTALL -eq "1"
$Script:SourceOverride = $env:BEADS_INSTALL_SOURCE

function Write-Info($Message)    { Write-Host "==> $Message" -ForegroundColor Cyan }
function Write-Success($Message) { Write-Host "==> $Message" -ForegroundColor Green }
function Write-WarningMsg($Message) { Write-Warning $Message }
function Write-Err($Message)     { Write-Host "Error: $Message" -ForegroundColor Red }

function Test-GoSupport {
    $goCmd = Get-Command go -ErrorAction SilentlyContinue
    if (-not $goCmd) {
        return [pscustomobject]@{
            Present = $false
            MeetsRequirement = $false
            RawVersion = $null
        }
    }

    try {
        $output = & go version
    } catch {
        return [pscustomobject]@{
            Present = $false
            MeetsRequirement = $false
            RawVersion = $null
        }
    }

    $match = [regex]::Match($output, 'go(?<major>\d+)\.(?<minor>\d+)')
    if (-not $match.Success) {
        return [pscustomobject]@{
            Present = $true
            MeetsRequirement = $true
            RawVersion = $output
        }
    }

    $major = [int]$match.Groups["major"].Value
    $minor = [int]$match.Groups["minor"].Value
    $meets = ($major -gt 1) -or ($major -eq 1 -and $minor -ge 24)

    return [pscustomobject]@{
        Present = $true
        MeetsRequirement = $meets
        RawVersion = $output.Trim()
    }
}

function Install-WithGo {
    if ($Script:SkipGoInstall) {
        Write-Info "Skipping go install (BEADS_INSTALL_SKIP_GOINSTALL=1)."
        return $false
    }

    Write-Info "Installing bd via go install..."
    try {
        & go install github.com/steveyegge/beads/cmd/bd@latest
        if ($LASTEXITCODE -ne 0) {
            Write-WarningMsg "go install exited with code $LASTEXITCODE"
            return $false
        }
    } catch {
        Write-WarningMsg "go install failed: $_"
        return $false
    }

    $gopath = (& go env GOPATH)
    if (-not $gopath) {
        return $true
    }

    $binDir = Join-Path $gopath "bin"
    $bdPath = Join-Path $binDir "bd.exe"
    if (-not (Test-Path $bdPath)) {
        Write-WarningMsg "bd.exe not found in $binDir after install"
    }

    $pathEntries = [Environment]::GetEnvironmentVariable("PATH", "Process").Split([IO.Path]::PathSeparator) | ForEach-Object { $_.Trim() }
    if (-not ($pathEntries -contains $binDir)) {
        Write-WarningMsg "$binDir is not in your PATH. Add it with:`n  setx PATH `"$Env:PATH;$binDir`""
    }

    return $true
}

function Install-FromSource {
    Write-Info "Building bd from source..."

    $tempRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("beads-install-" + [guid]::NewGuid().ToString("N"))
    New-Item -ItemType Directory -Path $tempRoot | Out-Null

    try {
        $repoPath = Join-Path $tempRoot "beads"
        if ($Script:SourceOverride) {
            Write-Info "Using source override: $Script:SourceOverride"
            if (Test-Path $Script:SourceOverride) {
                New-Item -ItemType Directory -Path $repoPath | Out-Null
                Get-ChildItem -LiteralPath $Script:SourceOverride -Force | Where-Object { $_.Name -ne ".git" } | ForEach-Object {
                    $destination = Join-Path $repoPath $_.Name
                    if ($_.PSIsContainer) {
                        Copy-Item -LiteralPath $_.FullName -Destination $destination -Recurse -Force
                    } else {
                        Copy-Item -LiteralPath $_.FullName -Destination $repoPath -Force
                    }
                }
            } else {
                Write-Info "Cloning override repository..."
                & git clone $Script:SourceOverride $repoPath
                if ($LASTEXITCODE -ne 0) {
                    throw "git clone failed with exit code $LASTEXITCODE"
                }
            }
        } else {
            Write-Info "Cloning repository..."
            & git clone --depth 1 https://github.com/steveyegge/beads.git $repoPath
            if ($LASTEXITCODE -ne 0) {
                throw "git clone failed with exit code $LASTEXITCODE"
            }
        }

        Push-Location $repoPath
        try {
            Write-Info "Compiling bd.exe..."
            & go build -o bd.exe ./cmd/bd
            if ($LASTEXITCODE -ne 0) {
                throw "go build failed with exit code $LASTEXITCODE"
            }
        } finally {
            Pop-Location
        }

        $installDir = Join-Path $env:LOCALAPPDATA "Programs\bd"
        New-Item -ItemType Directory -Path $installDir -Force | Out-Null

        Copy-Item -Path (Join-Path $repoPath "bd.exe") -Destination (Join-Path $installDir "bd.exe") -Force
        Write-Success "bd installed to $installDir\bd.exe"

        $pathEntries = [Environment]::GetEnvironmentVariable("PATH", "Process").Split([IO.Path]::PathSeparator) | ForEach-Object { $_.Trim() }
        if (-not ($pathEntries -contains $installDir)) {
            Write-WarningMsg "$installDir is not in your PATH. Add it with:`n  setx PATH `"$Env:PATH;$installDir`""
        }
    } finally {
        Remove-Item -Path $tempRoot -Recurse -Force -ErrorAction SilentlyContinue
    }

    return $true
}

function Verify-Install {
    Write-Info "Verifying installation..."
    try {
        $versionOutput = & bd version 2>$null
        if ($LASTEXITCODE -ne 0) {
            Write-WarningMsg "bd version exited with code $LASTEXITCODE"
            return $false
        }
        Write-Success "bd is installed: $versionOutput"
        return $true
    } catch {
        Write-WarningMsg "bd is not on PATH yet. Add the install directory to PATH and re-open your shell."
        return $false
    }
}

$goSupport = Test-GoSupport

if ($goSupport.Present) {
    Write-Info "Detected Go: $($goSupport.RawVersion)"
} else {
    Write-WarningMsg "Go not found on PATH."
}

$installed = $false

if ($goSupport.Present -and $goSupport.MeetsRequirement) {
    $installed = Install-WithGo
    if (-not $installed) {
        Write-WarningMsg "Falling back to source build..."
    }
} elseif ($goSupport.Present -and -not $goSupport.MeetsRequirement) {
    Write-Err "Go 1.24 or newer is required (found: $($goSupport.RawVersion)). Please upgrade Go or use the fallback build."
}

if (-not $installed) {
    $installed = Install-FromSource
}

if ($installed) {
    Verify-Install | Out-Null
    Write-Success "Installation complete. Run 'bd quickstart' inside a repo to begin."
} else {
    Write-Err "Installation failed. Please install Go 1.24+ and try again."
    exit 1
}
