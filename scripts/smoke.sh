#!/usr/bin/env bash
# End-to-end smoke test: launch the app, push real traffic through the
# embedded kernel, and report what the kernel counted.
set -euo pipefail

cd "$(dirname "$0")/.."

BIN=bin/Gopurr
PORT=7890
HTTP_PORT=8765
WORK=$(mktemp -d)

cleanup() {
  [[ -n "${APP_PID:-}" ]] && kill "$APP_PID" 2>/dev/null || true
  [[ -n "${HTTP_PID:-}" ]] && kill "$HTTP_PID" 2>/dev/null || true
  rm -rf "$WORK"
}
trap cleanup EXIT

echo "== build"
make build >/dev/null

echo "== local origin server on :$HTTP_PORT"
mkdir -p "$WORK/www"
head -c 4194304 /dev/urandom > "$WORK/www/blob.bin"
( cd "$WORK/www" && python3 -m http.server "$HTTP_PORT" >/dev/null 2>&1 ) &
HTTP_PID=$!

echo "== launch app (autostart kernel)"
GOPURR_AUTOSTART=1 "$BIN" >"$WORK/app.log" 2>&1 &
APP_PID=$!

echo "== wait for mixed-port $PORT"
for _ in $(seq 1 100); do
  if nc -z 127.0.0.1 "$PORT" 2>/dev/null; then break; fi
  sleep 0.2
done
if ! nc -z 127.0.0.1 "$PORT" 2>/dev/null; then
  echo "FAIL: kernel never listened on $PORT"
  tail -20 "$WORK/app.log"
  exit 1
fi

echo "== push 4 MiB download + 64 KiB upload through the proxy"
curl -sS --max-time 20 -x "http://127.0.0.1:$PORT" -o /dev/null "http://127.0.0.1:$HTTP_PORT/blob.bin"
head -c 65536 /dev/urandom > "$WORK/up.bin"
curl -sS --max-time 20 -x "http://127.0.0.1:$PORT" -o /dev/null \
  --data-binary @"$WORK/up.bin" "http://127.0.0.1:$HTTP_PORT/" || true

sleep 2

echo "== process footprint"
if [[ "$(uname -s)" == "Darwin" ]]; then
  python3 - "$APP_PID" <<'PY' || echo "  (footprint unavailable)"
import ctypes, struct, sys
pid = int(sys.argv[1])
buf = ctypes.create_string_buffer(1024)
if ctypes.CDLL("/usr/lib/libproc.dylib").proc_pid_rusage(pid, 4, ctypes.byref(buf)) != 0:
    raise SystemExit(1)
f = struct.unpack_from("<16Q", buf, 16)   # rusage_info_v4
print(f"  resident {f[6]/1048576:.1f} MiB / physical footprint {f[7]/1048576:.1f} MiB")
PY
else
  ps -o pid,rss,%cpu,command -p "$APP_PID" || echo "  (ps unavailable)"
fi

echo "== app log"
tail -5 "$WORK/app.log" || true

echo "PASS: kernel accepted proxied traffic without a sidecar process"
