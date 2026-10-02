@echo off
rem go-websocket launcher ------------------------------------------------------
rem Switches to this script's directory first, then starts the executable next
rem to it, so it works from anywhere (double-click friendly).
rem
rem   start.bat                 uses env.ini next to it when present
rem   start.bat -config x.ini   extra arguments are passed straight to go-websocket
rem
rem The page, the static assets and the default config are embedded in the
rem executable, so it also runs with no config file at all.
rem
rem This file is intentionally ASCII-only: after "chcp 65001" a batch file
rem containing non-ASCII text can make cmd.exe misparse the following lines.
rem The window deliberately stays open after the process exits, so a failed
rem start does not just flash by.
rem ---------------------------------------------------------------------------

rem Switch the console to UTF-8 so the application's Chinese log output is readable
chcp 65001 >nul 2>&1

setlocal
cd /d "%~dp0"

echo [go-websocket] dir: %CD%
echo [go-websocket] page, static assets and the default config are inside the binary.
echo [go-websocket] starting; the HTTP port is 8090 unless env.ini says otherwise.
echo.

go-websocket.exe %*
set "exit_code=%ERRORLEVEL%"

echo.
if "%exit_code%"=="0" (
    echo [go-websocket] exited normally.
) else (
    echo [go-websocket] process exited with code %exit_code%.
    echo [go-websocket] common causes: HTTP port 8090 already in use.
)
echo.
echo [go-websocket] Press any key to close this window...
pause >nul
