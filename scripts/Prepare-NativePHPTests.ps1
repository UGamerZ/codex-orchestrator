param(
  [Parameter(Mandatory=$true)][string]$Task,
  [string]$Config='config.native.json',
  [string]$PHPSource='C:\xampp\php',
  [string]$ComposerPhar='C:\ProgramData\ComposerSetup\bin\composer.phar'
)
$ErrorActionPreference='Stop'
$repoRoot=Split-Path -Parent $PSScriptRoot
$cfg=Get-Content -LiteralPath (Join-Path $repoRoot $Config) -Raw -Encoding UTF8|ConvertFrom-Json
if($cfg.backend -ne 'native-windows' -or $Task -notmatch '^[a-z0-9][a-z0-9-]{0,63}$'){throw 'A valid native task ID is required'}
$state=Get-Content -LiteralPath (Join-Path $cfg.state_dir 'state.json') -Raw -Encoding UTF8|ConvertFrom-Json
if(!$state.paused -or @($state.jobs.PSObject.Properties.Value|Where-Object {$_.stage -in @('running','publishing')}).Count){throw 'Pause scheduling and wait for active workers before preparing dependencies'}
$workspace=Join-Path (Join-Path (Join-Path $cfg.native_root 'jobs') $Task) 'workspace'
function Assert-PlainPath([string]$path){
  $current=[IO.Path]::GetFullPath($path)
  while($current){if((Test-Path -LiteralPath $current) -and ((Get-Item -LiteralPath $current -Force).Attributes -band [IO.FileAttributes]::ReparsePoint)){throw 'Linked preparation path is not allowed'};$parent=Split-Path -Parent $current;if($parent -eq $current){break};$current=$parent}
}
Assert-PlainPath $workspace
Assert-PlainPath (Join-Path $workspace 'vendor')
$manifestPath=Join-Path $workspace 'composer.json'
$lockPath=Join-Path $workspace 'composer.lock'
$manifest=Get-Content -LiteralPath $manifestPath -Raw|ConvertFrom-Json
$lock=Get-Content -LiteralPath $lockPath -Raw|ConvertFrom-Json
if($manifest.repositories){throw 'Review custom Composer repositories separately before provisioning'}
if(($manifest.config.'vendor-dir' -and $manifest.config.'vendor-dir' -ne 'vendor') -or ($manifest.config.'bin-dir' -and $manifest.config.'bin-dir' -ne 'vendor/bin')){throw 'Custom Composer output directories require separate review'}
foreach($autoload in @($manifest.autoload,$manifest.'autoload-dev')){if(!$autoload){continue};foreach($entry in $autoload.PSObject.Properties){$paths=if($entry.Name -in @('psr-0','psr-4')){@($entry.Value.PSObject.Properties.Value)}else{@($entry.Value)};foreach($path in $paths){foreach($part in @($path)){if($part -is [string] -and ([IO.Path]::IsPathRooted($part) -or $part -match '(^|[\\/])\.\.([\\/]|$)|:')){throw 'Composer autoload paths must stay in the project'}}}}}
foreach($package in @($lock.packages)+@($lock.'packages-dev')){
  if($package.type -eq 'composer-plugin' -or $package.dist.url -notmatch '^https://api\.github\.com/repos/'){throw 'Dependency source/type needs separate review'}
}
$manifestHash=(Get-FileHash -LiteralPath $manifestPath -Algorithm SHA256).Hash.ToLowerInvariant()
$lockHash=(Get-FileHash -LiteralPath $lockPath -Algorithm SHA256).Hash.ToLowerInvariant()
$relative='toolchains/php/'+$manifestHash.Substring(0,16)+'-'+$lockHash.Substring(0,16)
$control=Join-Path $cfg.native_root 'control'
$snapshot=Join-Path $control $relative
$runtime=Join-Path $snapshot 'php'
Assert-PlainPath $snapshot
New-Item -ItemType Directory -Path $runtime,(Join-Path $runtime 'ext') -Force|Out-Null
Copy-Item -LiteralPath (Join-Path $PHPSource 'php.exe') -Destination $runtime -Force
Get-ChildItem -LiteralPath $PHPSource -Filter '*.dll' -File|Copy-Item -Destination $runtime -Force
Get-ChildItem -LiteralPath (Join-Path $PHPSource 'ext') -Filter '*.dll' -File|Copy-Item -Destination (Join-Path $runtime 'ext') -Force
Copy-Item -LiteralPath $ComposerPhar -Destination (Join-Path $runtime 'composer.phar') -Force
$extensions=@('curl','fileinfo','mbstring','mysqli','openssl','pdo_mysql','pdo_sqlite','zip')
$extensions=@($extensions|Where-Object {Test-Path -LiteralPath (Join-Path $runtime ('ext\php_'+$_+'.dll'))})
if(Test-Path -LiteralPath (Join-Path $runtime 'ext\php_gd2.dll')){$extensions+='gd2'}else{$extensions+='gd'}
$ini="extension_dir=`"@PHP_RUNTIME@/ext`"`nmemory_limit=512M`ndate.timezone=UTC`nsession.save_path=`"`${TEMP}`"`n"+(($extensions|ForEach-Object {'extension='+$_}) -join "`n")
$utf8=New-Object Text.UTF8Encoding($false)
[IO.File]::WriteAllText((Join-Path $runtime 'php.ini'),$ini,$utf8)
$installIni=Join-Path $snapshot 'install.ini'
[IO.File]::WriteAllText($installIni,$ini.Replace('@PHP_RUNTIME@',$runtime.Replace('\','/')),$utf8)
$env:COMPOSER_HOME=Join-Path $snapshot 'composer-home'
$env:COMPOSER_CACHE_DIR=Join-Path $control 'composer-cache'
$env:COMPOSER_AUTH='{}'
$env:COMPOSER_NO_INTERACTION='1'
Remove-Item Env:GH_TOKEN,Env:GITHUB_TOKEN,Env:TRELLO_KEY,Env:TRELLO_TOKEN,Env:TELEGRAM_TOKEN,Env:OPENAI_API_KEY,Env:CODEX_API_KEY -ErrorAction SilentlyContinue
& (Join-Path $runtime 'php.exe') -c $installIni (Join-Path $runtime 'composer.phar') install --working-dir=$workspace --no-scripts --no-plugins --prefer-dist --no-interaction --no-progress
if($LASTEXITCODE -ne 0){throw 'Locked dependency installation failed; no project scripts or plugins were enabled'}
$vendor=Join-Path $snapshot 'vendor'
if(!(Test-Path -LiteralPath $vendor)){Copy-Item -LiteralPath (Join-Path $workspace 'vendor') -Destination $vendor -Recurse}
$profile=@{version=1;php=($relative+'/php');vendor=($relative+'/vendor');composer_manifest_sha256=$manifestHash;composer_lock_sha256=$lockHash}
[IO.File]::WriteAllText((Join-Path $control 'native-php-tests.json'),($profile|ConvertTo-Json),$utf8)
Write-Output 'Approved PHP runtime and locked dependencies prepared. Project code/tests were not executed on the host.'
