# Builds dist\vkvm.exe and VKVM-Setup.exe. Used by CI and for local builds:
#
#   powershell -ExecutionPolicy Bypass -File packaging\windows\build.ps1
#
# Code signing is optional and off by default. To sign, set
#   WINDOWS_CERT_PFX       path to a .pfx code-signing certificate
#   WINDOWS_CERT_PASSWORD  its password
# (and optionally WINDOWS_TIMESTAMP_URL). signtool.exe must be on PATH or in
# the Windows SDK.
param([string]$Root = "$PSScriptRoot\..\..")
$ErrorActionPreference = "Stop"
Push-Location $Root
try {
  $version = (Get-Content VERSION -Raw).Trim()
  New-Item -ItemType Directory -Force dist | Out-Null

  # ControlMyMonitor.exe is gitignored inside the embed directory; without it
  # the build has no DDC backend.
  Copy-Item tools\ControlMyMonitor.exe internal\embedded\tools\ControlMyMonitor.exe -Force

  # Icon files, and the icon as a Windows resource so Explorer shows it.
  go run ./packaging/icon dist\icon
  if ($LASTEXITCODE -ne 0) { throw "icon generation failed" }
  go run github.com/akavel/rsrc@v0.10.2 -ico dist\icon\vkvm.ico -arch amd64 -o cmd\rsrc_windows_amd64.syso
  if ($LASTEXITCODE -ne 0) { throw "icon resource failed" }

  go build -trimpath -ldflags "-H windowsgui -X main.version=$version" -o dist\vkvm.exe ./cmd
  if ($LASTEXITCODE -ne 0) { throw "go build failed" }
  Remove-Item cmd\rsrc_windows_amd64.syso -ErrorAction SilentlyContinue

  function Sign-File([string]$path) {
    if (-not $env:WINDOWS_CERT_PFX) { return }
    $signtool = Get-Command signtool.exe -ErrorAction SilentlyContinue
    if ($null -eq $signtool) {
      $signtool = Get-ChildItem "${env:ProgramFiles(x86)}\Windows Kits\10\bin\*\x64\signtool.exe" -ErrorAction SilentlyContinue |
        Sort-Object FullName -Descending | Select-Object -First 1
    }
    if ($null -eq $signtool) { throw "signtool.exe not found" }
    $exe = if ($signtool -is [System.Management.Automation.CommandInfo]) { $signtool.Source } else { $signtool.FullName }
    $ts = if ($env:WINDOWS_TIMESTAMP_URL) { $env:WINDOWS_TIMESTAMP_URL } else { "http://timestamp.digicert.com" }
    & $exe sign /fd SHA256 /td SHA256 /tr $ts /f $env:WINDOWS_CERT_PFX /p $env:WINDOWS_CERT_PASSWORD $path
    if ($LASTEXITCODE -ne 0) { throw "signing $path failed" }
  }

  Sign-File dist\vkvm.exe
  powershell -ExecutionPolicy Bypass -File packaging\windows\test-installer.ps1 -Version $version
  if ($LASTEXITCODE -ne 0) { throw "installer build failed" }
  if (Test-Path VKVM-Setup.exe) { Sign-File VKVM-Setup.exe }
} finally {
  Pop-Location
}
