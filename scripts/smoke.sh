#!/usr/bin/env bash
# Isolated real-core traffic test. Never uses user profiles or the OS proxy.
set -euo pipefail
cd "$(dirname "$0")/.."
export GOPATH="$PWD/.cache/go" GOMODCACHE="$PWD/.cache/mod" GOCACHE="$PWD/.cache/build"
export GOTOOLCHAIN=go1.27.3 GOFLAGS="${GOFLAGS:--tags=with_gvisor}"
make build
exec go run ./scripts/smoke -binary bin/Gopurr
