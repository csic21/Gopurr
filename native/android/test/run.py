#!/usr/bin/env python3
"""Real Android TUN test runner; mutations are restricted to an emulator UID."""

import argparse
import datetime
import hashlib
import http.server
import json
import pathlib
import re
import subprocess
import threading
import time


TARGET = "io.github.gopurr.nativeapp"
TEST = TARGET + ".test"
MARKER = b"GOPURR_ANDROID_NATIVE_SMOKE"


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--adb", required=True)
    parser.add_argument("--serial", default="emulator-5580")
    parser.add_argument("--main-apk", required=True, type=pathlib.Path)
    parser.add_argument("--test-apk", required=True, type=pathlib.Path)
    args = parser.parse_args()
    if not re.fullmatch(r"emulator-[0-9]+", args.serial):
        raise SystemExit("Refusing to run native VPN tests on a real device")

    def adb(*command, timeout=30, check=True):
        result = subprocess.run([args.adb, "-s", args.serial, *command], text=True,
                                stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                                timeout=timeout, check=False)
        if check and result.returncode:
            raise RuntimeError(f"adb {' '.join(command)} failed:\n{result.stdout}")
        return result.stdout.strip()

    if adb("get-state") != "device":
        raise SystemExit("The selected emulator is unavailable")
    if adb("shell", "getprop", "ro.kernel.qemu") != "1":
        raise SystemExit("Refusing tests: selected target does not identify as an emulator")
    if adb("shell", "getprop", "sys.boot_completed") != "1":
        raise SystemExit("Wait for the selected emulator to finish booting")

    requests = []
    def digest(path):
        with path.open("rb") as stream:
            return hashlib.file_digest(stream, "sha256").hexdigest()

    report = {"serial": args.serial, "package": TARGET, "success": False,
              "startedAt": datetime.datetime.now(datetime.timezone.utc).isoformat(),
              "mainApkSHA256": digest(args.main_apk), "testApkSHA256": digest(args.test_apk)}
    report_path = args.test_apk.parent / "android" / "test" / "last-result.json"

    class Fixture(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            requests.append(self.path)
            if self.path == "/native-smoke/slow-profile.yaml":
                time.sleep(2)
                content = b"proxies: []\nrules: [MATCH,DIRECT]\n"
            else:
                content = MARKER
            self.send_response(200)
            self.send_header("Content-Type", "text/plain")
            self.send_header("Content-Length", str(len(content)))
            self.end_headers()
            try:
                self.wfile.write(content)
            except (BrokenPipeError, ConnectionResetError):
                pass

        def log_message(self, *unused):
            pass

    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Fixture)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        print(f"Testing development package {TARGET} on {args.serial} only", flush=True)
        print(adb("install", "--no-incremental", "-r", str(args.main_apk), timeout=120), flush=True)
        print(adb("install", "--no-incremental", "-r", str(args.test_apk), timeout=60), flush=True)
        adb("shell", "am", "force-stop", TARGET)
        if "Success" not in adb("shell", "pm", "clear", TARGET):
            raise RuntimeError("Could not isolate the emulator development application's data")
        adb("shell", "appops", "set", TARGET, "ACTIVATE_VPN", "allow")
        adb("shell", "pm", "grant", TARGET, "android.permission.POST_NOTIFICATIONS", check=False)
        output = adb("shell", "am", "instrument", "-w", "-r", "-e", "hostPort",
                     str(server.server_port), "-e", "capture", "true", TEST + "/.NativeSmoke", timeout=240)
        report["instrumentation"] = output
        print("\n".join(line if not line.startswith("INSTRUMENTATION_RESULT: engineLogs=")
                        else "INSTRUMENTATION_RESULT: engineLogs saved in last-result.json"
                        for line in output.splitlines()), flush=True)
        if "INSTRUMENTATION_RESULT: nativeSmoke=PASS:" not in output or "INSTRUMENTATION_CODE: -1" not in output:
            for state in ("live", "disconnected"):
                copied = subprocess.run([args.adb, "-s", args.serial, "exec-out", "run-as", TARGET,
                                         "cat", f"cache/native-smoke-overview-{state}.png"],
                                        stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=15)
                if copied.stdout.startswith(b"\x89PNG\r\n\x1a\n"):
                    diagnostic_path = report_path.parent / f"diagnostic-overview-{state}.png"
                    diagnostic_path.write_bytes(copied.stdout)
                    print(f"Saved diagnostic capture: {diagnostic_path}", flush=True)
            diagnostic = adb("logcat", "-d", "-v", "brief", "-t", "150", check=False)
            lines = [line for line in diagnostic.splitlines() if any(word in line for word in ("gopurr", "Gopurr", "AndroidRuntime", "Mihomo", "FATAL"))]
            print("\n".join(lines), flush=True)
            raise RuntimeError("Android native VPN instrumentation did not pass")
        # Keep files and permissions, kill the actual application process, then
        # exercise persisted preferences through JNI and a new real system TUN.
        adb("shell", "am", "force-stop", TARGET)
        restart_output = adb("shell", "am", "instrument", "-w", "-r", "-e", "hostPort",
                             str(server.server_port), "-e", "restart", "true", TEST + "/.NativeSmoke", timeout=120)
        report["restartInstrumentation"] = restart_output
        print(restart_output, flush=True)
        if "INSTRUMENTATION_RESULT: restartSmoke=PASS:" not in restart_output or "INSTRUMENTATION_CODE: -1" not in restart_output:
            raise RuntimeError("Android process-restart persistence verification did not pass")
        first_pid = re.search(r"INSTRUMENTATION_RESULT: processId=(\d+)", output)
        second_pid = re.search(r"INSTRUMENTATION_RESULT: processId=(\d+)", restart_output)
        if not first_pid or not second_pid or first_pid.group(1) == second_pid.group(1):
            raise RuntimeError("Persistence was not checked in a distinct application process")
        expected = {"/native-smoke/direct", "/native-smoke/restored", "/native-smoke/reconnected", "/native-smoke/rapid-reconnected", "/native-smoke/rapid-restored", "/native-smoke/include-direct", "/native-smoke/excluded-bypass", "/native-smoke/all-restored", "/native-smoke/sticky-restored", "/native-smoke/restart-mode-direct", "/native-smoke/restart-restored"}
        if not expected.issubset(set(requests)):
            raise RuntimeError(f"Fixture did not receive the required packet paths: {requests}")
        if any(path in requests for path in ("/native-smoke/reject", "/native-smoke/rapid-reject", "/native-smoke/include-reject", "/native-smoke/sticky-reject", "/native-smoke/restart-reject")):
            raise RuntimeError("REJECT traffic reached the HTTP fixture")
        capture_dir = pathlib.Path(__file__).resolve().parents[2] / "ui" / "screenshots"
        capture_dir.mkdir(parents=True, exist_ok=True)
        captures = []
        for state in ("live", "disconnected"):
            copied = subprocess.run([args.adb, "-s", args.serial, "exec-out", "run-as", TARGET,
                                     "cat", f"cache/native-smoke-overview-{state}.png"],
                                    stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                    timeout=15, check=True)
            if not copied.stdout.startswith(b"\x89PNG\r\n\x1a\n"):
                raise RuntimeError("Android returned an invalid native screenshot")
            destination = capture_dir / f"android-overview-{state}.png"
            destination.write_bytes(copied.stdout)
            captures.append(str(destination))
        report["captures"] = captures
        for page in ("profiles", "settings", "app-scope", "proxies"):
            copied = subprocess.run([args.adb, "-s", args.serial, "exec-out", "run-as", TARGET,
                                     "cat", f"cache/native-smoke-{page}.png"],
                                    stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=15, check=True)
            if not copied.stdout.startswith(b"\x89PNG\r\n\x1a\n"):
                raise RuntimeError("Android returned an invalid page screenshot")
            destination = capture_dir / f"android-{page}-live.png"
            destination.write_bytes(copied.stdout)
            captures.append(str(destination))
        if pathlib.Path(captures[0]).read_bytes() == pathlib.Path(captures[1]).read_bytes():
            raise RuntimeError("Connected and disconnected captures are identical; the UI did not refresh")
        report["success"] = True
        print("PASS: host observed DIRECT, restored and reconnected requests; REJECT never arrived", flush=True)
    except Exception as error:
        report["error"] = str(error)
        raise
    finally:
        adb("shell", "am", "force-stop", TARGET, check=False)
        adb("shell", "am", "force-stop", TEST, check=False)
        server.shutdown()
        server.server_close()
        thread.join(timeout=5)
        report["httpPaths"] = requests
        report_path.parent.mkdir(parents=True, exist_ok=True)
        report_path.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
        print(f"Saved native test result: {report_path}", flush=True)


if __name__ == "__main__":
    main()
