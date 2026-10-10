param([string]$Config='config.native.json',[string]$TaskName='CodexOrchestratorWatchdog')
$ErrorActionPreference='Stop'
$watcher=Join-Path $PSScriptRoot 'Watch-Orchestrator.ps1'
$user=[Security.Principal.WindowsIdentity]::GetCurrent().Name
$arguments='-NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File "'+$watcher+'" -Config "'+$Config+'"'
$action=New-ScheduledTaskAction -Execute 'powershell.exe' -Argument $arguments -WorkingDirectory (Split-Path -Parent $PSScriptRoot)
$trigger=New-ScheduledTaskTrigger -Once -At (Get-Date).AddMinutes(1) -RepetitionInterval (New-TimeSpan -Minutes 1)
$principal=New-ScheduledTaskPrincipal -UserId $user -LogonType Interactive -RunLevel Highest
$settings=New-ScheduledTaskSettingsSet -MultipleInstances IgnoreNew -ExecutionTimeLimit (New-TimeSpan -Minutes 2) -StartWhenAvailable
Register-ScheduledTask -TaskName $TaskName -Action $action -Trigger $trigger -Principal $principal -Settings $settings -Description 'Check controller health every minute; notify Telegram and recover a stopped controller without duplicating live workers.' -Force | Out-Null
Write-Output ('Watchdog installed for '+$user+'. It runs while that Windows user is logged in, including disconnected RDP sessions.')