#!/usr/bin/env python3
"""Vātāyana: continuous X11 → MJPEG length-prefixed HTTP stream + xdotool input."""
import json
import os
import struct
import subprocess
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer
from socketserver import ThreadingMixIn

DISPLAY = os.environ.get("DISPLAY", ":99")
WIDTH = int(os.environ.get("BROWSER_WIDTH", "1280"))
HEIGHT = int(os.environ.get("BROWSER_HEIGHT", "720"))
FPS = int(os.environ.get("BROWSER_FPS", "12"))
PORT = int(os.environ.get("STREAM_PORT", "6080"))
JPEG_QUALITY = os.environ.get("BROWSER_JPEG_Q", "6")


class ThreadedHTTPServer(ThreadingMixIn, HTTPServer):
    daemon_threads = True
    allow_reuse_address = True


def extract_jpegs(buf: bytes):
    """Yield complete JPEG frames (SOI..EOI) and return leftover buffer."""
    frames = []
    while True:
        start = buf.find(b"\xff\xd8")
        if start < 0:
            return frames, b""
        if start > 0:
            buf = buf[start:]
        end = buf.find(b"\xff\xd9", 2)
        if end < 0:
            return frames, buf
        frames.append(buf[: end + 2])
        buf = buf[end + 2 :]


class StreamHandler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        pass

    def do_GET(self):
        path = self.path.split("?", 1)[0]
        if path == "/stream":
            self._handle_stream()
        elif path == "/screenshot":
            self._handle_screenshot()
        elif path == "/status":
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(
                json.dumps({"status": "ok", "width": WIDTH, "height": HEIGHT, "fps": FPS}).encode()
            )
        else:
            self.send_response(404)
            self.end_headers()

    def _handle_stream(self):
        self.send_response(200)
        self.send_header("Content-Type", "application/octet-stream")
        self.send_header("Cache-Control", "no-cache")
        self.send_header("Connection", "keep-alive")
        self.send_header("X-Browser-Width", str(WIDTH))
        self.send_header("X-Browser-Height", str(HEIGHT))
        self.end_headers()

        cmd = [
            "ffmpeg",
            "-loglevel",
            "error",
            "-f",
            "x11grab",
            "-video_size",
            f"{WIDTH}x{HEIGHT}",
            "-framerate",
            str(FPS),
            "-i",
            DISPLAY,
            "-codec:v",
            "mjpeg",
            "-q:v",
            JPEG_QUALITY,
            "-threads",
            "1",
            "-flush_packets",
            "1",
            "-f",
            "image2pipe",
            "-",
        ]
        proc = subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
        buf = b""
        try:
            assert proc.stdout is not None
            while True:
                chunk = proc.stdout.read(4096)
                if not chunk:
                    break
                buf += chunk
                frames, buf = extract_jpegs(buf)
                for jpeg in frames:
                    if len(jpeg) < 100:
                        continue
                    try:
                        self.wfile.write(struct.pack(">I", len(jpeg)))
                        self.wfile.write(jpeg)
                        self.wfile.flush()
                    except (BrokenPipeError, ConnectionResetError):
                        return
        except Exception as e:
            sys.stderr.write(f"stream error: {e}\n")
        finally:
            proc.kill()
            try:
                proc.wait(timeout=2)
            except Exception:
                pass

    def _handle_screenshot(self):
        cmd = [
            "ffmpeg",
            "-loglevel",
            "error",
            "-f",
            "x11grab",
            "-video_size",
            f"{WIDTH}x{HEIGHT}",
            "-i",
            DISPLAY,
            "-frames:v",
            "1",
            "-codec:v",
            "mjpeg",
            "-q:v",
            "4",
            "-f",
            "image2pipe",
            "-",
        ]
        proc = subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
        data = proc.stdout.read() if proc.stdout else b""
        proc.wait()
        self.send_response(200)
        self.send_header("Content-Type", "image/jpeg")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_POST(self):
        path = self.path.split("?", 1)[0]
        if path != "/input":
            self.send_response(404)
            self.end_headers()
            return

        content_len = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(content_len)
        try:
            event = json.loads(body)
            self.handle_input(event)
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps({"ok": True}).encode())
        except Exception as e:
            self.send_response(400)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps({"error": str(e)}).encode())

    def handle_input(self, event):
        etype = event.get("type")
        if etype == "mouse_move":
            x, y = int(event.get("x", 0)), int(event.get("y", 0))
            subprocess.run(["xdotool", "mousemove", str(x), str(y)], capture_output=True)
        elif etype == "mouse_down":
            subprocess.run(["xdotool", "mousedown", str(event.get("button", 1))], capture_output=True)
        elif etype == "mouse_up":
            subprocess.run(["xdotool", "mouseup", str(event.get("button", 1))], capture_output=True)
        elif etype == "mouse_click":
            btn = str(event.get("button", 1))
            x, y = event.get("x"), event.get("y")
            if x is not None and y is not None:
                subprocess.run(
                    ["xdotool", "mousemove", str(int(x)), str(int(y)), "click", btn],
                    capture_output=True,
                )
            else:
                subprocess.run(["xdotool", "click", btn], capture_output=True)
        elif etype == "key":
            key = event.get("key", "")
            if key:
                subprocess.run(["xdotool", "key", key], capture_output=True)
        elif etype == "type":
            text = event.get("text", "")
            if text:
                subprocess.run(["xdotool", "type", "--", text], capture_output=True)
        elif etype == "scroll":
            dy = event.get("dy", 0)
            btn = "5" if dy > 0 else "4"
            subprocess.run(["xdotool", "click", btn], capture_output=True)


if __name__ == "__main__":
    server = ThreadedHTTPServer(("0.0.0.0", PORT), StreamHandler)
    sys.stderr.write(f"[vatayana] stream server on :{PORT} ({WIDTH}x{HEIGHT}@{FPS}fps)\n")
    server.serve_forever()
