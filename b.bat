@echo off
REM ============================================
REM BrainForever Build Script (Windows)
REM Sets CGO and GCC path, builds local-server and remote-server
REM ============================================

REM Set console encoding to UTF-8 (Windows)
chcp 65001 >nul

setlocal

REM Set GCC path (adjust if your MinGW is elsewhere)
set "PATH=C:\msys64\ucrt64\bin;%PATH%"

REM Enable CGO (required for go-sqlite3)
set "CGO_ENABLED=1"

echo === BrainForever Builder ===
echo.

REM Tidy dependencies
echo [1/5] go mod tidy...
call go mod tidy
if %ERRORLEVEL% NEQ 0 (
    echo [ERROR] go mod tidy failed
    exit /b %ERRORLEVEL%
)

REM Build local-server
echo [2/5] Building local-server...
go build -o local-server.exe .\cmd\local-server\
if %ERRORLEVEL% NEQ 0 (
    echo [ERROR] local-server build failed
    exit /b %ERRORLEVEL%
)

REM Build remote-server
echo [3/5] Building remote-server...
go build -o remote-server.exe .\cmd\remote-server\
if %ERRORLEVEL% NEQ 0 (
    echo [ERROR] remote-server build failed
    exit /b %ERRORLEVEL%
)

echo.
echo [4/5] Build success!
echo   - local-server.exe
echo   - remote-server.exe
echo.

endlocal
