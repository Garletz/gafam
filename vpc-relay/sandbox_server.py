#!/usr/bin/env python3
"""Yantrashala sandbox: persistent shell sessions + tree API + HTTP file API + WS terminal."""
import asyncio
import json
import os
import re
import select
import signal
import subprocess
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from threading import Thread
from urllib.parse import urlparse, parse_qs

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

TOP_LEVEL = [
    ("files", FILES_DIR),
    ("downloads", DOWNLOADS_DIR),
    ("screenshots", SCREENSHOTS_DIR),
    ("scripts", SCRIPTS_DIR),
    ("tmp", TMP_DIR),
]

TREE_ENTRY_CAP = 2000
SHELL_IDLE_REAP_S = 1800  # 30 min


# --- Persistent shell sessions -------------------------------------------------
# One long-lived bash per session id. Humans (web terminal) and agents (Kāraka
# sandbox.shell) share the same sessions: cwd and env persist between commands.

SHELL_MARKER = "__GAFAM_SHELL_DONE__"
_shell_sessions = {}
_shell_sessions_lock = threading.Lock()


class ShellSession:
    def __init__(self, session_id):
        self.id = session_id
        self.lock = threading.Lock()
        self.last_used = time.time()
        self.proc = subprocess.Popen(
            ["bash", "--norc"],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            cwd=SANDBOX_ROOT,
            env={
                **os.environ,
                "HOME": SANDBOX_ROOT,
                "TERM": "dumb",
                "PS1": "",
                "PS2": "",
                "PROMPT_COMMAND": "",
            },
            preexec_fn=os.setsid,
            bufsize=0,
        )
        self.cwd = SANDBOX_ROOT

    def alive(self):
        return self.proc.poll() is None

    def kill(self):
        try:
            os.killpg(os.getpgid(self.proc.pid), signal.SIGKILL)
        except Exception:
            pass

    def run(self, command, timeout):
        """Run command in the persistent shell. Returns dict result."""
        with self.lock:
            self.last_used = time.time()
            if not self.alive():
                return {"error": "session_dead", "session_id": self.id}

            payload = (
                f"{command}\n"
                f'__gafam_rc=$?; echo "{SHELL_MARKER}:${{__gafam_rc}}:$PWD"\n'
            )
            try:
                self.proc.stdin.write(payload.encode())
                self.proc.stdin.flush()
            except (BrokenPipeError, OSError):
                return {"error": "session_dead", "session_id": self.id}

            fd = self.proc.stdout.fileno()
            buf = b""
            deadline = time.time() + timeout
            timed_out = False

            while True:
                remaining = deadline - time.time()
                if remaining <= 0:
                    timed_out = True
                    break
                try:
                    r, _, _ = select.select([fd], [], [], min(0.5, remaining))
                except (OSError, ValueError):
                    break
                if not r:
                    if self.proc.poll() is not None:
                        try:
                            buf += os.read(fd, 65536)
                        except OSError:
                            pass
                        break
                    continue
                try:
                    chunk = os.read(fd, 65536)
                except OSError:
                    break
                if not chunk:
                    break
                buf += chunk
                if SHELL_MARKER.encode() in buf:
                    break

            text = buf.decode("utf-8", errors="replace")

            if timed_out:
                # Cannot interrupt a foreground piped command cleanly: reset session.
                self.kill()
                return {
                    "error": "timeout",
                    "note": "command killed — shell session reset (run again to reopen)",
                    "session_id": self.id,
                    "output": text,
                    "timed_out": True,
                }

            m = re.search(re.escape(SHELL_MARKER) + r":(\d+):([^\n\r]*)", text)
            if m:
                exit_code = int(m.group(1))
                self.cwd = m.group(2).strip() or self.cwd
                output = text[: m.start()]
                # Strip the single newline printf'd before the marker.
                if output.endswith("\n"):
                    output = output[:-1]
            else:
                # Shell died mid-command or marker lost.
                exit_code = -1
                output = text
                if not self.alive():
                    return {
                        "error": "session_dead",
                        "session_id": self.id,
                        "output": output,
                    }

            return {
                "session_id": self.id,
                "output": output,
                "exit_code": exit_code,
                "cwd": self.cwd,
                "timed_out": False,
            }


def _get_session(session_id):
    with _shell_sessions_lock:
        sess = _shell_sessions.get(session_id)
        if sess is not None and not sess.alive():
            try:
                sess.kill()
            except Exception:
                pass
            sess = None
        if sess is None:
            sess = ShellSession(session_id)
            _shell_sessions[session_id] = sess
        return sess


def _reap_idle_sessions():
    now = time.time()
    with _shell_sessions_lock:
        for sid, sess in list(_shell_sessions.items()):
            if now - sess.last_used > SHELL_IDLE_REAP_S or not sess.alive():
                try:
                    sess.kill()
                except Exception:
                    pass
                del _shell_sessions[sid]


# --- Tree ----------------------------------------------------------------------


def _human_size(n):
    if n < 1024:
        return f"{n} B"
    if n < 1024 * 1024:
        return f"{n / 1024:.1f} KB"
    if n < 1024 * 1024 * 1024:
        return f"{n / (1024 * 1024):.1f} MB"
    return f"{n / (1024 * 1024 * 1024):.1f} GB"


def _tree_node(name, rel_path, real_path, depth, counter, truncated):
    node = {"name": name, "path": rel_path, "type": "dir", "size": 0, "modified": 0}
    try:
        st = os.stat(real_path)
        node["modified"] = int(st.st_mtime)
    except OSError:
        pass

    children = []
    if depth > 0:
        try:
            names = sorted(os.listdir(real_path))
        except OSError:
            names = []
        # Directories first, then files, alphabetical inside each group.
        names.sort(key=lambda n: (not os.path.isdir(os.path.join(real_path, n)), n.lower()))
        for child in names:
            if counter[0] >= TREE_ENTRY_CAP:
                truncated[0] = True
                break
            counter[0] += 1
            creal = os.path.join(real_path, child)
            crel = rel_path.rstrip("/") + "/" + child
            if os.path.isdir(creal):
                children.append(_tree_node(child, crel, creal, depth - 1, counter, truncated))
            else:
                try:
                    cst = os.stat(creal)
                    children.append({
                        "name": child,
                        "path": crel,
                        "type": "file",
                        "size": cst.st_size,
                        "modified": int(cst.st_mtime),
                    })
                except OSError:
                    continue
    node["children"] = children
    return node


def _tree_totals(node):
    """Return (n_dirs, n_files, total_bytes) below node (excluding node itself)."""
    d = f = 0
    total = 0
    for c in node.get("children") or []:
        if c["type"] == "dir":
            d += 1
            sd, sf, st = _tree_totals(c)
            d += sd
            f += sf
            total += st
        else:
            f += 1
            total += c.get("size", 0)
    return d, f, total


def _render_ascii(root):
    lines = []
    d, f, total = _tree_totals(root)
    lines.append(f"{root['name']}  ({d} dirs, {f} files, {_human_size(total)})")

    def walk(node, prefix):
        children = node.get("children") or []
        for i, child in enumerate(children):
            last = i == len(children) - 1
            connector = "└── " if last else "├── "
            if child["type"] == "dir":
                lines.append(f"{prefix}{connector}{child['name']}/")
                walk(child, prefix + ("    " if last else "│   "))
            else:
                lines.append(f"{prefix}{connector}{child['name']} ({_human_size(child.get('size', 0))})")

    walk(root, "")
    return "\n".join(lines)


# --- HTTP File API ---


class FileHandler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        pass

    def _send_json(self, status, data):
        payload = json.dumps(data).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.send_header("Access-Control-Allow-Origin", "*")
        self.end_headers()
        self.wfile.write(payload)

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
        parsed = urlparse(self.path)
        path = parsed.path

        if path == "/storage":
            self._handle_storage()
            return

        if path == "/tree":
            self._handle_tree(parse_qs(parsed.query))
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

    def _handle_tree(self, query):
        tpath = query.get("path", ["/"])[0] or "/"
        try:
            depth = min(max(int(query.get("depth", ["4"])[0]), 0), 8)
        except ValueError:
            depth = 4
        fmt = query.get("format", ["json"])[0]

        counter = [0]
        truncated = [False]

        if tpath == "/":
            root = {"name": "/", "path": "/", "type": "dir", "size": 0,
                    "modified": int(time.time()), "children": []}
            for label, real in TOP_LEVEL:
                if os.path.isdir(real):
                    root["children"].append(
                        _tree_node(label, "/" + label, real, depth, counter, truncated))
        else:
            real = self._safe_path(tpath)
            if real is None or not os.path.isdir(real):
                self._send_json(404, {"error": "invalid path"})
                return
            root = _tree_node(tpath.rstrip("/"), tpath, real, depth, counter, truncated)

        if fmt == "ascii":
            self._send_json(200, {
                "path": tpath,
                "format": "ascii",
                "ascii": _render_ascii(root),
                "truncated": truncated[0],
            })
        else:
            self._send_json(200, {
                "path": tpath,
                "format": "json",
                "root": root,
                "truncated": truncated[0],
            })

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
        parsed = urlparse(self.path)
        path = parsed.path

        if path == "/exec":
            self._handle_exec()
            return

        if path == "/shell/exec":
            self._handle_shell_exec()
            return

        if path == "/shell/create":
            length = int(self.headers.get("Content-Length", 0))
            body = json.loads(self.rfile.read(length)) if length > 0 else {}
            session_id = str(body.get("session_id") or "main")
            sess = _get_session(session_id)
            self._send_json(200, {"session_id": sess.id, "cwd": sess.cwd, "created": True})
            return

        if path == "/shell/close":
            length = int(self.headers.get("Content-Length", 0))
            body = json.loads(self.rfile.read(length)) if length > 0 else {}
            session_id = str(body.get("session_id") or "main")
            with _shell_sessions_lock:
                sess = _shell_sessions.pop(session_id, None)
            if sess:
                sess.kill()
            self._send_json(200, {"session_id": session_id, "closed": sess is not None})
            return

        if path == "/shell/list":
            _reap_idle_sessions()
            with _shell_sessions_lock:
                sessions = [
                    {"session_id": s.id, "cwd": s.cwd, "idle_s": int(time.time() - s.last_used)}
                    for s in _shell_sessions.values()
                ]
            self._send_json(200, {"sessions": sessions})
            return

        self._send_json(404, {"error": f"unknown post path: {path}"})

    def _handle_exec(self):
        """One-shot stateless exec (kept for backward compatibility)."""
        length = int(self.headers.get("Content-Length", 0))
        body = json.loads(self.rfile.read(length)) if length > 0 else {}
        cmd = body.get("command", "")
        timeout = int(body.get("timeout", 30))
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
        except Exception as e:
            self._send_json(500, {"error": str(e)})

    def _handle_shell_exec(self):
        """Persistent shell exec — shared by the web terminal and Kāraka agents."""
        _reap_idle_sessions()
        length = int(self.headers.get("Content-Length", 0))
        body = json.loads(self.rfile.read(length)) if length > 0 else {}
        cmd = body.get("command", "")
        if not cmd.strip():
            self._send_json(400, {"error": "missing command"})
            return
        session_id = str(body.get("session_id") or "main")
        timeout = int(body.get("timeout", 60))
        timeout = max(1, min(timeout, 600))

        sess = _get_session(session_id)
        result = sess.run(cmd, timeout)
        if result.get("error") in ("session_dead", "timeout"):
            # Drop the broken session so the next call starts fresh.
            with _shell_sessions_lock:
                if _shell_sessions.get(session_id) is sess:
                    del _shell_sessions[session_id]
            status = 408 if result.get("error") == "timeout" else 200
            self._send_json(status, result)
            return
        self._send_json(200, result)

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
    # Start HTTP file server in a thread (threaded: long shell commands must not
    # block tree/file requests from other clients).
    httpd = ThreadingHTTPServer(("0.0.0.0", HTTP_PORT), FileHandler)
    http_thread = Thread(target=httpd.serve_forever, daemon=True)
    http_thread.start()
    sys.stderr.write(f"[yantrashala] file API on :{HTTP_PORT}\n")

    # Start WebSocket terminal server
    sys.stderr.write(f"[yantrashala] terminal WS on :{WS_PORT}\n")
    async with ws_serve(terminal_handler, "0.0.0.0", WS_PORT):
        await asyncio.get_running_loop().create_future()  # run forever


if __name__ == "__main__":
    asyncio.run(main())
