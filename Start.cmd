@echo off
cd /d "%~dp0"
if not exist installation.json (
  echo Run Install.cmd first.
  pause
  exit /b 1
)
powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "$ErrorActionPreference='Stop'; $i=Get-Content -LiteralPath installation.json -Raw | ConvertFrom-Json; & '.\scripts\Start-Orchestrator.ps1' -Config $i.config"
if errorlevel 1 pause
