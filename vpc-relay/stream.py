#!/usr/bin/env python3
"""Vātāyana: continuous X11 → MJPEG length-prefixed HTTP stream + xdotool input.

Agent-facing endpoints (Khadyota spirit — MD + actions over GUI):
  GET  /fetch?url=...  → fetch a page server-side, return title + markdown-ish text + links
  POST /navigate       → drive the visible Firefox to a URL (xdotool)
  GET  /window         → current window title ("what am I looking at")
"""
import json
import os
import re
import struct
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from html.parser import HTMLParser
from http.server import BaseHTTPRequestHandler, HTTPServer
from socketserver import ThreadingMixIn

DISPLAY = os.environ.get("DISPLAY", ":99")
WIDTH = int(os.environ.get("BROWSER_WIDTH", "1280"))
HEIGHT = int(os.environ.get("BROWSER_HEIGHT", "720"))
FPS = int(os.environ.get("BROWSER_FPS", "12"))
PORT = int(os.environ.get("STREAM_PORT", "6080"))
JPEG_QUALITY = os.environ.get("BROWSER_JPEG_Q", "6")

FETCH_UA = os.environ.get(
    "FETCH_UA",
    "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0 GAFAM-Vatayana",
)
FETCH_MAX_BYTES = 2_000_000
FETCH_TEXT_CAP = 50_000
FETCH_LINK_CAP = 200


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


# --- Agent helpers (fetch → markdown-ish text, window title) -------------------

_BLOCK_TAGS = {
    "p", "div", "br", "li", "ul", "ol", "tr", "table", "section", "article",
    "header", "footer", "blockquote", "pre", "hr", "form", "figure",
}
_SKIP_TAGS = {"script", "style", "noscript", "svg", "template"}


class PageTextExtractor(HTMLParser):
    """Minimal HTML → readable text/markdown converter (stdlib only)."""

    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.parts = []
        self.links = []
        self.title = ""
        self._skip_depth = 0
        self._in_title = False
        self._heading = 0
        self._cur_href = None

    def handle_starttag(self, tag, attrs):
        if tag in _SKIP_TAGS:
            self._skip_depth += 1
            return
        if self._skip_depth:
            return
        if tag == "title":
            self._in_title = True
        elif tag in ("h1", "h2", "h3", "h4", "h5", "h6"):
            self._heading = int(tag[1])
            self.parts.append("\n" + "#" * self._heading + " ")
        elif tag in _BLOCK_TAGS:
            self.parts.append("\n")
        elif tag == "li":
            self.parts.append("- ")
        elif tag == "a":
            self._cur_href = dict(attrs).get("href")
        elif tag == "img":
            alt = dict(attrs).get("alt", "").strip()
            if alt:
                self.parts.append(f"[img: {alt}]")

    def handle_endtag(self, tag):
        if tag in _SKIP_TAGS:
            if self._skip_depth:
                self._skip_depth -= 1
            return
        if self._skip_depth:
            return
        if tag == "title":
            self._in_title = False
        elif tag in ("h1", "h2", "h3", "h4", "h5", "h6"):
            self._heading = 0
            self.parts.append("\n")
        elif tag in _BLOCK_TAGS:
            self.parts.append("\n")
        elif tag == "a":
            self._cur_href = None

    def handle_data(self, data):
        if self._skip_depth:
            return
        if self._in_title:
            self.title += data.strip()
            return
        text = re.sub(r"\s+", " ", data)
        if not text.strip():
            return
        self.parts.append(text)
        if self._cur_href and len(self.links) < FETCH_LINK_CAP:
            label = text.strip()
            if label:
                self.links.append({"text": label, "href": self._cur_href})

    def text(self):
        raw = "".join(self.parts)
        # Collapse 3+ newlines, strip trailing spaces per line.
        raw = re.sub(r"[ \t]+\n", "\n", raw)
        raw = re.sub(r"\n{3,}", "\n\n", raw)
        return raw.strip()


def _fetch_page(url):
    if not url.lower().startswith(("http://", "https://")):
        return 400, {"error": "url must start with http:// or https://"}
    req = urllib.request.Request(url, headers={"User-Agent": FETCH_UA, "Accept": "text/html,application/xhtml+xml,*/*"})
    try:
        with urllib.request.urlopen(req, timeout=20) as resp:
            content_type = resp.headers.get_content_type()
            status = resp.status
            final_url = resp.geturl()
            data = resp.read(FETCH_MAX_BYTES)
    except urllib.error.HTTPError as e:
        return 502, {"error": f"upstream HTTP {e.code}", "url": url}
    except Exception as e:
        return 502, {"error": f"fetch failed: {e}", "url": url}

    result = {
        "url": url,
        "final_url": final_url,
        "status": status,
        "content_type": content_type,
        "fetched_at": int(time.time()),
    }

    if content_type in ("text/html", "application/xhtml+xml"):
        ex = PageTextExtractor()
        try:
            ex.feed(data.decode("utf-8", errors="replace"))
        except Exception:
            pass
        text = ex.text()[:FETCH_TEXT_CAP]
        result.update({
            "title": ex.title,
            "text": text,
            "links": ex.links,
            "truncated": len(ex.text()) > FETCH_TEXT_CAP,
        })
    else:
        # Non-HTML: return an excerpt as raw text.
        result.update({
            "title": "",
            "text": data[:FETCH_TEXT_CAP].decode("utf-8", errors="replace"),
            "links": [],
            "truncated": len(data) >= FETCH_MAX_BYTES,
        })
    return 200, result


def _chrome_windows():
    """Visible Chrome (for Testing) window IDs, best class match first."""
    try:
        out = subprocess.run(
            ["xdotool", "search", "--onlyvisible", "--class", "google"],
            capture_output=True, text=True, timeout=5,
        )
        wins = [w for w in out.stdout.split() if w.strip()]
        if wins:
            return wins
    except Exception:
        pass
    try:
        out = subprocess.run(
            ["xdotool", "search", "--onlyvisible", "--name", "Chrome"],
            capture_output=True, text=True, timeout=5,
        )
        return [w for w in out.stdout.split() if w.strip()]
    except Exception:
        return []


def _chrome_geometry():
    """(W, H, X, Y) of the Chrome window client area, or None if invisible."""
    wins = _chrome_windows()
    if not wins:
        return None
    try:
        out = subprocess.run(
            ["xdotool", "getwindowgeometry", "--shell", wins[0]],
            capture_output=True, text=True, timeout=5,
        )
        vals = {}
        for line in out.stdout.splitlines():
            if "=" in line:
                k, v = line.split("=", 1)
                vals[k.strip()] = int(v.strip())
        if "WIDTH" in vals and "HEIGHT" in vals and "X" in vals and "Y" in vals:
            return vals["WIDTH"], vals["HEIGHT"], vals["X"], vals["Y"]
    except Exception:
        pass
    return None


def _capture_geometry():
    """Current capture region: the Chrome window client area when visible
    (no dead black X11 margins), otherwise the whole screen."""
    geo = _chrome_geometry()
    if geo:
        w, h, x, y = geo
        # Clamp: ignore off-screen/negative windows, cap at the screen.
        if w > 0 and h > 0 and x < WIDTH and y < HEIGHT:
            x = max(0, x)
            y = max(0, y)
            w = min(w, WIDTH - x)
            h = min(h, HEIGHT - y)
            if w > 0 and h > 0:
                return w, h, x, y
    return WIDTH, HEIGHT, 0, 0


def _navigate(url):
    wins = _chrome_windows()
    if not wins:
        return {"ok": False, "error": "no Chrome window found"}
    win = wins[0]
    steps = [
        ["xdotool", "windowactivate", "--sync", win],
        ["xdotool", "key", "ctrl+l"],
        ["xdotool", "type", "--delay", "15", "--", url],
        ["xdotool", "key", "Return"],
    ]
    for step in steps:
        r = subprocess.run(step, capture_output=True, text=True, timeout=10)
        if r.returncode != 0:
            return {"ok": False, "error": r.stderr.strip() or "xdotool failed", "step": step[1]}
    return {"ok": True, "url": url, "window": win}


def _window_title():
    try:
        out = subprocess.run(
            ["xdotool", "getactivewindow", "getwindowname"],
            capture_output=True, text=True, timeout=5,
        )
        title = out.stdout.strip()
        if title:
            return title
    except Exception:
        pass
    wins = _chrome_windows()
    if wins:
        try:
            out = subprocess.run(
                ["xdotool", "getwindowname", wins[0]],
                capture_output=True, text=True, timeout=5,
            )
            return out.stdout.strip()
        except Exception:
            return ""
    return ""


class StreamHandler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        pass

    def _send_json(self, status, data):
        payload = json.dumps(data).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def do_GET(self):
        path = self.path.split("?", 1)[0]
        if path == "/stream":
            self._handle_stream()
        elif path == "/screenshot":
            self._handle_screenshot()
        elif path == "/status":
            self._send_json(200, {"status": "ok", "width": WIDTH, "height": HEIGHT, "fps": FPS})
        elif path == "/fetch":
            qs = urllib.parse.parse_qs(urllib.parse.urlparse(self.path).query)
            url = qs.get("url", [""])[0]
            if not url:
                self._send_json(400, {"error": "missing 'url' query param"})
                return
            status, result = _fetch_page(url)
            self._send_json(status, result)
        elif path == "/window":
            geo = _capture_geometry()
            self._send_json(200, {
                "title": _window_title(),
                "width": geo[0],
                "height": geo[1],
                "x": geo[2],
                "y": geo[3],
                "chrome_windows": len(_chrome_windows()),
            })
        else:
            self.send_response(404)
            self.end_headers()

    def _handle_stream(self):
        w, h, x, y = _capture_geometry()
        self.send_response(200)
        self.send_header("Content-Type", "application/octet-stream")
        self.send_header("Cache-Control", "no-cache")
        self.send_header("Connection", "keep-alive")
        self.send_header("X-Browser-Width", str(w))
        self.send_header("X-Browser-Height", str(h))
        self.end_headers()

        cmd = [
            "ffmpeg",
            "-loglevel",
            "error",
            "-f",
            "x11grab",
            "-video_size",
            f"{w}x{h}",
            "-framerate",
            str(FPS),
            "-i",
            f"{DISPLAY}+{x},{y}",
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
        w, h, x, y = _capture_geometry()
        cmd = [
            "ffmpeg",
            "-loglevel",
            "error",
            "-f",
            "x11grab",
            "-video_size",
            f"{w}x{h}",
            "-i",
            f"{DISPLAY}+{x},{y}",
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

        if path == "/navigate":
            content_len = int(self.headers.get("Content-Length", 0))
            body = self.rfile.read(content_len) if content_len else b"{}"
            try:
                payload = json.loads(body)
            except Exception:
                payload = {}
            url = str(payload.get("url", "")).strip()
            if not url:
                self._send_json(400, {"error": "missing 'url'"})
                return
            if not url.lower().startswith(("http://", "https://", "about:")):
                url = "https://" + url
            try:
                self._send_json(200, _navigate(url))
            except Exception as e:
                self._send_json(500, {"ok": False, "error": str(e)})
            return

        if path != "/input":
            self.send_response(404)
            self.end_headers()
            return

        content_len = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(content_len)
        try:
            event = json.loads(body)
            self.handle_input(event)
            self._send_json(200, {"ok": True})
        except Exception as e:
            self._send_json(400, {"error": str(e)})

    def handle_input(self, event):
        etype = event.get("type")
        # Stream is cropped to the Chrome client area; canvas coords are
        # relative to that crop. xdotool mousemove is screen-absolute.
        ox = oy = 0
        if etype in ("mouse_move", "mouse_click"):
            geo = _capture_geometry()
            if geo:
                ox, oy = geo[2], geo[3]
        if etype == "mouse_move":
            x, y = int(event.get("x", 0)) + ox, int(event.get("y", 0)) + oy
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
                    ["xdotool", "mousemove", str(int(x) + ox), str(int(y) + oy), "click", btn],
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
