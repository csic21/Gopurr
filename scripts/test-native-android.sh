#!/usr/bin/env bash
# Builds separate instrumentation, then exercises the real Go/JNI VPN on a
# disposable Android emulator. Never starts an emulator or touches real devices.
set -euo pipefail

project_dir="$(cd "$(dirname "$0")/.." && pwd)"
cd "$project_dir"
serial="${1:-emulator-5580}"
if [[ ! "$serial" =~ ^emulator-[0-9]+$ ]]; then
  echo "Refusing native smoke test: only an explicit emulator-* serial is accepted." >&2
  exit 2
fi
android_sdk="${ANDROID_SDK_ROOT:-${ANDROID_HOME:-$HOME/Library/Android/sdk}}"
build_tools="${ANDROID_BUILD_TOOLS:-$android_sdk/build-tools/36.1.0}"
android_jar="$android_sdk/platforms/android-36/android.jar"
adb="$android_sdk/platform-tools/adb"
# Use a separate loopback server so unrelated IDE/wireless debugging sessions
# cannot tear down this instrumentation connection. Never auto-connect Wi-Fi.
export ADB_SERVER_SOCKET="tcp:localhost:${GOPURR_TEST_ADB_PORT:-5039}"
export ADB_MDNS_AUTO_CONNECT=0
java_home="${GOPURR_JAVA_HOME:-$(/usr/libexec/java_home -v 17)}"
export JAVA_HOME="$java_home"
export PATH="$JAVA_HOME/bin:$PATH"
debug_key="$project_dir/.cache/nsh/android-debug.keystore"
abi="${GOPURR_TEST_ANDROID_ABI:-arm64-v8a}"
case "$abi" in arm64-v8a|x86_64) ;; *) echo "Unsupported test ABI: $abi" >&2; exit 2 ;; esac
main_apk="$project_dir/bin/native/gopurr-native-$abi-debug.apk"
main_classes="$project_dir/bin/native/android/$abi/classes.jar"
for required in "$adb" "$android_jar" "$build_tools/aapt2" "$build_tools/d8" "$build_tools/apksigner" "$main_apk" "$main_classes" "$debug_key"; do
  [[ -f "$required" ]] || { echo "Missing test dependency: $required. Build the development Android APK first." >&2; exit 1; }
done
main_badging="$("$build_tools/aapt2" dump badging "$main_apk")"
if [[ "$main_badging" != "package: name='io.github.gopurr.nativeapp'"* ]]; then
  echo "Refusing native smoke test: APK is not the isolated Gopurr development package." >&2
  exit 2
fi

build_dir="$project_dir/bin/native/android/test"
rm -rf "$build_dir/classes" "$build_dir/dex" "$build_dir/generated"
mkdir -p "$build_dir/classes" "$build_dir/dex" "$build_dir/generated"
"$build_tools/aapt2" link -o "$build_dir/base.apk" -I "$android_jar" \
  --manifest native/android/test/AndroidManifest.xml --java "$build_dir/generated"
find native/android/test/src "$build_dir/generated" -name '*.java' -print > "$build_dir/sources.txt"
javac -source 17 -target 17 -encoding UTF-8 -classpath "$android_jar:$main_classes" \
  -d "$build_dir/classes" @"$build_dir/sources.txt"
jar cf "$build_dir/classes.jar" -C "$build_dir/classes" .
"$build_tools/d8" --min-api 28 --lib "$android_jar" --classpath "$main_classes" \
  --output "$build_dir/dex" "$build_dir/classes.jar"
cp "$build_dir/base.apk" "$build_dir/unsigned.apk"
(cd "$build_dir/dex" && zip -q "$build_dir/unsigned.apk" ./*.dex)
"$build_tools/zipalign" -P 16 -f 4 "$build_dir/unsigned.apk" "$build_dir/aligned.apk"
test_apk="$project_dir/bin/native/gopurr-native-instrumentation-debug.apk"
"$build_tools/apksigner" sign --ks "$debug_key" --ks-pass pass:android \
  --ks-key-alias androiddebugkey --key-pass pass:android --out "$test_apk" "$build_dir/aligned.apk"
"$build_tools/apksigner" verify "$test_apk"
if [[ "${GOPURR_TEST_BUILD_ONLY:-0}" == "1" ]]; then
  printf 'Built separate instrumentation APK: %s\n' "$test_apk"
  exit 0
fi
python3 native/android/test/run.py --adb "$adb" --serial "$serial" \
  --main-apk "$main_apk" --test-apk "$test_apk"
