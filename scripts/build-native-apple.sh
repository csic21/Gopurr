#!/usr/bin/env bash
# Build the shared NSH UI and Swift host with an in-process Go C archive.
set -euo pipefail

project_dir="$(cd "$(dirname "$0")/.." && pwd)"
cd "$project_dir"
python3 native/ui/sync-assets.py
target="${1:-catalyst}"
app_version="$(python3 -c 'import json; print(json.load(open("mygo.json"))["version"])')"
update_feed="${GOPURR_NATIVE_UPDATE_FEED:-}"
update_public_key=""
if [[ -n "$update_feed" ]]; then
  python3 - "$update_feed" <<'PY'
import sys
from urllib.parse import urlparse
url = urlparse(sys.argv[1])
if url.scheme != 'https' or not url.hostname or url.username or url.password or url.query or url.fragment or not url.path.endswith('/update-native-darwin-arm64.json') or any(c.isspace() for c in sys.argv[1]):
    raise SystemExit('Native releases require a separate HTTPS update-native-darwin-arm64.json feed')
PY
  update_public_key="$(python3 -c 'import json; print(json.load(open("mygo.json"))["updates"]["publicKey"])')"
fi
sdk_root="$project_dir/.cache/nsh/nsh"
nsh="$sdk_root/bin/nsh"
[[ -x "$nsh" ]] || "$project_dir/scripts/bootstrap-nsh.sh" >/dev/null

export GOPATH="$project_dir/.cache/go"
export GOMODCACHE="$project_dir/.cache/mod"
export GOCACHE="$project_dir/.cache/build"
export GOTOOLCHAIN=go1.27.3
export GOFLAGS="${GOFLAGS:--tags=with_gvisor}"
export CGO_ENABLED=1
export GOOS=ios
export GOARCH=arm64

case "$target" in
  catalyst)
    apple_sdk="macosx"
    triple="arm64-apple-ios15.0-macabi"
    framework_slice="ios-arm64-maccatalyst"
    app_path="$project_dir/bin/native/Gopurr Native.app"
    device_family="<integer>6</integer>"
    ;;
  ios-simulator)
    apple_sdk="iphonesimulator"
    triple="arm64-apple-ios15.0-simulator"
    framework_slice="ios-arm64-simulator"
    app_path="$project_dir/bin/native/ios-simulator/Gopurr Native.app"
    device_family="<integer>1</integer><integer>2</integer>"
    ;;
  *) echo "Usage: $0 [catalyst|ios-simulator]" >&2; exit 1 ;;
esac

build_dir="$project_dir/bin/native/apple/$target"
mkdir -p "$build_dir" "$project_dir/.cache/clang"
sdk_path="$(xcrun --sdk "$apple_sdk" --show-sdk-path)"
clang_path="$(xcrun --sdk "$apple_sdk" --find clang)"
cc_wrapper="$build_dir/clang-ios"
cat > "$cc_wrapper" <<EOF
#!/bin/sh
exec "$clang_path" -target "$triple" -isysroot "$sdk_path" "\$@"
EOF
chmod +x "$cc_wrapper"
export CC="$cc_wrapper"
export CGO_CFLAGS="-fmodules -fmodules-cache-path=$project_dir/.cache/clang"
export CGO_LDFLAGS="-target $triple -isysroot $sdk_path"

go build -buildmode=c-archive -o "$build_dir/libgopurr.a" ./cmd/gopurr-bridge
ui_entry="native/ui/App.tsx"
if [[ "$target" == catalyst ]]; then
  ui_entry="native/ui/Desktop.tsx"
  helper_ldflags="-X gopurr/internal/buildinfo.Version=$app_version"
  if [[ -n "$update_feed" ]]; then
    helper_ldflags+=" -X github.com/egoist/mygo.production=1"
    helper_ldflags+=" -X github.com/egoist/mygo.packageName=Gopurr"
    helper_ldflags+=" -X github.com/egoist/mygo.packageIdentifier=io.github.gopurr.native"
    helper_ldflags+=" -X github.com/egoist/mygo.packageURLSchemes=gopurr,clash"
    helper_ldflags+=" -X github.com/egoist/mygo.packageVersion=$app_version"
    helper_ldflags+=" -X github.com/egoist/mygo.packageUpdateFeed=$update_feed"
    helper_ldflags+=" -X github.com/egoist/mygo.packageUpdateKey=$update_public_key"
  fi
  # This executable also implements --core and the privileged service flags.
  # The NSH window never loads the legacy desktop UI.
  env GOOS=darwin CGO_LDFLAGS="-isysroot $sdk_path" CGO_CFLAGS="-isysroot $sdk_path" CC="$clang_path" \
    go build -ldflags "$helper_ldflags" \
    -o "$build_dir/gopurr-desktop" .
fi
"$nsh" build "$ui_entry" --release --out "$build_dir/ui"

if [[ "$target" == catalyst ]]; then
  executable_dir="$app_path/Contents/MacOS"
  resource_dir="$app_path/Contents/Resources"
  plist_path="$app_path/Contents/Info.plist"
else
  executable_dir="$app_path"
  resource_dir="$app_path"
  plist_path="$app_path/Info.plist"
fi
mkdir -p "$executable_dir" "$resource_dir/nsh"
cp -R "$build_dir/ui/." "$resource_dir/nsh/"
if [[ "$target" == catalyst ]]; then
  cp "$build_dir/gopurr-desktop" "$resource_dir/gopurr-desktop"
  codesign --force --sign - "$resource_dir/gopurr-desktop"
  python3 - "$resource_dir/native-build.json" "$app_version" "$update_feed" "$update_public_key" <<'PY'
import json, pathlib, sys
path, version, feed, key = sys.argv[1:]
pathlib.Path(path).write_text(json.dumps(dict(schema=1,ui='nsh-desktop',backend='desktop-v2',version=version,updateFeed=feed,updatePublicKey=key,updateEnabled=bool(feed)),indent=2)+'\n')
PY
fi
cp LICENSE THIRD_PARTY_NOTICES.md "$resource_dir/"
cp -R licenses "$resource_dir/"
if [[ "$target" == catalyst ]]; then
  iconset="$build_dir/AppIcon.iconset"
  mkdir -p "$iconset"
  for size in 16 32 128 256 512; do
    sips -z "$size" "$size" internal/brand/icon-master.png --out "$iconset/icon_${size}x${size}.png" >/dev/null
    retina_size=$((size * 2))
    sips -z "$retina_size" "$retina_size" internal/brand/icon-master.png --out "$iconset/icon_${size}x${size}@2x.png" >/dev/null
  done
  python3 - "$iconset" "$resource_dir/AppIcon.icns" <<'PY'
import pathlib
import struct
import sys

source = pathlib.Path(sys.argv[1])
entries = {
    b"icp4": "icon_16x16.png", b"icp5": "icon_32x32.png",
    b"icp6": "icon_32x32@2x.png", b"ic07": "icon_128x128.png",
    b"ic08": "icon_256x256.png", b"ic09": "icon_512x512.png",
    b"ic10": "icon_512x512@2x.png",
}
payload = b""
for kind, filename in entries.items():
    image = (source / filename).read_bytes()
    payload += kind + struct.pack(">I", len(image) + 8) + image
pathlib.Path(sys.argv[2]).write_bytes(b"icns" + struct.pack(">I", len(payload) + 8) + payload)
PY
fi
framework_dir="$sdk_root/embed/ios/NativeShell.xcframework/$framework_slice"
"$cc_wrapper" -fobjc-arc -fmodules -fmodules-cache-path="$project_dir/.cache/clang" \
  -c native/apple/DesktopWindow.m -o "$build_dir/DesktopWindow.o"
cat > "$build_dir/host.h" <<EOF
#include "libgopurr.h"
#include "$project_dir/native/apple/DesktopWindow.h"
EOF
swift_args=(
  -parse-as-library -swift-version 5 -O -target "$triple" -sdk "$sdk_path"
  -module-cache-path "$project_dir/.cache/clang"
  -F "$framework_dir" -framework NativeShell -framework UIKit
  -framework Foundation -framework CoreText -framework CoreGraphics
  -framework QuartzCore -framework Security -framework SystemConfiguration
  -framework Metal -framework CoreVideo -framework AVFoundation -lresolv
  -import-objc-header "$build_dir/host.h"
)
if [[ "$target" == catalyst ]]; then
  swift_args+=(
    -F "$sdk_path/System/iOSSupport/System/Library/Frameworks"
    -I "$sdk_path/System/iOSSupport/usr/include"
    -L "$sdk_path/System/iOSSupport/usr/lib"
  )
fi
xcrun swiftc "${swift_args[@]}" native/apple/EngineBridge.swift native/apple/DesktopBridge.swift native/apple/App.swift \
  "$build_dir/libgopurr.a" "$build_dir/DesktopWindow.o" -o "$executable_dir/Gopurr Native"

cat > "$plist_path" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>io.github.gopurr.native</string>
<key>CFBundleName</key><string>Gopurr Native</string>
<key>CFBundleDisplayName</key><string>Gopurr Native</string>
<key>CFBundleExecutable</key><string>Gopurr Native</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleURLTypes</key><array><dict><key>CFBundleURLSchemes</key><array><string>gopurr</string><string>clash</string></array></dict></array>
<key>CFBundleShortVersionString</key><string>$app_version</string>
<key>CFBundleVersion</key><string>1</string>
<key>CFBundleIconFile</key><string>AppIcon</string>
<key>LSMinimumSystemVersion</key><string>14.0</string>
<key>MinimumOSVersion</key><string>15.0</string>
<key>UIDeviceFamily</key><array>$device_family</array>
<key>UILaunchScreen</key><dict/>
<key>UIApplicationSceneManifest</key><dict>
  <key>UIApplicationSupportsMultipleScenes</key><false/>
  <key>UISceneConfigurations</key><dict>
    <key>UIWindowSceneSessionRoleApplication</key><array><dict>
      <key>UISceneConfigurationName</key><string>Gopurr</string>
      <key>UISceneDelegateClassName</key><string>GopurrSceneDelegate</string>
    </dict></array>
  </dict>
</dict>
<key>CADisableMinimumFrameDurationOnPhone</key><true/>
<key>NSHighResolutionCapable</key><true/>
<key>CFBundleDevelopmentRegion</key><string>en</string>
<key>CFBundleLocalizations</key><array><string>en</string><string>zh-Hans</string></array>
</dict></plist>
EOF
codesign --force --sign - "$app_path"
printf 'Built %s\n' "$app_path"
