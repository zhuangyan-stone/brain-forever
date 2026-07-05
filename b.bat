@echo off
REM ============================================
REM BrainForever Build Script (Windows)
REM Sets CGO and GCC path, builds brain-forever
REM ============================================

REM Set console encoding to UTF-8 (Windows)
chcp 65001 >nul

setlocal

REM Set GCC path (adjust if your MinGW is elsewhere)
set "PATH=C:\msys64\ucrt64\bin;%PATH%"

REM Enable CGO (required for go-sqlite3)
set "CGO_ENABLED=1"

echo === d2Brain Builder ===
echo.

REM Tidy dependencies
echo [1/3] go mod tidy...
call go mod tidy
if %ERRORLEVEL% NEQ 0 (
    echo [ERROR] go mod tidy failed
    exit /b %ERRORLEVEL%
)

REM Build
echo [2/3] Building brain-forever...
go build -o brain-forever.exe .\cmd\local-server\
if %ERRORLEVEL% NEQ 0 (
    echo [ERROR] brain-forever build failed
    exit /b %ERRORLEVEL%
)

echo.
echo [3/3] Build success!
echo   - brain-forever.exe
echo.

endlocal
