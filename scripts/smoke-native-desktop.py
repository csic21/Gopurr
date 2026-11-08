#!/usr/bin/env python3
"""Exercise the shipped NSH desktop backend and real core in an isolated home.

All requests use this process's private authenticated Unix socket. The fixture
never installs a service, toggles system proxy/TUN, opens installed profiles, or
uses an external network destination. The macOS backend needs a GUI session.
"""

import argparse
import contextlib
import http.server
import json
import os
from pathlib import Path
import secrets
import shutil
import socket
import subprocess
import tempfile
import threading
import time

PAGES = ("overview", "subs", "proxies", "conns", "mesh", "rules", "logs", "settings")
SOURCE = """mode: rule
proxies: [{name: Loopback placeholder, type: http, server: 127.0.0.1, port: 9}]
proxy-groups:
  - name: Native Smoke Route
    type: select
    proxies: [DIRECT, REJECT]
rules: ["MATCH,Native Smoke Route"]
"""


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--bundle", type=Path, default=Path(__file__).resolve().parents[1] / "bin/native/Gopurr Native.app")
    parser.add_argument("--keep-home", action="store_true", help="retain this run's isolated directory for inspection")
    args = parser.parse_args()
    bundle = args.bundle.resolve()
    backend = bundle / "Contents/Resources/gopurr-desktop"
    frontend = bundle / "Contents/MacOS/GopurrNative"
    if not frontend.is_file():
        # The bundle's executable is authoritative if its name changes.
        import plistlib
        with (bundle / "Contents/Info.plist").open("rb") as source:
            frontend = bundle / "Contents/MacOS" / plistlib.load(source)["CFBundleExecutable"]
    assert backend.is_file() and frontend.is_file(), "Build the native desktop bundle first"
    directory = Path(tempfile.mkdtemp(prefix="gp-native-", dir="/tmp"))
    directory.chmod(0o700)
    home = directory / "home"
    home.mkdir(mode=0o700)
    # Seed a loopback-only local document before startup, so the legacy demo's
    # automatic latency group cannot contact its public URL during this test.
    (home / "local.yaml").write_text(SOURCE)
    (home / "local.yaml").chmod(0o600)
    (home / "prefs.json").write_text(json.dumps({"checkAppUpdates": False, "systemProxyStartup": "off", "restoreSystemProxy": False, "autoUpdateHours": 0}))
    (home / "prefs.json").chmod(0o600)
    socket_path = directory / "native.sock"
    token_path = directory / "session"
    token = secrets.token_hex(32)
    token_path.write_text(token)
    token_path.chmod(0o600)
    log_path = directory / "backend.log"
    env = {key: value for key, value in os.environ.items() if not key.startswith("GOPURR_")}
    env.update(GOPURR_HOME=str(home), GOPURR_AUTOSTART="1", GOPURR_TUN="0")
    process = None
    server = None
    ready = False
    summary = {"pages": [], "realCore": False, "loopbackTraffic": False,
               "invalidConfigRollback": False, "backupValidated": False,
               "osNetworkChangesRequested": False, "cleanShutdown": False}

    def request(method, params=None, expected_error=False, timeout=35):
        # An allowlist makes accidental privileged/system-network mutations in
        # future smoke-test edits fail before a request reaches the app.
        allowed = {"host.ping", "host.shutdown", "desktop.snapshot", "desktop.profiles.import",
                   "desktop.profiles.get", "desktop.profiles.activate", "desktop.profiles.aggregate",
                   "desktop.group.select", "desktop.rules.save", "desktop.library.save",
                   "desktop.backup.export", "desktop.backup.inspect", "desktop.prefs.save"}
        assert method in allowed, f"Smoke test does not permit {method}"
        with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as connection:
            connection.settimeout(timeout)
            connection.connect(str(socket_path))
            connection.sendall(json.dumps({"token": token, "method": method, "params": params or {}}).encode() + b"\n")
            response = b""
            while True:
                part = connection.recv(65536)
                if not part:
                    break
                response += part
                assert len(response) <= 32 << 20, "Oversized desktop response"
        reply = json.loads(response)
        if expected_error:
            assert reply.get("error"), f"{method} unexpectedly accepted invalid input"
            return reply["error"]
        if reply.get("error"):
            raise RuntimeError(f"{method}: {reply['error'].get('message', 'request failed')}")
        return reply.get("result")

    def eventually(check, timeout=35):
        deadline = time.monotonic() + timeout
        last_error = None
        while time.monotonic() < deadline:
            if process.poll() is not None:
                raise RuntimeError(f"Desktop backend exited with {process.returncode}")
            try:
                value = check()
                if value:
                    return value
            except (OSError, ValueError, RuntimeError) as error:
                last_error = error
            time.sleep(0.1)
        raise RuntimeError(f"Desktop readiness condition timed out: {last_error}")

    def snapshot(page="overview", **params):
        value = request("desktop.snapshot", {"page": page, **params})
        assert value["apiVersion"] == 2 and value["desktop"], "Wrong desktop protocol"
        assert not value["status"]["systemProxy"] and not value["status"]["tun"], "Fixture must never own OS network settings"
        return value

    try:
        with log_path.open("wb") as log:
            process = subprocess.Popen([str(backend), "--native-backend", "--home", str(home),
                                        "--socket", str(socket_path), "--token-file", str(token_path),
                                        "--frontend", str(frontend), "--parent", str(os.getpid())],
                                       env=env, stdout=log, stderr=subprocess.STDOUT)
        eventually(lambda: socket_path.is_socket() and request("host.ping")["ready"])
        ready = True
        eventually(lambda: snapshot()["status"]["kernel"] and not snapshot()["status"]["busy"])
        summary["realCore"] = True
        initial = snapshot()
        assert initial["profiles"] == [], "Fixture opened an installed profile store"
        port = int(initial["network"]["mixedPort"])
        assert 1 <= port <= 65535 and not initial["network"]["tunEnable"]
        first = request("desktop.profiles.import", {"name": "Desktop smoke source", "content": SOURCE})
        request("desktop.profiles.activate", {"id": first["id"]})
        eventually(lambda: any(group["name"] == "Native Smoke Route" for group in snapshot("proxies")["groups"]))
        request("desktop.group.select", {"group": "Native Smoke Route", "member": "REJECT"})
        eventually(lambda: any(group["name"] == "Native Smoke Route" and group["now"] == "REJECT" for group in snapshot("proxies")["groups"]))
        request("desktop.group.select", {"group": "Native Smoke Route", "member": "DIRECT"})
        eventually(lambda: any(group["name"] == "Native Smoke Route" and group["now"] == "DIRECT" for group in snapshot("proxies")["groups"]))

        # Real SOCKS5 traffic proves the NSH process is controlling the same
        # core that owns the isolated mixed listener, beyond snapshot rendering.
        payload = b"NSH desktop backend: verified isolated loopback traffic\n"
        class Handler(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                self.send_response(200)
                self.send_header("Content-Length", str(len(payload)))
                self.end_headers()
                self.wfile.write(payload)
            def log_message(self, *_args):
                pass
        server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        def receive_exact(connection, length):
            result = b""
            while len(result) < length:
                part = connection.recv(length - len(result))
                assert part, "SOCKS listener closed early"
                result += part
            return result
        with socket.create_connection(("127.0.0.1", port), timeout=8) as connection:
            connection.sendall(b"\x05\x01\x00")
            assert receive_exact(connection, 2) == b"\x05\x00"
            target_port = server.server_address[1]
            connection.sendall(b"\x05\x01\x00\x01\x7f\x00\x00\x01" + target_port.to_bytes(2, "big"))
            reply = receive_exact(connection, 4)
            assert reply[:2] == b"\x05\x00", "SOCKS connection refused"
            address_length = {1: 4, 4: 16}.get(reply[3])
            if reply[3] == 3:
                address_length = receive_exact(connection, 1)[0]
            assert address_length is not None
            receive_exact(connection, address_length + 2)
            connection.sendall(b"GET / HTTP/1.0\r\nHost: 127.0.0.1\r\n\r\n")
            response = b""
            while part := connection.recv(65536):
                response += part
            assert payload in response, "Traffic did not reach the fixture server"
        summary["loopbackTraffic"] = True

        before = request("desktop.profiles.get", {"id": first["id"]})
        request("desktop.profiles.import", {"name": "Invalid YAML", "content": "proxies: ["}, expected_error=True)
        assert len(snapshot("subs")["profiles"]) == 1
        assert request("desktop.profiles.get", {"id": first["id"]})["content"] == before["content"]
        request("desktop.rules.save", {"extraRules": ["DOMAIN,smoke.invalid,DIRECT"]})
        request("desktop.rules.save", {"id": first["id"], "mergeYaml": "profile: {store-selected: true}\n"})
        before_rules = snapshot("rules", scopeId=first["id"])
        request("desktop.rules.save", {"id": first["id"], "mergeYaml": "proxies: broken\n"}, expected_error=True)
        after_rules = snapshot("rules", scopeId=first["id"])
        assert before_rules["scope"] == after_rules["scope"], "Invalid override changed the accepted scope"
        assert after_rules["network"]["extraRules"] == ["DOMAIN,smoke.invalid,DIRECT"]
        assert after_rules["status"]["kernel"], "Invalid override stopped the accepted core"
        summary["invalidConfigRollback"] = True
        request("desktop.library.save", {"library": [{"id": "native-smoke", "name": "Smoke template", "yaml": "profile: {store-selected: true}\n"}]})
        request("desktop.rules.save", {"id": first["id"], "templateIds": ["native-smoke"]})
        second = request("desktop.profiles.import", {"name": "Second source", "content": SOURCE.replace("Native Smoke Route", "Second Smoke Route")})
        aggregate = request("desktop.profiles.aggregate", {"name": "Smoke combined", "spec": {"sources": [{"id": first["id"], "prefix": "One "}, {"id": second["id"], "prefix": "Two "}], "dedupe": True}})
        assert aggregate["profile"]["kind"] == "aggregate"
        request("desktop.prefs.save", {"appearance": "dark"})
        assert snapshot("settings")["prefs"]["appearance"] == "dark"
        backup_path = directory / "desktop-smoke.zip"
        request("desktop.backup.export", {"path": str(backup_path)})
        preview = request("desktop.backup.inspect", {"path": str(backup_path)})
        assert preview["count"] == 3 and backup_path.stat().st_mode & 0o777 == 0o600
        summary["backupValidated"] = True
        required = {"overview": "history", "subs": "profiles", "proxies": "groups", "conns": "connections",
                    "mesh": "mesh", "rules": "effectiveRules", "logs": "logs", "settings": "prefs"}
        for page in PAGES:
            assert required[page] in snapshot(page), f"Missing {page} model"
            summary["pages"].append(page)
        assert not (home / "sysproxy-recovery.json").exists(), "Fixture acquired a system proxy lease"
        request("host.shutdown", timeout=45)
        process.wait(timeout=15)
        assert process.returncode == 0, f"Backend shutdown failed: {process.returncode}"
        with socket.socket() as probe:
            probe.settimeout(1)
            assert probe.connect_ex(("127.0.0.1", port)) != 0, "Owned core listener survived shutdown"
        summary["cleanShutdown"] = True
        print(json.dumps(summary, ensure_ascii=False))
    except BaseException:
        if log_path.exists():
            print("Desktop fixture log tail:")
            print("\n".join(log_path.read_text(errors="replace").splitlines()[-35:]))
        raise
    finally:
        if server:
            server.shutdown()
            server.server_close()
        if process and process.poll() is None:
            if ready:
                with contextlib.suppress(Exception):
                    request("host.shutdown", timeout=30)
            if process.poll() is None:
                process.terminate()
            try:
                process.wait(timeout=15)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=5)
        if args.keep_home:
            print(f"Isolated smoke home: {directory}")
        else:
            shutil.rmtree(directory)


if __name__ == "__main__":
    main()
