#!/usr/bin/env python3
"""Exercise the shipped C ABI and a real loopback SOCKS traffic path.

The test owns a temporary home and HTTP server. It never reads installed
profiles, installs a service, or changes the operating system's proxy settings.
"""

import argparse
import ctypes
import http.server
import json
import socket
import tempfile
import threading


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--library", required=True)
    args = parser.parse_args()
    lib = ctypes.CDLL(args.library)
    lib.GopurrOpen.argtypes = [ctypes.c_char_p]
    lib.GopurrOpen.restype = ctypes.c_void_p
    lib.GopurrCall.argtypes = [ctypes.c_uint64, ctypes.c_char_p]
    lib.GopurrCall.restype = ctypes.c_void_p
    lib.GopurrClose.argtypes = [ctypes.c_uint64]
    lib.GopurrFree.argtypes = [ctypes.c_void_p]

    def decoded(pointer):
        assert pointer, "C ABI returned a null response"
        try:
            return json.loads(ctypes.string_at(pointer))
        finally:
            lib.GopurrFree(pointer)

    def invoke(handle, method, params=None):
        request = json.dumps({"method": method, "params": params or {}}).encode()
        response = decoded(lib.GopurrCall(handle, request))
        assert "error" not in response, (method, response)
        return response.get("result")

    payload = b"Gopurr NSH native bridge: verified loopback traffic\n"

    class Handler(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            self.send_response(200)
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)

        def log_message(self, *_args):
            pass

    with tempfile.TemporaryDirectory(prefix="gopurr-native-smoke-") as home:
        opened = decoded(lib.GopurrOpen(json.dumps({"home": home}).encode()))
        assert "error" not in opened, opened
        handle = opened["handle"]
        server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        worker = threading.Thread(target=server.serve_forever, daemon=True)
        worker.start()
        try:
            status = invoke(handle, "engine.status")
            assert not status["running"] and not status["vpnSupported"], status
            assert not status["systemProxy"] and not status["tun"], status
            assert invoke(handle, "profiles.list")["profiles"] == []
            imported = invoke(handle, "profiles.import", {
                "name": "Native ABI smoke",
                "yaml": "mode: rule\nproxies: []\nproxy-groups:\n"
                        "  - name: Route\n    type: select\n"
                        "    proxies: [DIRECT, REJECT]\nrules: ['MATCH,Route']\n",
            })
            profile_id = imported["id"]
            invoke(handle, "profiles.activate", {"id": profile_id})
            invoke(handle, "engine.start")
            status = invoke(handle, "engine.status")
            assert status["running"] and status["activeProfile"] == profile_id, status
            groups = invoke(handle, "proxies.groups")["groups"]
            assert any(group["name"] == "Route" for group in groups), groups
            invoke(handle, "proxies.select", {"group": "Route", "member": "REJECT"})
            groups = invoke(handle, "proxies.groups")["groups"]
            assert next(group for group in groups if group["name"] == "Route")["now"] == "REJECT"
            invoke(handle, "proxies.select", {"group": "Route", "member": "DIRECT"})

            def receive_exact(connection, length):
                data = b""
                while len(data) < length:
                    part = connection.recv(length - len(data))
                    assert part, "SOCKS connection closed early"
                    data += part
                return data

            with socket.create_connection(("127.0.0.1", status["mixedPort"]), timeout=5) as connection:
                connection.sendall(b"\x05\x01\x00")
                assert receive_exact(connection, 2) == b"\x05\x00"
                port = server.server_address[1]
                connection.sendall(b"\x05\x01\x00\x01\x7f\x00\x00\x01" + port.to_bytes(2, "big"))
                reply = receive_exact(connection, 4)
                assert reply[:2] == b"\x05\x00", reply
                address_size = {1: 4, 4: 16}.get(reply[3])
                if reply[3] == 3:
                    address_size = receive_exact(connection, 1)[0]
                assert address_size is not None, reply
                receive_exact(connection, address_size + 2)
                connection.sendall(b"GET / HTTP/1.0\r\nHost: localhost\r\n\r\n")
                response = b""
                while chunk := connection.recv(4096):
                    response += chunk
                assert response.split(b"\r\n\r\n", 1)[1] == payload
            invoke(handle, "engine.stop")
            assert not invoke(handle, "engine.status")["running"]
        finally:
            lib.GopurrClose(handle)
            lib.GopurrClose(handle)
            server.shutdown()
            server.server_close()
            worker.join(timeout=5)
        closed = decoded(lib.GopurrCall(handle, b'{"method":"engine.status"}'))
        assert closed["error"]["code"] == "closed", closed
    print("Native C ABI smoke passed: import, activation, selection, SOCKS traffic, stop, close")


if __name__ == "__main__":
    main()
