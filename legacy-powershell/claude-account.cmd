@echo off
rem Claude Account Manager launcher. Lives on PATH; the real logic is in ..\lib\claude-account.ps1.
powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File "%~dp0..\lib\claude-account.ps1" %*
exit /b %ERRORLEVEL%
