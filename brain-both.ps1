# ============================================
# BrainForever + BrainOnline Dual Launcher
# Starts both local-server and remote-server
# simultaneously from a single terminal.
# ============================================

param(
    [switch]$NoBuild = $false
)

# Set console output to UTF-8
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new()

Write-Host "=== 脑力永存 BrainBoth Launcher ===" -ForegroundColor Cyan
Write-Host "  Starting local-server + remote-server simultaneously" -ForegroundColor Cyan
Write-Host ""

# --------------------------------------------------
# 1. Check if .env exists
# --------------------------------------------------
$envFile = ".env"
if (-not (Test-Path $envFile)) {
    Write-Host "[ERROR] .env file not found! Please create .env first." -ForegroundColor Red
    Read-Host "Press Enter to exit"
    exit 1
}

# --------------------------------------------------
# 2. Build if needed
# --------------------------------------------------
if (-not $NoBuild) {
    Write-Host "[1/3] Building binaries..." -ForegroundColor Yellow

    $buildResult = & ".\b.bat"
    if ($LASTEXITCODE -ne 0) {
        Write-Host "[ERROR] Build failed!" -ForegroundColor Red
        Read-Host "Press Enter to exit"
        exit $LASTEXITCODE
    }
    Write-Host "  Build succeeded." -ForegroundColor Green
    Write-Host ""
}

# --------------------------------------------------
# 3. Load .env into process environment
# --------------------------------------------------
Write-Host "[2/3] Loading environment variables from .env..." -ForegroundColor Yellow

Get-Content $envFile | ForEach-Object {
    $line = $_ -replace '\s+$', ''

    # Skip empty lines and comments
    if (-not $line -or $line -match '^\s*#') { return }

    # Strip inline comment
    $stripped = ''
    $inQuote = $false
    for ($i = 0; $i -lt $line.Length; $i++) {
        $c = $line[$i]
        if ($c -eq '#' -and -not $inQuote) { break }
        if ($c -eq '"') { $inQuote = -not $inQuote }
        $stripped += $c
    }
    $stripped = $stripped -replace '\s+$', ''

    if (-not $stripped) { return }

    $eqIndex = $stripped.IndexOf('=')
    if ($eqIndex -le 0) { return }

    $key = $stripped.Substring(0, $eqIndex)
    $val = $stripped.Substring($eqIndex + 1)

    if ($val.Length -ge 2 -and $val[0] -eq '"' -and $val[-1] -eq '"') {
        $val = $val.Substring(1, $val.Length - 2)
    }

    [Environment]::SetEnvironmentVariable($key, $val, "Process")
    Write-Host "  set $key" -ForegroundColor Green
}

Write-Host ""
Write-Host "  Environment loaded." -ForegroundColor Yellow
Write-Host ""

# --------------------------------------------------
# 4. Check executables exist
# --------------------------------------------------
$localExe = "brain-forever.exe"
$remoteExe = "brain-online.exe"

if (-not (Test-Path $localExe)) {
    Write-Host "[ERROR] $localExe not found! Build first or use -NoBuild if already built." -ForegroundColor Red
    Read-Host "Press Enter to exit"
    exit 1
}
if (-not (Test-Path $remoteExe)) {
    Write-Host "[ERROR] $remoteExe not found! Build first or use -NoBuild if already built." -ForegroundColor Red
    Read-Host "Press Enter to exit"
    exit 1
}

# --------------------------------------------------
# 5. Start both servers
# --------------------------------------------------
Write-Host "[3/3] Starting both servers..." -ForegroundColor Yellow
Write-Host ""

Write-Host "============================================" -ForegroundColor Cyan
Write-Host "  local-server : http://localhost:8080" -ForegroundColor Cyan
Write-Host "  remote-server: http://localhost:8088" -ForegroundColor Cyan
Write-Host "  Press Ctrl+C to stop all servers" -ForegroundColor Cyan
Write-Host "============================================" -ForegroundColor Cyan
Write-Host ""

# Start remote-server in background job
$remoteJob = Start-Job -ScriptBlock {
    param($exe)
    & ".\$exe"
} -ArgumentList $remoteExe

# Give remote-server a moment to bind port
Start-Sleep -Milliseconds 500

# Start local-server in foreground (so Ctrl+C works on it)
$localProcess = Start-Process -FilePath ".\$localExe" -NoNewWindow -PassThru

Write-Host ""
Write-Host "[INFO] Both servers are running. Press Ctrl+C to stop." -ForegroundColor Green

# Wait for local-server to exit (Ctrl+C will kill it)
try {
    $localProcess.WaitForExit()
}
finally {
    Write-Host ""
    Write-Host "Shutting down remote-server..." -ForegroundColor Yellow
    Stop-Job -Job $remoteJob -ErrorAction SilentlyContinue
    Remove-Job -Job $remoteJob -ErrorAction SilentlyContinue
    Write-Host "All servers stopped." -ForegroundColor Green
}

exit $localProcess.ExitCode
