@echo off
REM ============================================
REM BrainForever Launcher
REM Reads .env, sets environment variables,
REM then starts brain-forever.exe
REM ============================================

setlocal enabledelayedexpansion

echo === 脑力在线 BrainForever Launcher ===
echo.

REM --------------------------------------------------
REM 1. Check if .env exists
REM --------------------------------------------------
if not exist ".env" (
    echo [ERROR] .env file not found! Please create .env first.
    pause
    exit /b 1
)

REM --------------------------------------------------
REM 2. Load .env — parse each non-empty, non-comment line
REM    and set it as a permanent environment variable.
REM    Lines with # after the value are also handled.
REM --------------------------------------------------
echo [1/3] Loading environment variables from .env...

for /f "usebackq tokens=*" %%A in (".env") do (
    set "_line=%%A"
    
    REM Skip empty lines and comment lines (starting with # or REM)
    if not "!_line!"=="" (
        set "_first=!_line:~0,1!"
        if not "!_first!"=="#" (
            REM Remove inline comments (everything from # onwards)
            set "_clean="
            set "_in_quote=0"
            for /l %%i in (0,1,512) do (
                if "!_line:~%%i,1!"=="" set "_clean=!_line:~0,%%i!" && goto :break_loop
                if "!_line:~%%i,1!"=="#" (
                    if !_in_quote! equ 0 (
                        set "_clean=!_line:~0,%%i!"
                        goto :break_loop
                    )
                )
                if "!_line:~%%i,1!"==""^" (
                    if !_in_quote! equ 0 (set "_in_quote=1") else (set "_in_quote=0")
                )
            )
            :break_loop
            if "!_clean!"=="" set "_clean=!_line!"

            REM Trim trailing whitespace from _clean
            :trim_loop
            if "!_clean:~-1!"==" " set "_clean=!_clean:~0,-1!" && goto :trim_loop
            if "!_clean:~-1!"=="	" set "_clean=!_clean:~0,-1!" && goto :trim_loop

            if not "!_clean!"=="" (
                REM Split on first =
                for /f "tokens=1,* delims==" %%B in ("!_clean!") do (
                    set "_key=%%B"
                    set "_val=%%C"
                    
                    REM Remove surrounding quotes from value if present
                    if defined _val (
                        if "!_val:~0,1!"==""^" (
                            if "!_val:~-1!"==""^" (
                                set "_val=!_val:~1,-1!"
                            )
                        )
                    )
                    
                    REM Set the environment variable
                    set "!_key!=!_val!"
                    echo   set !_key!=!_val!
                )
            )
        )
    )
)

echo.
echo [2/3] Environment variables loaded successfully.
echo.

REM --------------------------------------------------
REM 3. Start brain-forever.exe
REM --------------------------------------------------
echo [3/3] Starting brain-forever.exe...
echo.

if not exist "brain-forever.exe" (
    echo [ERROR] brain-forever.exe not found! Please build first with b.bat.
    pause
    exit /b 1
)

echo ============================================
echo   BrainForever is starting...
echo   Open http://localhost:8080 in your browser
echo   Press Ctrl+C to stop the server
echo ============================================
echo.

brain-forever.exe

if %ERRORLEVEL% NEQ 0 (
    echo.
    echo [ERROR] brain-forever.exe exited with code %ERRORLEVEL%
    pause
    exit /b %ERRORLEVEL%
)

endlocal
