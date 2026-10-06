param(
 [ValidateSet('github','local')][string]$Mode,
 [ValidateSet('docker','native-windows')][string]$Backend,
 [string]$Config,
 [string]$Repository,
 [string]$GitHubUser,
 [string]$BaseBranch,
 [string]$LocalPath,
 [switch]$Publish,
 [switch]$LocalOnly
)
$ErrorActionPreference='Stop'
if ($Publish -and $LocalOnly) { throw 'Publish and LocalOnly are mutually exclusive' }
if ($Publish -and $Mode -eq 'github') { throw 'Publish is only supported for a new local project' }
$root=Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $root
function Ask([string]$value,[string]$prompt,[string]$default='') {
 if ($value) { return $value }
 $answer=Read-Host "$prompt [$default]"
 if (!$answer) { return $default }; return $answer
}
if (!$Backend) {
 $choice=Ask '' 'Execution: 1 Docker, 2 restricted Windows' '1'
 $Backend=if ($choice -eq '2') {'native-windows'} else {'docker'}
}
if (!$Mode) {
 $choice=Ask '' 'Project: 1 existing GitHub repository, 2 local development' '1'
 $Mode=if ($choice -eq '2') {'local'} else {'github'}
}
if (!$Config) { $Config=if ($Backend -eq 'native-windows') {'config.native.json'} else {'config.json'} }
if (Test-Path -LiteralPath $Config) {
 if ((Read-Host "Configuration $Config exists. Type REPLACE to overwrite it") -ne 'REPLACE') { throw 'Existing configuration retained' }
}
$template=if ($Backend -eq 'native-windows') {'config.native.example.json'} else {'config.example.json'}
$settings=Get-Content -LiteralPath $template -Raw | ConvertFrom-Json
$settings.project_mode=$Mode
$settings.backend=$Backend
$BaseBranch=Ask $BaseBranch 'Base branch' 'main'
git check-ref-format --branch $BaseBranch | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'Invalid base branch' }
$settings.base_branch=$BaseBranch
$utf8=New-Object Text.UTF8Encoding($false)
if ($Mode -eq 'local') {
 $LocalPath=Ask $LocalPath 'New EMPTY directory for the local project' ''
 if (!$LocalPath) { throw 'Local project directory is required' }
 $LocalPath=[IO.Path]::GetFullPath($LocalPath)
 if ((Test-Path -LiteralPath $LocalPath) -and (Get-ChildItem -LiteralPath $LocalPath -Force | Select-Object -First 1)) { throw 'Choose an empty project directory; existing files are never staged automatically' }
 New-Item -ItemType Directory -Path $LocalPath -Force | Out-Null
 git init --initial-branch $BaseBranch $LocalPath | Out-Null
 if ($LASTEXITCODE -ne 0) { throw 'Cannot initialize local project' }
 [IO.File]::WriteAllText((Join-Path $LocalPath 'README.md'),"# New project`n",$utf8)
 git -C $LocalPath -c core.hooksPath=NUL add -- README.md
 git -C $LocalPath -c core.hooksPath=NUL -c 'user.name=Project Owner' -c 'user.email=project-owner@localhost' commit -m 'Initialize project' | Out-Null
 if ($LASTEXITCODE -ne 0) { throw 'Cannot initialize baseline commit' }
 if (!$Publish -and !$LocalOnly) { $Publish=(Read-Host 'Create a private GitHub repository for this project? [y/N]') -eq 'y' }
 if ($Publish) { $Mode='github';$settings.project_mode='github' }
 else {
  $settings.repository='';$settings.github_user=''
  # Controller uses a private bare Git store; agents never receive this path or its Git metadata.
  $store=Join-Path $LocalPath '.orchestrator.git'
  git clone --bare --no-hardlinks $LocalPath $store | Out-Null
  if ($LASTEXITCODE -ne 0) { throw 'Cannot create local publication store' }
  $settings | Add-Member -NotePropertyName local_repository -NotePropertyValue $store -Force
  Write-Host "Local branches will be stored in $store. Import a reviewed branch using git fetch .orchestrator.git codex/TASK-ID."
 }
}
if ($Mode -eq 'github') {
 $gh=Get-Command gh -ErrorAction SilentlyContinue
 $ghPath=if ($gh) {$gh.Source} else {'C:\Program Files\GitHub CLI\gh.exe'}
 if (!(Test-Path -LiteralPath $ghPath)) { throw 'Install GitHub CLI first' }
 $settings.gh_path=$ghPath
 $GitHubUser=Ask $GitHubUser 'GitHub account with WRITE access to the project' ''
 if ($GitHubUser -notmatch '^[A-Za-z0-9][A-Za-z0-9-]*$') { throw 'Invalid GitHub login' }
 $originalGhToken=$env:GH_TOKEN;$originalGithubToken=$env:GITHUB_TOKEN;$originalHost=$env:GH_HOST
 try {
  Remove-Item Env:GH_TOKEN,Env:GITHUB_TOKEN,Env:GH_HOST -ErrorAction SilentlyContinue
  $token=& $ghPath auth token --hostname github.com --user $GitHubUser 2>$null
  if ($LASTEXITCODE -ne 0) {
   Write-Host "Sign in as $GitHubUser using the device code shown by GitHub CLI."
   & $ghPath auth login --hostname github.com --git-protocol https --web
   if ($LASTEXITCODE -ne 0) { throw 'GitHub login failed' }
   $token=& $ghPath auth token --hostname github.com --user $GitHubUser
   if ($LASTEXITCODE -ne 0) { throw 'The selected account is not authorized' }
  }
  $env:GH_TOKEN=$token;$env:GH_HOST='github.com'
  $identity=& $ghPath api user --jq '.login'
  if ($LASTEXITCODE -ne 0 -or $identity -ine $GitHubUser) { throw 'Authenticated account differs from the selected account' }
  $Repository=Ask $Repository 'Repository OWNER/NAME or https://github.com/OWNER/NAME' ''
  $Repository=$Repository -replace '^https://github.com/','' -replace '\.git$','' -replace '/$',''
  if ($Repository -notmatch '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$') { throw 'Invalid GitHub repository' }
  if ($Publish) {
   & $ghPath repo create $Repository --private
   if ($LASTEXITCODE -ne 0) { throw 'Repository creation failed' }
   $oldPath=$env:PATH
   try {
    $env:PATH=(Split-Path -Parent $ghPath)+';'+$env:PATH
    git -C $LocalPath -c 'credential.helper=' -c 'credential.helper=!gh auth git-credential' push "https://github.com/$Repository.git" "${BaseBranch}:refs/heads/$BaseBranch"
    if ($LASTEXITCODE -ne 0) { throw 'Initial publication failed' }
   } finally { $env:PATH=$oldPath }
  }
  $repoJson=& $ghPath api "repos/$Repository"
  if ($LASTEXITCODE -ne 0) { throw 'Repository is unavailable to the selected account' }
  $repoInfo=$repoJson | ConvertFrom-Json
  if ($repoInfo.archived -or !$repoInfo.permissions.push) { throw 'The selected account needs write access to an active repository' }
  $encodedBranch=[Uri]::EscapeDataString($BaseBranch)
  & $ghPath api "repos/$Repository/branches/$encodedBranch" --jq '.name' | Out-Null
  if ($LASTEXITCODE -ne 0) { throw 'Selected base branch does not exist' }
  $settings.repository=$Repository;$settings.github_user=$identity
 } finally {
  $env:GH_TOKEN=$originalGhToken;$env:GITHUB_TOKEN=$originalGithubToken;$env:GH_HOST=$originalHost
 }
}
$projectName=if ($Mode -eq 'local') {Split-Path -Leaf $LocalPath} else {$settings.repository.Replace('/','-')}
$instanceID=[Guid]::NewGuid().ToString('N').Substring(0,8)
$settings.state_dir="./state/$projectName-$instanceID"
if ($Backend -eq 'native-windows') {
 $settings.native_root="C:\ProgramData\CodexOrchestrator\instances\$projectName-$instanceID"
 $settings.native_isolation_verified=$false
 $settings.codex_path=Ask '' 'Full path to codex.exe' $settings.codex_path
}
[IO.File]::WriteAllText([IO.Path]::GetFullPath($Config),($settings | ConvertTo-Json -Depth 10),$utf8)
Write-Host "Saved $Config. Configure Trello, Telegram and Codex authentication, then run isolation setup. No agents were started."
