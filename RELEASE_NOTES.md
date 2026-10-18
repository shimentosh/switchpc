# VKVM 0.3.0

## Install and go

- **Own app window.** VKVM now has a native window (WebView2 on Windows, WebKit on
  macOS) instead of opening browser tabs, and only one copy of VKVM runs at a time:
  starting it again just brings the window forward.
- **First-run setup.** Name the computer, choose *Main* or *Second computer*, and
  VKVM reads which monitor input this computer is on.
- **6-digit pairing.** The second computer finds the main one on the network and
  pairs with a code, with no tokens, IP addresses or config files to edit. The main
  computer learns which input the second computer uses on each monitor.
- **Dashboard.** Switch buttons, per-monitor input editing with a *Test* button,
  paired computers, hotkey recorder, start at login, and macOS permission prompts.
- **Keyboard and mouse sharing** is a single toggle on the main computer, off by
  default.
- **Real icon** in the tray/menu bar, app, installer and DMG. The tray menu updates
  when profiles change.
- **Windows installer** no longer installs a background service. It sets up the
  firewall rule (so VKVM never asks for admin rights while running), adds shortcuts,
  and offers to open VKVM. Upgrades remove the 0.2.x service.
- **macOS DMG** has the drag-to-Applications layout and an app icon.
- VKVM restarts itself when a change needs it, and logs to `vkvm.log` in the user
  cache folder.

## Fixes

- Monitors that report HDMI 1 are no longer rewritten to DisplayPort 1.
- On macOS, *Quit* no longer gets undone by the login item relaunching VKVM.

## Notes

- Builds are not code-signed. See the README for the one-time steps on Windows
  (SmartScreen) and macOS (Gatekeeper); the build scripts accept signing identities.

# VKVM 0.2.2

## Fixes

- Windows release builds now embed ControlMyMonitor.exe, so DDC switching
  works out of the box instead of failing with "required tool not found".
- Windows also looks for ControlMyMonitor.exe next to vkvm.exe (and in a
  `tools` folder beside it) instead of a hardcoded `D:kvm` path.
- README documents how to open the unsigned macOS build past Gatekeeper.

# VKVM 0.2.1

## Fixes

- The `ddc` package now builds on platforms without a DDC backend.
- Release builds publish automatically when a `v*` tag is pushed.

# VKVM 0.2.0

## Highlights

- Safe-by-default authenticated API, WebSocket and UDP forwarding with paired
  device identities, session MACs, replay protection and release-all recovery.
- Ordered Raw Input forwarding with per-agent transport negotiation, calibration,
  latency/loss metrics and network-change-aware discovery.
- macOS LaunchAgent and Windows companion/service packaging foundations,
  permission diagnostics, authenticated core/companion IPC and bounded service
  supervision.
- Optional ESP32-S3 USB/BLE HID report ABI and a transport-neutral BLE HID
  profile/session prototype.

## Verification

The release gate runs formatting, vet, unit/race tests, coverage reporting and
macOS arm64/Windows amd64 builds. Physical HID/BLE, DDC multi-machine and
notarization/SmartScreen acceptance remain release-environment tests.
