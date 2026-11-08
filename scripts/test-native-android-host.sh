#!/usr/bin/env bash
set -euo pipefail
project_dir="$(cd "$(dirname "$0")/.." && pwd)"
cd "$project_dir"
output="$project_dir/.cache/nsh/android-host-tests"
mkdir -p "$output"
javac -source 17 -target 17 -d "$output" \
  native/android/src/io/github/gopurr/nativeapp/SessionMachine.java \
  native/android/src/io/github/gopurr/nativeapp/TunnelOwnership.java \
  native/android/test/host/SessionMachineTest.java
java -ea -cp "$output" io.github.gopurr.nativeapp.SessionMachineTest
