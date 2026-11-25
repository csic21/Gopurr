#!/usr/bin/env bash
# Download the pinned NSH SDK into this checkout; never install globally.
set -euo pipefail

project_dir="$(cd "$(dirname "$0")/.." && pwd)"
version="0.7.0"
archive_name="nsh-macos-arm64.tar.gz"
expected_sha256="5b2e5385e09a65ed63b178a8dd9cef03a9dcab204d2efc942300e0c5baea3853"
embed_ios_sha256="71a952b23b8ac0d0a8a40176acd76149bd1a6e3007eca8bc8c4c66ca7f26124a"
embed_android_sha256="89d0c979d1a457b57118b32b5f8312e45c856377501816b37069ad9d36c77033"
cache_dir="$project_dir/.cache/nsh"
archive_path="$cache_dir/download/$archive_name"
cli_path="$cache_dir/nsh/bin/nsh"

if [[ "$(uname -s)" != Darwin || "$(uname -m)" != arm64 ]]; then
  echo "This bootstrap currently supports macOS Apple Silicon." >&2
  exit 1
fi

mkdir -p "$cache_dir/download"
fetch() {
  local asset="$1" out="$2" digest="$3"
  local path="$cache_dir/download/$out"
  if [[ -f "$path" ]] && [[ "$(shasum -a 256 "$path" | awk '{print $1}')" == "$digest" ]]; then
    return 0
  fi
  # The browser download redirect can return 504 even while the release API
  # serves the same immutable asset. Hosted jobs have a read-only GH_TOKEN;
  # keep curl available for local machines without an authenticated gh CLI.
  if command -v gh >/dev/null 2>&1 && \
     [[ -n "${GH_TOKEN:-${GITHUB_TOKEN:-}}" ]]; then
    GH_TOKEN="${GH_TOKEN:-${GITHUB_TOKEN:-}}" gh release download "v$version" --repo longbridge/nsh \
      --pattern "$asset" --output "$path.part" --clobber
  else
    curl --fail --location --retry 5 --retry-all-errors --connect-timeout 20 \
      "https://github.com/longbridge/nsh/releases/download/v$version/$asset" \
      --output "$path.part"
  fi
  mv "$path.part" "$path"
  if [[ "$(shasum -a 256 "$path" | awk '{print $1}')" != "$digest" ]]; then
    echo "NSH archive checksum mismatch: $asset" >&2
    exit 1
  fi
}

stamp="$cache_dir/verified-$version"
if [[ -x "$cli_path" ]] && [[ -f "$stamp" ]] &&
   [[ "$(cat "$stamp")" == "$expected_sha256 $embed_ios_sha256 $embed_android_sha256" ]] &&
   [[ -d "$cache_dir/nsh/embed/ios/NativeShell.xcframework" ]] &&
   [[ -f "$cache_dir/nsh/embed/android/nsh.aar" ]]; then
  printf '%s\n' "$cli_path"
  exit 0
fi

fetch "$archive_name" "$archive_name" "$expected_sha256"
fetch "nsh-embed-ios.zip" "nsh-embed-ios.zip" "$embed_ios_sha256"
fetch "nsh-embed-android.zip" "nsh-embed-android.zip" "$embed_android_sha256"

if [[ ! -f "$stamp" ]] || [[ "$(cat "$stamp")" != "$expected_sha256 $embed_ios_sha256 $embed_android_sha256" ]]; then
  rm -rf "$cache_dir/nsh"
  mkdir -p "$cache_dir/nsh"
  tar -xzf "$archive_path" -C "$cache_dir"
  mkdir -p "$cache_dir/nsh/embed/ios" "$cache_dir/nsh/embed/android"
  unzip -q -o "$cache_dir/download/nsh-embed-ios.zip" -d "$cache_dir/nsh/embed/ios-tmp"
  unzip -q -o "$cache_dir/download/nsh-embed-android.zip" -d "$cache_dir/nsh/embed/android-tmp"
  cp -R "$cache_dir/nsh/embed/ios-tmp/nsh-embed-ios/." "$cache_dir/nsh/embed/ios/"
  cp -R "$cache_dir/nsh/embed/android-tmp/nsh-embed-android/." "$cache_dir/nsh/embed/android/"
  rm -rf "$cache_dir/nsh/embed/ios-tmp" "$cache_dir/nsh/embed/android-tmp"
  printf '%s\n' "$expected_sha256 $embed_ios_sha256 $embed_android_sha256" > "$stamp"
fi
if [[ "$("$cli_path" --version)" != "nsh $version (3f8ecd8)" ]]; then
  echo "Unexpected NSH version in the downloaded SDK." >&2
  exit 1
fi
if [[ ! -d "$cache_dir/nsh/embed/ios/NativeShell.xcframework" ]] ||
   [[ ! -f "$cache_dir/nsh/embed/android/nsh.aar" ]]; then
  echo "The NSH embedding SDKs are incomplete." >&2
  exit 1
fi
printf '%s\n' "$cli_path"
