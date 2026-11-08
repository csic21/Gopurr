#!/usr/bin/env bash
# Build an Android APK with the pinned NSH AAR and Go/JNI runtime.
# No Gradle or system-wide dependencies are installed by this script.
set -euo pipefail

project_dir="$(cd "$(dirname "$0")/.." && pwd)"
cd "$project_dir"
abi="${1:-arm64-v8a}"
mode="${2:-debug}"
requested_version="${3:-}"
case "$mode" in
  debug) version_code=1 ;;
  release)
    # Release builds never generate a key or fall back to development signing.
    : "${GOPURR_ANDROID_KEYSTORE:?Release requires an Android signing keystore}"
    : "${GOPURR_ANDROID_KEY_ALIAS:?Release requires a signing key alias}"
    : "${GOPURR_ANDROID_STORE_PASSWORD:?Release requires a keystore password}"
    : "${GOPURR_ANDROID_KEY_PASSWORD:?Release requires a signing key password}"
    : "${GOPURR_ANDROID_VERSION_CODE:?Release requires a monotonically increasing version code}"
    [[ -f "$GOPURR_ANDROID_KEYSTORE" ]] || { echo "Android signing keystore is missing" >&2; exit 1; }
    version_code="$GOPURR_ANDROID_VERSION_CODE"
    ;;
  *) echo "Usage: $0 [arm64-v8a|x86_64] [debug|release] [version]" >&2; exit 1 ;;
esac

case "$abi" in
  arm64-v8a) go_arch=arm64; clang_target=aarch64-linux-android28 ;;
  x86_64) go_arch=amd64; clang_target=x86_64-linux-android28 ;;
  *) echo "Usage: $0 [arm64-v8a|x86_64] [debug|release] [version]" >&2; exit 1 ;;
esac
build_dir="$project_dir/bin/native/android/$abi"
if [[ "$mode" == release ]]; then
  build_dir="$build_dir/release"
fi
metadata="$(python3 scripts/prepare-android-manifest.py --mode "$mode" \
  --version "$requested_version" --version-code "$version_code" --out "$build_dir/AndroidManifest.xml")"
read -r app_version version_code <<< "$metadata"
python3 native/ui/sync-assets.py
android_sdk="${ANDROID_SDK_ROOT:-${ANDROID_HOME:-$HOME/Library/Android/sdk}}"
build_tools="${ANDROID_BUILD_TOOLS:-$android_sdk/build-tools/36.1.0}"
android_jar="$android_sdk/platforms/android-36/android.jar"
ndk="${ANDROID_NDK_HOME:-$android_sdk/ndk/29.0.14206865}"
if [[ ! -d "$ndk" ]]; then
  ndk="$(find "$android_sdk/ndk" -mindepth 1 -maxdepth 1 -type d | sort | tail -1)"
fi
java_home="${GOPURR_JAVA_HOME:-${JAVA_HOME:-$(/usr/libexec/java_home -v 17)}}"
export JAVA_HOME="$java_home"
export PATH="$JAVA_HOME/bin:$PATH"
sdk_dir="$project_dir/.cache/nsh/nsh"
nsh="$sdk_dir/bin/nsh"
[[ -x "$nsh" ]] || "$project_dir/scripts/bootstrap-nsh.sh" >/dev/null

clang="$ndk/toolchains/llvm/prebuilt/darwin-x86_64/bin/$clang_target-clang"
for required in "$android_jar" "$build_tools/aapt2" "$build_tools/d8" "$build_tools/zipalign" "$build_tools/apksigner" "$clang"; do
  [[ -f "$required" ]] || { echo "Missing Android build dependency: $required" >&2; exit 1; }
done

export GOPATH="$project_dir/.cache/go"
export GOMODCACHE="$project_dir/.cache/mod"
export GOCACHE="$project_dir/.cache/build"
export GOTOOLCHAIN=go1.27.3
# Upstream's cmfa mode delegates Android UID/package routing to VpnService.
# The standalone Android mode tries to read root-only /data/system/packages.xml.
# Keep this flag specific to the native Android host, never desktop releases.
export GOFLAGS="${GOFLAGS:-} -tags=with_gvisor,cmfa"
export GOOS=android
export GOARCH="$go_arch"
export CGO_ENABLED=1
export CC="$clang"

mkdir -p "$build_dir" "$project_dir/.cache/nsh/android-aar"
rm -rf "$build_dir/classes" "$build_dir/dex" "$build_dir/apk" "$build_dir/generated" "$build_dir/res"
unzip -q -o "$sdk_dir/embed/android/nsh.aar" -d "$project_dir/.cache/nsh/android-aar"
aar_dir="$project_dir/.cache/nsh/android-aar"
# cmfa leaves Android's app/route management to our VpnService. Without it,
# upstream attempts to read the root-only /data/system/packages.xml file.
go build -trimpath -ldflags="-s -w" -tags=with_gvisor,cmfa -buildmode=c-shared -o "$build_dir/libgopurr.so" ./cmd/gopurr-bridge
"$clang" -fPIC -shared -Wl,-soname,libgopurrjni.so -Wl,-z,max-page-size=16384 \
  -I "$build_dir" native/android/jni/gopurr_jni.c -L "$build_dir" -lgopurr -llog \
  -o "$build_dir/libgopurrjni.so"

"$nsh" build native/ui/App.tsx --release --out "$build_dir/ui"
mkdir -p "$build_dir/res/drawable-nodpi" "$build_dir/generated" "$build_dir/classes" "$build_dir/dex"
cp internal/brand/icon-master.png "$build_dir/res/drawable-nodpi/app_icon.png"
"$build_tools/aapt2" compile --dir "$build_dir/res" -o "$build_dir/resources.zip"
"$build_tools/aapt2" link -o "$build_dir/base.apk" -I "$android_jar" \
  --manifest "$build_dir/AndroidManifest.xml" --java "$build_dir/generated" \
  --version-code "$version_code" --version-name "$app_version" "$build_dir/resources.zip"
find native/android/src "$build_dir/generated" -name '*.java' -print > "$build_dir/sources.txt"
javac -source 17 -target 17 -encoding UTF-8 -classpath "$android_jar:$aar_dir/classes.jar" \
  -d "$build_dir/classes" @"$build_dir/sources.txt"
jar cf "$build_dir/classes.jar" -C "$build_dir/classes" .
"$build_tools/d8" --min-api 28 --lib "$android_jar" --output "$build_dir/dex" \
  "$build_dir/classes.jar" "$aar_dir/classes.jar"

stage="$build_dir/apk"
mkdir -p "$stage/assets/nsh" "$stage/lib/$abi"
cp -R "$build_dir/ui/." "$stage/assets/nsh/"
cp LICENSE THIRD_PARTY_NOTICES.md "$stage/assets/"
cp -R licenses "$stage/assets/"
cp "$build_dir/dex/"*.dex "$stage/"
cp "$build_dir/libgopurr.so" "$build_dir/libgopurrjni.so" "$aar_dir/jni/$abi/libnsh.so" "$stage/lib/$abi/"
cp "$build_dir/base.apk" "$build_dir/unsigned.apk"
(cd "$stage" && zip -q -r "$build_dir/unsigned.apk" assets lib ./*.dex)
"$build_tools/zipalign" -P 16 -f 4 "$build_dir/unsigned.apk" "$build_dir/aligned.apk"

if [[ "$mode" == release ]]; then
  output_dir="$project_dir/dist/android-$abi"
  mkdir -p "$output_dir"
  output="$output_dir/gopurr-$app_version-android-$abi.apk"
  "$build_tools/apksigner" sign --ks "$GOPURR_ANDROID_KEYSTORE" \
    --ks-pass env:GOPURR_ANDROID_STORE_PASSWORD --ks-key-alias "$GOPURR_ANDROID_KEY_ALIAS" \
    --key-pass env:GOPURR_ANDROID_KEY_PASSWORD --v4-signing-enabled false \
    --out "$output" "$build_dir/aligned.apk"
else
  debug_key="$project_dir/.cache/nsh/android-debug.keystore"
  if [[ ! -f "$debug_key" ]]; then
    keytool -genkeypair -keystore "$debug_key" -storepass android -keypass android \
      -alias androiddebugkey -keyalg RSA -keysize 2048 -validity 10000 \
      -dname "CN=Gopurr Development,O=Gopurr,C=US" >/dev/null 2>&1
  fi
  output="$project_dir/bin/native/gopurr-native-$abi-debug.apk"
  "$build_tools/apksigner" sign --ks "$debug_key" --ks-pass pass:android \
    --ks-key-alias androiddebugkey --key-pass pass:android --out "$output" "$build_dir/aligned.apk"
fi
"$build_tools/apksigner" verify --verbose --print-certs "$output" | tee "$build_dir/signature.txt"
"$build_tools/zipalign" -c -P 16 4 "$output"
"$build_tools/aapt2" dump badging "$output" > "$build_dir/badging.txt"
python3 - "$build_dir/badging.txt" "$output" "$mode" "$app_version" "$version_code" "$abi" "$build_dir/signature.txt" <<'PY'
import base64
import hashlib
import struct
import sys
import zipfile
from pathlib import Path
badging_path, apk, mode, version, code, abi, signature_path = sys.argv[1:]
if mode == "release":
    certificate = Path("native/android/release-certificate.pem").read_text()
    der = base64.b64decode("".join(certificate.splitlines()[1:-1]), validate=True)
    digest = hashlib.sha256(der).hexdigest()
    actual = [line.split(": ", 1)[1] for line in Path(signature_path).read_text().splitlines()
              if "certificate SHA-256 digest: " in line]
    if actual != [digest]:
        raise SystemExit("APK signing certificate does not match native/android/release-certificate.pem")
badging = Path(badging_path).read_text()
expected = f"package: name='io.github.gopurr.nativeapp' versionCode='{code}' versionName='{version}'"
if expected not in badging or ("application-debuggable" in badging) != (mode == "debug"):
    raise SystemExit("APK version, application ID or debuggable flag does not match the build")
with zipfile.ZipFile(apk) as package:
    libraries = {name for name in package.namelist() if name.startswith("lib/") and name.endswith(".so")}
    expected_libraries = {f"lib/{abi}/{name}.so" for name in ("libgopurr", "libgopurrjni", "libnsh")}
    if libraries != expected_libraries:
        raise SystemExit("APK has missing or unexpected native libraries")
    for name in libraries:
        data = package.read(name)
        if data[:6] != b"\x7fELF\x02\x01":
            raise SystemExit(f"Expected a 64-bit little-endian ELF library: {name}")
        header = struct.unpack_from("<16sHHIQQQIHHHHHH", data)
        loads = [struct.unpack_from("<IIQQQQQQ", data, header[5] + header[9] * i)
                 for i in range(header[10])]
        loads = [segment for segment in loads if segment[0] == 1]
        if not loads or any(segment[7] < 16384 or (segment[3] - segment[2]) % 16384 for segment in loads):
            raise SystemExit(f"Native library is not compatible with 16 KB pages: {name}")
PY
printf 'Built %s APK: %s\n' "$mode" "$output"
