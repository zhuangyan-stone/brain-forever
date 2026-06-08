# ============================================
# BrainForever Launcher (PowerShell)
# Reads .env, sets environment variables,
# then starts local-server.exe
# ============================================

# Set console output to UTF-8 so Chinese characters display correctly
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new()

Write-Host "=== 脑力在线 BrainForever Launcher ===" -ForegroundColor Cyan
Write-Host ""

# --------------------------------------------------
# 1. Check if .env exists
# --------------------------------------------------
if (-not (Test-Path ".env")) {
    Write-Host "[ERROR] .env file not found! Please create .env first." -ForegroundColor Red
    Read-Host "Press Enter to exit"
    exit 1
}

# --------------------------------------------------
# 2. Load .env — parse each non-empty, non-comment line
#    and set it as a process-level environment variable.
#    Lines with # after the value are also handled.
# --------------------------------------------------
Write-Host "[1/3] Loading environment variables from .env..." -ForegroundColor Yellow

Get-Content ".env" | ForEach-Object {
    $line = $_ -replace '\s+$', ''  # trim trailing whitespace

    # Skip empty lines and comment lines (starting with #)
    if (-not $line -or $line -match '^\s*#') {
        return
    }

    # Strip inline comment (respecting double-quotes)
    $stripped = ''
    $inQuote = $false
    for ($i = 0; $i -lt $line.Length; $i++) {
        $c = $line[$i]
        if ($c -eq '#' -and -not $inQuote) { break }
        if ($c -eq '"') { $inQuote = -not $inQuote }
        $stripped += $c
    }
    $stripped = $stripped -replace '\s+$', ''  # trim again after stripping

    if (-not $stripped) { return }

    # Split on first =
    $eqIndex = $stripped.IndexOf('=')
    if ($eqIndex -le 0) { return }  # no key, or no value

    $key = $stripped.Substring(0, $eqIndex)
    $val = $stripped.Substring($eqIndex + 1)

    # Remove surrounding quotes from value if present
    if ($val.Length -ge 2 -and $val[0] -eq '"' -and $val[-1] -eq '"') {
        $val = $val.Substring(1, $val.Length - 2)
    }

    # Set the environment variable for the current process
    [Environment]::SetEnvironmentVariable($key, $val, "Process")
    Write-Host "  set $key" -ForegroundColor Green
}

Write-Host ""
Write-Host "[2/3] Environment variables loaded successfully." -ForegroundColor Yellow
Write-Host ""

# --------------------------------------------------
# 3. Start local-server.exe
# --------------------------------------------------
Write-Host "[3/3] Starting local-server.exe..." -ForegroundColor Yellow
Write-Host ""

if (-not (Test-Path "local-server.exe")) {
    Write-Host "[ERROR] local-server.exe not found! Please build first with b.bat." -ForegroundColor Red
    Read-Host "Press Enter to exit"
    exit 1
}

Write-Host "============================================" -ForegroundColor Cyan
Write-Host "  BrainForever (local-server) is starting..." -ForegroundColor Cyan
Write-Host "  Open http://localhost:8080 in your browser" -ForegroundColor Cyan
Write-Host "  Press Ctrl+C to stop the server" -ForegroundColor Cyan
Write-Host "============================================" -ForegroundColor Cyan
Write-Host ""

# Start local-server.exe
& ".\local-server.exe"
$exitCode = $LASTEXITCODE

if ($exitCode -ne 0) {
    Write-Host ""
    Write-Host "[ERROR] local-server.exe exited with code $exitCode" -ForegroundColor Red
}

exit $exitCode
