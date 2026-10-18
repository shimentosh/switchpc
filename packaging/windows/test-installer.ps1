param([string]$Root = "$PSScriptRoot\..\..", [string]$Version = "")
$ErrorActionPreference = "Stop"
$nsi = Get-Content (Join-Path $Root "packaging\windows\vkvm.nsi") -Raw
if ($nsi -match 'sc\.exe.*create') { throw "the installer must not create a Windows service" }
if ($nsi -notmatch 'advfirewall firewall add rule' -or $nsi -notmatch 'program=') { throw "program firewall rule missing" }
if ($nsi -notmatch 'delete VKVMCore') { throw "upgrade cleanup of the 0.2.x service missing" }
if ($nsi -notmatch 'CreateShortCut' -or $nsi -notmatch 'LaunchUnelevated') { throw "shortcut / unelevated launch missing" }
if ($nsi -notmatch 'WriteUninstaller' -or $nsi -notmatch 'InstallLocation') { throw "uninstall metadata missing" }
$autostart = Get-Content (Join-Path $Root "internal\autostart\autostart_windows.go") -Raw
if ($autostart -match '--companion') { throw "startup entry must launch the full app" }
foreach ($f in @("dist\vkvm.exe", "dist\icon\vkvm.ico", "VERSION")) {
  if (-not (Test-Path (Join-Path $Root $f))) { throw "$f missing" }
}
if ($Version -eq "") { $Version = (Get-Content (Join-Path $Root "VERSION") -Raw).Trim() }
$makensis = Get-Command makensis.exe -ErrorAction SilentlyContinue
if ($null -eq $makensis -and (Test-Path "${env:ProgramFiles(x86)}\NSIS\makensis.exe")) {
  $makensis = Get-Command "${env:ProgramFiles(x86)}\NSIS\makensis.exe"
}
if ($null -ne $makensis) {
  Push-Location $Root
  try {
    & $makensis.Source "/DAPP_VERSION=$Version" "packaging\windows\vkvm.nsi"
    if ($LASTEXITCODE -ne 0) { throw "makensis failed" }
    if (-not (Test-Path (Join-Path $Root "VKVM-Setup.exe"))) { throw "NSIS output missing" }
  } finally {
    Pop-Location
  }
}
Write-Host "Windows NSIS installer smoke test passed"
