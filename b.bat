@echo off
REM ============================================
REM BrainForever Build Script
REM Sets CGO and GCC path, then builds
REM ============================================

REM Set console encoding to UTF-8 (Windows)
chcp 65001 >nul

setlocal

REM Set GCC path (adjust if your MinGW is elsewhere)
set "PATH=C:\msys64\ucrt64\bin;%PATH%"

REM Enable CGO
set "CGO_ENABLED=1"

echo === BrainForever Builder ===
echo.

REM Tidy dependencies
echo [1/3] go mod tidy...
call go mod tidy
if %ERRORLEVEL% NEQ 0 (
    echo [ERROR] go mod tidy failed
    exit /b %ERRORLEVEL%
)

REM Build
echo [2/3] go build...
go build -o brain-forever.exe .
if %ERRORLEVEL% NEQ 0 (
    echo [ERROR] build failed
    exit /b %ERRORLEVEL%
)

echo [3/3] Build success: brain-forever.exe
echo.

endlocal
