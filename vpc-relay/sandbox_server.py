#!/usr/bin/env python3
"""Yantrashala sandbox: WebSocket terminal + HTTP file API."""
import asyncio
import json
import os
import signal
import subprocess
import sys
from http.server import HTTPServer, BaseHTTPRequestHandler
from threading import Thread

import websockets
from websockets.asyncio.server import serve as ws_serve

SANDBOX_ROOT = "/sandbox"
FILES_DIR = os.path.join(SANDBOX_ROOT, "files")
TMP_DIR = os.path.join(SANDBOX_ROOT, "tmp")
DOWNLOADS_DIR = os.path.join(SANDBOX_ROOT, "downloads")
SCREENSHOTS_DIR = os.path.join(SANDBOX_ROOT, "screenshots")
SCRIPTS_DIR = os.path.join(SANDBOX_ROOT, "scripts")

WS_PORT = int(os.environ.get("SANDBOX_WS_PORT", "6090"))
HTTP_PORT = int(os.environ.get("SANDBOX_HTTP_PORT", "6091"))


# --- HTTP File API ---

class FileHandler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        pass

    def _send_json(self, status, data):
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Access-Control-Allow-Origin", "*")
        self.end_headers()
        self.wfile.write(json.dumps(data).encode())

    def _safe_path(self, subpath):
        allowed = {
            "/files": FILES_DIR,
            "/tmp": TMP_DIR,
            "/downloads": DOWNLOADS_DIR,
            "/screenshots": SCREENSHOTS_DIR,
            "/scripts": SCRIPTS_DIR,
        }
        for prefix, real in allowed.items():
            if subpath == prefix or subpath.startswith(prefix + "/"):
                resolved = os.path.normpath(os.path.join(real, subpath[len(prefix):].lstrip("/")))
                if resolved.startswith(os.path.normpath(real)):
                    return resolved
        return None

    def do_GET(self):
        path = self.path.split("?")[0]

        if path == "/storage":
            self._handle_storage()
            return

        real = self._safe_path(path)
        if real is None:
            self._send_json(404, {"error": "invalid path"})
            return

        if os.path.isdir(real):
            entries = []
            for name in sorted(os.listdir(real)):
                fp = os.path.join(real, name)
                st = os.stat(fp)
                entries.append({
                    "name": name,
                    "type": "dir" if os.path.isdir(fp) else "file",
                    "size": st.st_size,
                    "modified": int(st.st_mtime),
                })
            self._send_json(200, {"path": path, "entries": entries})
        elif os.path.isfile(real):
            self.send_response(200)
            self.send_header("Content-Type", "application/octet-stream")
            self.send_header("Content-Length", str(os.path.getsize(real)))
            self.end_headers()
            with open(real, "rb") as f:
                while True:
                    chunk = f.read(65536)
                    if not chunk:
                        break
                    self.wfile.write(chunk)
        else:
            self._send_json(404, {"error": "not found"})

    def do_PUT(self):
        path = self.path.split("?")[0]
        real = self._safe_path(path)
        if real is None:
            self._send_json(404, {"error": "invalid path"})
            return
        os.makedirs(os.path.dirname(real), exist_ok=True)
        length = int(self.headers.get("Content-Length", 0))
        data = self.rfile.read(length) if length > 0 else b""
        with open(real, "wb") as f:
            f.write(data)
        self._send_json(200, {"path": path, "written": len(data)})

    def do_DELETE(self):
        path = self.path.split("?")[0]
        real = self._safe_path(path)
        if real is None:
            self._send_json(404, {"error": "invalid path"})
            return
        if os.path.isdir(real):
            import shutil
            shutil.rmtree(real)
        elif os.path.isfile(real):
            os.remove(real)
        else:
            self._send_json(404, {"error": "not found"})
            return
        self._send_json(200, {"path": path, "deleted": True})

    def do_POST(self):
        if self.path == "/exec":
            length = int(self.headers.get("Content-Length", 0))
            body = json.loads(self.rfile.read(length))
            cmd = body.get("command", "")
            timeout = body.get("timeout", 30)
            try:
                proc = subprocess.run(
                    ["bash", "-c", cmd],
                    capture_output=True, text=True, timeout=timeout,
                    cwd=SANDBOX_ROOT,
                )
                self._send_json(200, {
                    "stdout": proc.stdout,
                    "stderr": proc.stderr,
                    "exit_code": proc.returncode,
                })
            except subprocess.TimeoutExpired:
                self._send_json(408, {"error": "timeout"})
        else:
            self._send_json(404, {})

    def _handle_storage(self):
        stats = {}
        for label, d in [("files", FILES_DIR), ("tmp", TMP_DIR),
                          ("downloads", DOWNLOADS_DIR), ("screenshots", SCREENSHOTS_DIR),
                          ("scripts", SCRIPTS_DIR)]:
            total = 0
            if os.path.isdir(d):
                for root, _, files in os.walk(d):
                    for f in files:
                        try:
                            total += os.path.getsize(os.path.join(root, f))
                        except OSError:
                            pass
            stats[label] = total
        self._send_json(200, stats)

    def do_OPTIONS(self):
        self.send_response(200)
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Methods", "GET, PUT, DELETE, POST, OPTIONS")
        self.send_header("Access-Control-Allow-Headers", "Content-Type, Authorization")
        self.end_headers()


# --- WebSocket Terminal ---

async def terminal_handler(websocket):
    """Each WebSocket client gets its own bash process."""
    proc = await asyncio.create_subprocess_exec(
        "bash", "-i",
        stdin=asyncio.subprocess.PIPE,
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.STDOUT,
        cwd=SANDBOX_ROOT,
        env={**os.environ, "HOME": SANDBOX_ROOT, "TERM": "xterm-256color"},
        preexec_fn=os.setsid,
    )

    async def stdin_forward():
        try:
            async for message in websocket:
                if proc.stdin:
                    proc.stdin.write(message.encode() if isinstance(message, str) else message)
                    await proc.stdin.drain()
        except Exception:
            pass
        finally:
            if proc.returncode is None:
                try:
                    os.killpg(os.getpgid(proc.pid), signal.SIGTERM)
                except Exception:
                    pass

    async def stdout_forward():
        try:
            if proc.stdout:
                while True:
                    chunk = await proc.stdout.read(4096)
                    if not chunk:
                        break
                    await websocket.send(chunk.decode("utf-8", errors="replace"))
        except Exception:
            pass

    await asyncio.gather(stdin_forward(), stdout_forward())
    if proc.returncode is None:
        try:
            os.killpg(os.getpgid(proc.pid), signal.SIGTERM)
        except Exception:
            pass


async def main():
    # Start HTTP file server in a thread
    httpd = HTTPServer(("0.0.0.0", HTTP_PORT), FileHandler)
    http_thread = Thread(target=httpd.serve_forever, daemon=True)
    http_thread.start()
    sys.stderr.write(f"[yantrashala] file API on :{HTTP_PORT}\n")

    # Start WebSocket terminal server
    sys.stderr.write(f"[yantrashala] terminal WS on :{WS_PORT}\n")
    async with ws_serve(terminal_handler, "0.0.0.0", WS_PORT):
        await asyncio.get_running_loop().create_future()  # run forever


if __name__ == "__main__":
    asyncio.run(main())
