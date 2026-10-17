@echo off
cd /d "%~dp0"
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0scripts\Install.ps1"
if errorlevel 1 echo Installation did not finish. Read the message above, fix it and run Install.cmd again.
pause
