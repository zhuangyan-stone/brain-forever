@echo off
REM ============================================
REM BrainForever Build Script (Windows)
REM Builds brain-forever
REM ============================================

REM Set console encoding to UTF-8 (Windows)
chcp 65001 >nul

setlocal

echo === d2Brain Builder ===
echo.

REM Ensure bin directory exists
if not exist "bin" mkdir bin

REM Tidy dependencies
echo [1/3] go mod tidy...
call go mod tidy
if %ERRORLEVEL% NEQ 0 (
    echo [ERROR] go mod tidy failed
    exit /b %ERRORLEVEL%
)

REM Build
echo [2/3] Building brain-forever...
go build -o bin\brain-forever.exe .\cmd\server\
if %ERRORLEVEL% NEQ 0 (
    echo [ERROR] brain-forever build failed
    exit /b %ERRORLEVEL%
)

echo.
echo [3/3] Build success!
echo   - bin\brain-forever.exe
echo.

endlocal
