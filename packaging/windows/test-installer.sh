#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
NSI="$ROOT/packaging/windows/vkvm.nsi"
test -s "$ROOT/VERSION"
# 0.3+: VKVM is a per-user app. The installer must not create a service, but
# must clean up the 0.2.x one, add the program firewall rule and launch VKVM
# unelevated.
if grep -q 'sc.exe.*create' "$NSI"; then
  echo "the installer must not create a Windows service" >&2
  exit 1
fi
grep -q 'delete VKVMCore' "$NSI"
grep -q 'advfirewall firewall add rule' "$NSI"
grep -q 'program=' "$NSI"
grep -q 'LaunchUnelevated' "$NSI"
grep -q 'CreateShortCut' "$NSI"
grep -q 'WriteUninstaller' "$NSI"
grep -q 'InstallLocation' "$NSI"
if grep -q -- '--companion' "$ROOT/internal/autostart/autostart_windows.go"; then
  echo "the startup entry must launch the full app" >&2
  exit 1
fi
grep -q 'ProductVersion' "$ROOT/packaging/windows/vkvm.wxs"
if command -v makensis >/dev/null 2>&1 && [ -s "$ROOT/dist/vkvm.exe" ] && [ -s "$ROOT/dist/icon/vkvm.ico" ]; then
  (cd "$ROOT" && makensis "-DAPP_VERSION=$(tr -d '[:space:]' < VERSION)" packaging/windows/vkvm.nsi >/dev/null)
  test -s "$ROOT/VKVM-Setup.exe"
fi
echo "Windows installer metadata smoke test passed"
