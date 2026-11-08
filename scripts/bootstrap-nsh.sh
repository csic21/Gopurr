#!/usr/bin/env bash
# Download the pinned NSH SDK into this checkout; never install globally.
set -euo pipefail

project_dir="$(cd "$(dirname "$0")/.." && pwd)"
version="0.4.0"
archive_name="nsh-macos-arm64.tar.gz"
expected_sha256="f63c3d57c31b28c9b34227aac81f099d1e0e61f1db552a6ec5127d114eaf59cb"
cache_dir="$project_dir/.cache/nsh"
archive_path="$cache_dir/download/$archive_name"
cli_path="$cache_dir/nsh/bin/nsh"

if [[ "$(uname -s)" != Darwin || "$(uname -m)" != arm64 ]]; then
  echo "This bootstrap currently supports macOS Apple Silicon." >&2
  exit 1
fi

if [[ -x "$cli_path" && -f "$cache_dir/verified-$version" ]] &&
   [[ "$(cat "$cache_dir/verified-$version")" == "$expected_sha256" ]]; then
  printf '%s\n' "$cli_path"
  exit 0
fi

mkdir -p "$cache_dir/download"
if [[ ! -f "$archive_path" ]]; then
  # The browser download redirect can return 504 even while the release API
  # serves the same immutable asset. Hosted jobs have a read-only GH_TOKEN;
  # keep curl available for local machines without an authenticated gh CLI.
  if command -v gh >/dev/null 2>&1 && \
     [[ -n "${GH_TOKEN:-${GITHUB_TOKEN:-}}" ]]; then
    gh release download "v$version" --repo longbridge/nsh \
      --pattern "$archive_name" --output "$archive_path.part" --clobber
  else
    curl --fail --location --retry 2 --retry-all-errors --connect-timeout 20 \
      "https://github.com/longbridge/nsh/releases/download/v$version/$archive_name" \
      --output "$archive_path.part"
  fi
  mv "$archive_path.part" "$archive_path"
fi

actual_sha256="$(shasum -a 256 "$archive_path" | awk '{print $1}')"
if [[ "$actual_sha256" != "$expected_sha256" ]]; then
  echo "NSH archive checksum mismatch. Remove $archive_path and retry." >&2
  exit 1
fi

tar -xzf "$archive_path" -C "$cache_dir"
if [[ "$("$cli_path" --version)" != "nsh $version (e626a8d)" ]]; then
  echo "Unexpected NSH version in the downloaded SDK." >&2
  exit 1
fi
printf '%s\n' "$expected_sha256" > "$cache_dir/verified-$version"
printf '%s\n' "$cli_path"
