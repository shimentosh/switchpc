#!/bin/sh
set -eu

# Reproducible local packaging helper. Release signing/notarization are
# supplied through CODESIGN_IDENTITY and NOTARY_PROFILE. Local builds receive
# an ad-hoc bundle signature so LaunchServices can validate the App bundle;
# this is not a trusted distribution signature.
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
OUT=${1:-"$ROOT/dist"}
PORTABLE=0
if [ "${2:-}" = "--portable" ]; then
  PORTABLE=1
fi
cd "$ROOT"
VERSION=$(tr -d '[:space:]' < "$ROOT/VERSION")
if [ -z "${GOCACHE:-}" ]; then
  GOCACHE="${TMPDIR:-/tmp}/vkvm-go-build-cache"
  export GOCACHE
fi
rm -rf "$OUT/stage"
mkdir -p "$OUT/stage/VKVM.app/Contents/MacOS" "$OUT/stage/VKVM.app/Contents/Resources"
CGO_ENABLED=1 go build -trimpath -o "$OUT/stage/VKVM.app/Contents/MacOS/vkvm" "$ROOT/cmd"
# App icon, drawn by internal/brand.
go run ./packaging/icon "$OUT/icon"
iconutil -c icns "$OUT/icon/AppIcon.iconset" -o "$OUT/stage/VKVM.app/Contents/Resources/AppIcon.icns"
cat > "$OUT/stage/VKVM.app/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleInfoDictionaryVersion</key><string>6.0</string>
<key>CFBundleIdentifier</key><string>com.vkvm.app</string>
<key>CFBundleName</key><string>VKVM</string>
<key>CFBundleDisplayName</key><string>VKVM</string>
<key>CFBundleIconFile</key><string>AppIcon</string>
<key>LSMinimumSystemVersion</key><string>11.0</string>
<key>NSHumanReadableCopyright</key><string>MIT License</string>
<key>CFBundleExecutable</key><string>vkvm</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleShortVersionString</key><string>$VERSION</string>
<key>CFBundleVersion</key><string>$VERSION</string>
<key>LSUIElement</key><true/>
<key>NSHighResolutionCapable</key><true/>
</dict></plist>
PLIST
if [ -n "${CODESIGN_IDENTITY:-}" ]; then
  # Developer ID signing (set CODESIGN_IDENTITY, e.g. "Developer ID
  # Application: Name (TEAMID)") keeps macOS permissions across updates.
  codesign --force --options runtime --timestamp --identifier com.vkvm.app --sign "$CODESIGN_IDENTITY" "$OUT/stage/VKVM.app"
else
  # Keep local .app bundles structurally signed. Gatekeeper still requires a
  # Developer ID signature for distribution, but an unsigned bundle is
  # rejected by LaunchServices before the user can grant TCC permissions.
  # Ad-hoc signatures change with every build, so macOS asks for the
  # Accessibility permission again after each update.
  codesign --force --deep --identifier com.vkvm.app --sign - "$OUT/stage/VKVM.app"
fi
if [ "$PORTABLE" -eq 1 ]; then
  tar -czf "$OUT/VKVM.app.tar.gz" -C "$OUT/stage" VKVM.app
  echo "created $OUT/VKVM.app.tar.gz"
  exit 0
fi
# Drag-to-install layout: VKVM.app next to a link to /Applications.
ln -sf /Applications "$OUT/stage/Applications"
hdiutil create -volname VKVM -srcfolder "$OUT/stage" -ov -format UDZO "$OUT/VKVM.dmg"
if [ -n "${NOTARY_PROFILE:-}" ]; then
  xcrun notarytool submit "$OUT/VKVM.dmg" --keychain-profile "$NOTARY_PROFILE" --wait
  xcrun stapler staple "$OUT/VKVM.dmg"
fi
echo "created $OUT/VKVM.dmg"
