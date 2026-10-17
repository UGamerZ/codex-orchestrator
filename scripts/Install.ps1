param([ValidateSet('native-windows','docker')][string]$Backend,[switch]$CheckPrerequisites)
$ErrorActionPreference='Stop'
$root=Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $root
try {
 Write-Host "`nCodex Orchestrator - installation wizard`n" -ForegroundColor Cyan
 if (!$Backend) {
  Write-Host '1. Windows sandbox (no Docker or VM)'
  Write-Host '2. Docker Engine inside an existing Ubuntu WSL distribution'
  $choice=Read-Host 'Choose installation type [1]'
  if ($choice -and $choice -notin @('1','2')) { throw 'Choose 1 or 2.' }
  $Backend=if($choice -eq '2'){'docker'}else{'native-windows'}
 }
 foreach($command in @('git')) {
  if (!(Get-Command $command -ErrorAction SilentlyContinue)) {
   $id=if($command -eq 'git'){'Git.Git'}else{'GitHub.cli'}
   throw "Missing $command. Install it with: winget install --id $id . Then reopen this installer."
  }
 }
 if ((!(Test-Path -LiteralPath 'VERSION' -PathType Leaf) -or !(Test-Path -LiteralPath 'bin/orchestrator.exe' -PathType Leaf)) -and !(Get-Command go -ErrorAction SilentlyContinue)) {
  throw 'Download the Windows release ZIP (it includes the program), or install Go to build this source checkout.'
 }
 $codexPath=''
 if($Backend -eq 'native-windows') {
  $codex=Get-Command codex.exe -ErrorAction SilentlyContinue
  if($codex){$codexPath=$codex.Source}
  if(!$codexPath){
   $shim=Get-Command codex -ErrorAction SilentlyContinue
   if($shim){
    $npmRoot=Join-Path (Split-Path -Parent $shim.Source) 'node_modules/@openai/codex'
    if(Test-Path -LiteralPath $npmRoot){
     $candidate=Get-ChildItem -LiteralPath $npmRoot -Recurse -Filter codex.exe | Select-Object -First 1
     if($candidate){$codexPath=$candidate.FullName}
    }
   }
  }
  if(!$codexPath) {
   $releaseRoot=Join-Path $env:USERPROFILE '.codex/packages/standalone/releases'
   if(Test-Path -LiteralPath $releaseRoot){
    $candidate=Get-ChildItem -LiteralPath $releaseRoot -Recurse -Filter codex.exe | Sort-Object LastWriteTime -Descending | Select-Object -First 1
    if($candidate){$codexPath=$candidate.FullName}
   }
  }
  if(!$codexPath -and !$CheckPrerequisites){$codexPath=Read-Host 'Full path to your installed codex.exe'}
  if(!$codexPath -or !(Test-Path -LiteralPath $codexPath -PathType Leaf)){throw 'Install and sign in to Codex CLI first. See https://developers.openai.com/codex/cli/' }
 } else {
  if(!(Get-Command wsl.exe -ErrorAction SilentlyContinue)){throw 'Install Ubuntu WSL and Linux Docker Engine first; see INSTALL.md.'}
 }
 if($CheckPrerequisites){Write-Host 'Prerequisite checks passed.';return}
 if($Backend -eq 'native-windows'){
  $actor=[Security.Principal.WindowsIdentity]::GetCurrent()
  if(!(New-Object Security.Principal.WindowsPrincipal($actor)).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)){
   Write-Host 'Windows sandbox setup requires elevation. Accept the Windows UAC prompt to continue.'
   $args="-NoProfile -ExecutionPolicy Bypass -File `"$PSCommandPath`" -Backend native-windows"
   $child=Start-Process powershell.exe -ArgumentList $args -Verb RunAs -Wait -PassThru
   if($child.ExitCode -ne 0){throw 'Elevated setup did not finish.'};return
  }
 }
 $config=if($Backend -eq 'native-windows'){'config.native.json'}else{'config.json'}
 Write-Host "`nStep 1/4 - project and GitHub account" -ForegroundColor Cyan
 if(Test-Path -LiteralPath $config){Write-Host "Keeping $config. Use the dashboard later to add or edit environments."}
 else{& "$PSScriptRoot/Configure-Project.ps1" -Backend $Backend -Config $config -CodexPath $codexPath}
 Write-Host "`nStep 2/4 - Trello, Telegram and Codex login" -ForegroundColor Cyan
 & "$PSScriptRoot/Configure-Integrations.ps1" -Config $config
 if($Backend -eq 'docker'){
  $settings=Get-Content -LiteralPath $config -Raw -Encoding UTF8 | ConvertFrom-Json
  $answer=Read-Host "Ubuntu WSL distribution [$($settings.docker_distro)]"
  if($answer){$settings.docker_distro=$answer;[IO.File]::WriteAllText((Join-Path $root $config),($settings|ConvertTo-Json -Depth 12),(New-Object Text.UTF8Encoding($false)))}
 }
 Write-Host "`nStep 3/4 - prepare and verify the execution backend" -ForegroundColor Cyan
 if($Backend -eq 'native-windows'){& "$PSScriptRoot/Setup-Native.ps1" -Config $config}
 else{& "$PSScriptRoot/Setup.ps1" -Config $config
  $settings=Get-Content $config -Raw | ConvertFrom-Json
  $linuxRoot=(& wsl.exe -d $settings.docker_distro -u root --exec wslpath -a $root).Trim()
  if($LASTEXITCODE -ne 0){throw 'Cannot map the installation into WSL.'}
  & wsl.exe -d $settings.docker_distro -u root --exec sh "$linuxRoot/scripts/check-isolation.sh"
  if($LASTEXITCODE -ne 0){throw 'Docker isolation checks failed.'}
 }
 Write-Host "`nStep 4/4 - check configuration and integrations (no agents started)" -ForegroundColor Cyan
 & "$PSScriptRoot/Start-Orchestrator.ps1" -Config $config -Check
 [IO.File]::WriteAllText((Join-Path $root 'installation.json'),(@{config=$config}|ConvertTo-Json),(New-Object Text.UTF8Encoding($false)))
 Write-Host "`nReady. Double-click Start.cmd when you want to start processing tasks." -ForegroundColor Green
 Write-Host 'Dashboard: http://127.0.0.1:8787 . Keep this installation folder private.'
} catch {Write-Host ("`nInstallation stopped: "+$_.Exception.Message) -ForegroundColor Red;exit 1}
