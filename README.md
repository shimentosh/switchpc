# VKVM: a software KVM switch for two computers

[📖 繁體中文版文檔 / Chinese Documentation](./README_zh-TW.md) (describes the older 0.2 setup)

VKVM switches your monitors between two computers (for example a Windows PC and a Mac)
with a hotkey or a click, like a KVM switch without the box. It talks to the monitor over
**DDC/CI**, the same channel the monitor's own buttons use to change input.

Optionally, the main computer's keyboard and mouse can control the second computer while
the monitors show it. This is off by default.

Forked from [aluo96078/vkvm](https://github.com/aluo96078/vkvm) (MIT).

## What you need

- **Each computer connected to the monitor with its own cable**, each to a **different input**.
  Example: Windows PC → HDMI 1, Mac → HDMI 2. VKVM doesn't carry video; it only tells the
  monitor which input to show.
- **DDC/CI turned on** in the monitor's on-screen menu (usually under *System* or *Setup*).
- Both computers on the **same network** (same Wi-Fi or router).
- Windows 10/11 (64-bit) and/or macOS 11+ on Apple Silicon.

> Many USB-C/Thunderbolt adapters and docks block DDC/CI. If a computer can't control the
> monitor, a direct HDMI/DisplayPort cable usually fixes it. Only the main computer actually
> needs to control the monitor; the other one just has to be paired.

## Install

Download the latest release from the
[Releases page](https://github.com/shimentosh/switchpc/releases).

### Windows

1. Run **`VKVM-Setup.exe`**. If SmartScreen says *"Windows protected your PC"*, click
   **More info → Run anyway** (the build isn't code-signed yet).
2. Keep **"Open VKVM now"** checked and click **Finish**.

The installer adds VKVM to the Start menu and the desktop, and allows it through Windows
Firewall so the computers can find each other. VKVM itself runs as a normal app. It never
asks for administrator rights.

### macOS

1. Open **`VKVM.dmg`** and drag **VKVM** into **Applications**.
2. The build isn't notarized by Apple yet, so macOS blocks it on first launch. Allow it once,
   either way:
   - Open Terminal and run
     ```bash
     xattr -dr com.apple.quarantine /Applications/VKVM.app
     ```
     then open VKVM from Applications, **or**
   - Open VKVM, then go to **System Settings → Privacy & Security** and click **Open Anyway**.

VKVM lives in the menu bar. The setup window asks for the macOS permissions it needs.

## Set up (about two minutes)

Set up the **main computer first** (the one your keyboard and mouse are plugged into).

**On the main computer**
1. The VKVM window opens by itself. Enter a name and choose **Main computer**.
2. VKVM reads which input the monitors are showing right now (this computer's input).
   Check it and click **Continue**. Use **Test** if you're unsure.
3. A **6-digit pairing code** appears. Leave it on screen.

**On the second computer**
1. Open VKVM, enter a name and choose **Second computer**.
2. Pick the main computer from the list (or type its IP address), enter the code, and click
   **Pair**.

That's it. While pairing, the second computer reports which input *it* is on, so the main
computer learns both inputs without you typing any codes. Finish the setup on both computers.

### Use it

- **Hotkeys** work on both computers: `Ctrl+Alt+1` shows the main computer and `Ctrl+Alt+2`
  shows the second one. On a Mac, `Cmd` works in place of `Ctrl`.
- The **tray / menu bar icon** has *Switch to …* entries and **Open VKVM**.
- The **window** (Home tab) has a button for each computer.

Change hotkeys, monitor inputs and other settings in the window's **Monitors** and
**Settings** tabs. Profiles and hotkeys are managed on the main computer and synced to the
second one.

### Keyboard and mouse sharing (optional)

On the main computer go to **Settings → Share keyboard and mouse**. While the monitors show
the second computer, the main computer's keyboard and mouse control it. Emergency release:
`Ctrl+Alt+Shift+Esc`.

- On a **Mac** receiving the input, allow **Accessibility** when asked (Settings tab →
  *macOS permissions*).
- On **Windows**, capturing the keyboard and mouse needs VKVM to run as administrator. If
  sharing doesn't work, start VKVM with *Run as administrator*.

## Troubleshooting

| Problem | Fix |
|---|---|
| "No monitors with DDC/CI found" | Turn on DDC/CI in the monitor menu. Try a direct cable instead of an adapter or dock. |
| The second computer doesn't find the main one | Same network? Set up the main computer first. Type the main computer's IP (shown on its *Computers* tab). On Windows, reinstalling restores the firewall rule. |
| Switching picks the wrong input | **Monitors** tab → choose the right input for each computer → **Test**. |
| Hotkeys do nothing on the Mac | Settings tab → *macOS permissions* → allow **Accessibility**, then restart VKVM. With unsigned builds, macOS asks again after each update. |
| Nothing happens when starting VKVM | It may already be running in the tray/menu bar. Starting it again opens its window. |

Logs: `%LOCALAPPDATA%\vkvm\vkvm.log` (Windows) or `~/Library/Caches/vkvm/vkvm.log` (macOS).
The old all-options page is still available under **Settings → Advanced → Classic settings**.

## Build from source

Requires Go 1.25+.

```bash
# Windows: exe + installer (installer needs NSIS: choco install nsis)
powershell -ExecutionPolicy Bypass -File packaging/windows/build.ps1

# macOS: VKVM.app + VKVM.dmg in dist/
packaging/macos/build-dmg.sh

# Quick dev run (any OS; on Windows copy tools\ControlMyMonitor.exe into
# internal\embedded\tools\ first)
go run ./cmd
```

### Code signing (optional)

Builds are unsigned by default. The scripts are ready for signing:

- **Windows**: set `WINDOWS_CERT_PFX` (path to a .pfx) and `WINDOWS_CERT_PASSWORD` before
  running `build.ps1`. In GitHub Actions, add the repository secrets `WINDOWS_CERT_PFX_BASE64`
  and `WINDOWS_CERT_PASSWORD`.
- **macOS**: set `CODESIGN_IDENTITY` (a *Developer ID Application* identity) and optionally
  `NOTARY_PROFILE` (a `notarytool` keychain profile) before running `build-dmg.sh`.

### How it works

VKVM runs as a single per-user process with a tray icon. It serves its window UI on
`127.0.0.1:18079`; the window itself is a native web view (WebView2 on Windows, WebKit on
macOS) running as a small separate process. The main computer runs an authenticated API on
port 18080 (advertised over mDNS). Pairing exchanges device keys and the API token using a
single-use, short-lived 6-digit code. The second computer keeps a WebSocket to the main
computer for switching, profile sync and the optional input forwarding.

```
┌──────────────────────┐    LAN (pairing, sync,     ┌──────────────────────┐
│ Main computer (Host) │◄──── switching, input) ───►│ Second computer      │
│ tray + window + API  │                            │ (Agent) tray+window  │
└─────────┬────────────┘                            └──────────┬───────────┘
          │ DDC/CI                                              │ DDC/CI (optional)
          ▼                                                     ▼
                         ┌────────────────────────┐
                         │ Monitor  HDMI1 │ HDMI2 │
                         └────────────────────────┘
```

## Acknowledgements

VKVM relies on the following excellent open-source tools for DDC/CI communication:

| Platform | Tool | Author | License |
|----------|------|--------|--------|
| **macOS** | [m1ddc](https://github.com/waydabber/m1ddc) | @waydabber | MIT |
| **Windows** | [ControlMyMonitor](https://www.nirsoft.net/utils/control_my_monitor.html) | NirSoft | Freeware |

The native window uses [go-webview2](https://github.com/jchv/go-webview2) (Windows) and
[webview_go](https://github.com/webview/webview_go) (macOS).

The original VKVM is by [aluo96078](https://github.com/aluo96078/vkvm).

## License

MIT License. See [LICENSE](./LICENSE).
