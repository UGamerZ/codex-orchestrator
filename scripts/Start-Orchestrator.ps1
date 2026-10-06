param([switch]$Check,[string]$Config='config.json')
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $projectRoot
# The local secrets file stays on the host and is never mounted into agents.
$secretsPath = Join-Path $projectRoot 'secrets\integrations.json'
$integrationSecrets = Get-Content -LiteralPath $secretsPath -Raw | ConvertFrom-Json
$env:TRELLO_KEY = $integrationSecrets.trello_key
$env:TRELLO_TOKEN = $integrationSecrets.trello_token
$env:TELEGRAM_TOKEN = $integrationSecrets.telegram_token
try {
  if ($Check) { & '.\bin\orchestrator.exe' -config $Config -check }
  else { & '.\bin\orchestrator.exe' -config $Config }
  if ($LASTEXITCODE -ne 0) { throw "Orchestrator exited with code $LASTEXITCODE" }
} finally {
  Remove-Item Env:TRELLO_KEY,Env:TRELLO_TOKEN,Env:TELEGRAM_TOKEN -ErrorAction SilentlyContinue
}
