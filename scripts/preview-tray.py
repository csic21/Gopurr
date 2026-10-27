#!/usr/bin/env python3
"""Run a separate macOS tray preview with a fake core and fake OS proxy tools.

Build first with `make build`, then run `python3 scripts/preview-tray.py`.
The preview never loads real profiles, installs a service or changes networking.
Use --no-service to review the disabled TUN state, --light to test window release.
"""

import argparse
import http.server
import json
import os
from pathlib import Path
import plistlib
import shutil
import socketserver
import subprocess
import tempfile
import threading
import urllib.parse


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--no-service", action="store_true")
    parser.add_argument("--light", action="store_true")
    parser.add_argument("--duration", type=int, default=900)
    args = parser.parse_args()
    repo = Path(__file__).resolve().parent.parent
    bundle = repo / ".cache/tray-preview/Gopurr Tray Preview.app"
    executable = bundle / "Contents/MacOS/Gopurr"
    executable.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(repo / "bin/Gopurr", executable)
    with (bundle / "Contents/Info.plist").open("wb") as file:
        plistlib.dump({
            "CFBundleName": "Gopurr Tray Preview",
            "CFBundleDisplayName": "Gopurr Tray Preview",
            "CFBundleIdentifier": "io.github.gopurr.tray-preview",
            "CFBundleExecutable": "Gopurr",
            "CFBundlePackageType": "APPL",
            "NSHighResolutionCapable": True,
        }, file)

    with tempfile.TemporaryDirectory(prefix="gopurr-tray-preview-", dir="/tmp") as temporary:
        home = Path(temporary)
        fakebin = home / "bin"
        fakebin.mkdir()
        (home / "core.secret").write_text("local-tray-preview-controller-only")
        if not args.no_service:
            (home / "core.json").write_text('{"uid":0}')
        (home / "proxy-state").write_text("Enabled: No\n")
        (home / "prefs.json").write_text(json.dumps({
            "appearance": "dark", "lightMode": args.light, "lightDelaySeconds": 3,
        }))
        scripts = {
            "route": "#!/bin/sh\nprintf 'interface: en0\\n'\n",
            "networksetup": """#!/bin/sh
case "$1" in
-listnetworkserviceorder) printf '(1) Wi-Fi\\n(Hardware Port: Wi-Fi, Device: en0)\\n';;
-getwebproxy) /bin/cat "$GOPURR_HOME/proxy-state";;
esac
""",
            "osascript": """#!/bin/sh
printf '%s\\n' "$*" >> "$GOPURR_HOME/proxy-calls"
case "$*" in
*-setwebproxystate*) printf 'Enabled: No\\n' > "$GOPURR_HOME/proxy-state";;
*-setwebproxy*) printf 'Enabled: Yes\\nServer: 127.0.0.1\\nPort: 7890\\n' > "$GOPURR_HOME/proxy-state";;
esac
""",
        }
        for name, script in scripts.items():
            target = fakebin / name
            target.write_text(script)
            target.chmod(0o700)

        long_node = "德国 05 · " + "高速低延迟专线节点" * 7
        nodes = ["香港 01", "日本 02", "新加坡 03", "美国 04", long_node, "DIRECT"]
        state = {"mode": "rule", "selected": {"节点选择": nodes[0], "GLOBAL": "节点选择"}}
        lock = threading.Lock()

        def save_state():
            (home / "controller.json").write_text(json.dumps(state, ensure_ascii=False, indent=2))

        class Handler(http.server.BaseHTTPRequestHandler):
            def log_message(self, *unused):
                pass

            def respond(self, payload):
                body = json.dumps(payload, ensure_ascii=False).encode()
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

            def do_GET(self):
                with lock:
                    if self.path == "/version":
                        return self.respond({"version": "tray-preview"})
                    if self.path == "/configs":
                        return self.respond({"mode": state["mode"], "mixed-port": 7890})
                    if self.path == "/proxies":
                        proxies = {node: {"type": "Shadowsocks"} for node in nodes}
                        proxies.update({
                            "节点选择": {"type": "Selector", "now": state["selected"]["节点选择"], "all": ["自动选择"] + nodes},
                            "自动选择": {"type": "URLTest", "now": "日本 02", "all": nodes[:3]},
                            "GLOBAL": {"type": "Selector", "now": state["selected"]["GLOBAL"], "all": ["节点选择"] + nodes},
                        })
                        return self.respond({"proxies": proxies})
                    if self.path == "/connections":
                        return self.respond({"connections": []})
                    if self.path == "/rules":
                        return self.respond({"rules": [{"type": "Match", "payload": "", "proxy": "节点选择"}]})
                    return self.respond({})

            def do_PUT(self):
                request = json.loads(self.rfile.read(int(self.headers.get("Content-Length", 0))))
                with lock:
                    if self.path.startswith("/proxies/"):
                        group = urllib.parse.unquote(self.path.removeprefix("/proxies/"))
                        state["selected"][group] = request["name"]
                    elif self.path.startswith("/configs"):
                        state["payload"] = request["payload"]
                    save_state()
                self.send_response(204)
                self.end_headers()

            def do_PATCH(self):
                request = json.loads(self.rfile.read(int(self.headers.get("Content-Length", 0))))
                with lock:
                    state["mode"] = request["mode"]
                    save_state()
                self.send_response(204)
                self.end_headers()

        class Server(socketserver.ThreadingMixIn, socketserver.UnixStreamServer):
            daemon_threads = True

        save_state()
        with Server(str(home / "core.sock"), Handler) as server:
            threading.Thread(target=server.serve_forever, daemon=True).start()
            env = dict(os.environ, GOPURR_HOME=str(home), GOPURR_AUTOSTART="0", PATH=str(fakebin))
            with (home / "app.log").open("w") as log:
                process = subprocess.Popen([str(executable)], env=env, stdout=log, stderr=log)
                print(f"Preview app: {bundle}\nFixture home: {home}\nPID: {process.pid}", flush=True)
                try:
                    process.wait(timeout=args.duration)
                except (KeyboardInterrupt, subprocess.TimeoutExpired):
                    pass
                finally:
                    process.terminate()
                    process.wait()
                    server.shutdown()


if __name__ == "__main__":
    main()
