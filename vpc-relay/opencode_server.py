#!/usr/bin/env python3
"""Dakṣiṇā runner: headless opencode with GitHub self-improvement.

The relay (gafam-api) posts a task, this server:
  1. clones/updates the GAFAM repo in a persistent workspace,
  2. writes the opencode provider config for the task,
  3. runs `opencode run --auto` on the instruction with a context pack,
  4. runs the relevant test suites, repairs up to 2 times,
  5. pushes a branch and opens a PR (auto-merge when green).

Everything is written under /daksina/runs/<id>/ so the relay can poll and the
human can inspect the full transcript later. Only one task runs at a time.
"""

import base64
import json
import os
import re
import shutil
import signal
import sqlite3
import subprocess
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlparse

VERSION = "1.0"

DAKSINA = Path("/daksina")
RUNS = DAKSINA / "runs"
WORKSPACE = DAKSINA / "workspace"
REPO_DIR = WORKSPACE / "gafam"
CACHE_DIR = WORKSPACE / ".cache"
SESSIONS_IMPORTED = DAKSINA / "sessions" / "imported"
CONTEXT_ROOT = DAKSINA / "context"
OPENCODE_DB = Path("/root/.local/share/opencode/opencode.db")
OPENCODE_CONFIG = Path("/root/.config/opencode/opencode.json")
HOME = Path("/root")

HTTP_PORT = int(os.environ.get("OPENCODE_PORT", "6092"))
MAX_INSTRUCTION = 12000
MAX_LOG_TAIL = 16000
AGENT_TIMEOUT_S = int(os.environ.get("DAKSINA_AGENT_TIMEOUT", "1200"))  # 20 min
TEST_TIMEOUT_S = int(os.environ.get("DAKSINA_TEST_TIMEOUT", "900"))  # 15 min
MERGE_WAIT_S = int(os.environ.get("DAKSINA_MERGE_WAIT", "1200"))  # 20 min
RUN_KEEP = int(os.environ.get("DAKSINA_RUN_KEEP", "40"))

SHARED_SECRET = os.environ.get("OPENCODE_SECRET", "")
GIT_BASE = os.environ.get("DAKSINA_GIT_BASE", "https://github.com").rstrip("/")
RUN_ID_RE = re.compile(r"^[A-Za-z0-9_-]{4,80}$")
REPO_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9][A-Za-z0-9_.-]*$")
ANSI_RE = re.compile(r"\x1b\[[0-9;?]*[ -/]*[@-~]")

ACTIVE_LOCK = threading.Lock()
ACTIVE = {}  # run_id -> {"proc": Popen, "cancelled": bool}
CANCELLED = set()
BUSY = {"id": ""}


# ─── small helpers ─────────────────────────────────────────────────────────

def utc_ms():
    return int(time.time() * 1000)


def safe_json(path, default=None):
    try:
        with open(path, "r", encoding="utf-8") as f:
            return json.load(f)
    except Exception:
        return default


def write_json(path, obj):
    tmp = str(path) + ".tmp"
    with open(tmp, "w", encoding="utf-8") as f:
        json.dump(obj, f, ensure_ascii=False, indent=2)
    os.replace(tmp, path)


def redact(text, token):
    if token:
        text = text.replace(token, "***")
    return ANSI_RE.sub("", text)


def tail_text(path, limit=MAX_LOG_TAIL):
    try:
        size = os.path.getsize(path)
        with open(path, "rb") as f:
            if size > limit:
                f.seek(size - limit)
            data = f.read()
        return data.decode("utf-8", errors="replace")
    except Exception:
        return ""


def safe_id(value, fallback="run"):
    value = re.sub(r"[^A-Za-z0-9_.-]", "_", str(value or ""))
    return value[:80] or fallback


def ensure_layout():
    for d in (RUNS, WORKSPACE, CACHE_DIR, SESSIONS_IMPORTED, CONTEXT_ROOT,
              HOME / ".local/share/opencode", HOME / ".config/opencode"):
        d.mkdir(parents=True, exist_ok=True)


def prune_runs():
    try:
        runs = sorted(
            [p for p in RUNS.iterdir() if p.is_dir()],
            key=lambda p: p.stat().st_mtime,
            reverse=True,
        )
        keep = {p.name for p in runs[:RUN_KEEP]}
        for old in runs[RUN_KEEP:]:
            shutil.rmtree(old, ignore_errors=True)
        if CONTEXT_ROOT.exists():
            for ctx in CONTEXT_ROOT.iterdir():
                if ctx.is_dir() and ctx.name not in keep:
                    shutil.rmtree(ctx, ignore_errors=True)
    except Exception:
        pass


def proc_group_kill(proc):
    try:
        os.killpg(os.getpgid(proc.pid), signal.SIGKILL)
    except Exception:
        try:
            proc.kill()
        except Exception:
            pass


def git_env(token):
    env = {
        **os.environ,
        "HOME": str(HOME),
        "GIT_TERMINAL_PROMPT": "0",
        "GIT_AUTHOR_NAME": "Dakṣiṇā",
        "GIT_AUTHOR_EMAIL": "daksina@gafam.cloud",
        "GIT_COMMITTER_NAME": "Dakṣiṇā",
        "GIT_COMMITTER_EMAIL": "daksina@gafam.cloud",
        "GOPATH": str(WORKSPACE / ".gopath"),
        "GOCACHE": str(CACHE_DIR / "go-build"),
        "GOMODCACHE": str(CACHE_DIR / "go-mod"),
        "CGO_ENABLED": "0",
        "NO_COLOR": "1",
        "TERM": "dumb",
        "PATH": "/root/.opencode/bin:/usr/local/go/bin:" + os.environ.get("PATH", "/usr/bin:/bin"),
    }
    if token:
        auth = base64.b64encode(f"x-access-token:{token}".encode()).decode()
        env["GIT_CONFIG_COUNT"] = "1"
        env["GIT_CONFIG_KEY_0"] = "http.extraHeader"
        env["GIT_CONFIG_VALUE_0"] = f"AUTHORIZATION: basic {auth}"
        env["GH_TOKEN"] = token
        env["GITHUB_TOKEN"] = token
    return env


def run_cmd(args, cwd, timeout, log_path, token, env=None):
    """Run a command, stream its output into log_path, return (rc, output)."""
    if env is None:
        env = git_env(token)
    entry = f"\n$ {' '.join(args)}\n"
    append_log(log_path, redact(entry, token))
    proc = subprocess.Popen(
        args,
        cwd=str(cwd),
        env=env,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        start_new_session=True,
        text=True,
        bufsize=1,
    )
    out_lines = []

    def pump():
        try:
            for line in proc.stdout:
                out_lines.append(line)
                append_log(log_path, redact(line, token))
        except Exception:
            pass

    t = threading.Thread(target=pump, daemon=True)
    t.start()
    try:
        rc = proc.wait(timeout=timeout)
    except subprocess.TimeoutExpired:
        proc_group_kill(proc)
        rc = -9
        append_log(log_path, "\n[timeout] command killed\n")
    t.join(timeout=5)
    return rc, "".join(out_lines)


def append_log(path, text):
    try:
        with open(path, "a", encoding="utf-8") as f:
            f.write(text)
    except Exception:
        pass


def is_cancelled(run_id):
    with ACTIVE_LOCK:
        return run_id in CANCELLED


def mark_cancelled(task, reason="Annulé par l'utilisateur."):
    save_status(task, status="cancelled", phase="cancelled", summary=reason)
    with ACTIVE_LOCK:
        CANCELLED.add(task["id"])


# ─── status + task state ──────────────────────────────────────────────────

def status_path(run_id):
    return RUNS / run_id / "status.json"


def save_status(task, **patch):
    task.update(patch)
    task["updated_at"] = utc_ms()
    try:
        write_json(status_path(task["id"]), task)
    except Exception:
        pass


def load_status(run_id):
    return safe_json(status_path(run_id), default={})


def touch(task, phase=None, status=None):
    patch = {}
    if phase:
        patch["phase"] = phase
    if status:
        patch["status"] = status
    save_status(task, **patch)


# ─── sessions (opencode history + imported exports) ───────────────────────

def opencode_sessions(limit=50):
    out = []
    if OPENCODE_DB.exists():
        try:
            con = sqlite3.connect(f"file:{OPENCODE_DB}?mode=ro", uri=True, timeout=5)
            con.row_factory = sqlite3.Row
            rows = con.execute(
                "SELECT id, parent_id, title, directory, time_created, time_updated "
                "FROM session ORDER BY time_updated DESC LIMIT ?",
                (limit,),
            ).fetchall()
            for r in rows:
                out.append({
                    "id": r["id"],
                    "parent_id": r["parent_id"] or "",
                    "title": r["title"] or "",
                    "directory": r["directory"] or "",
                    "source": "opencode",
                    "created_at": r["time_created"],
                    "updated_at": r["time_updated"],
                })
            con.close()
        except Exception:
            pass
    return out


def imported_sessions(limit=50):
    out = []
    try:
        files = sorted(
            SESSIONS_IMPORTED.glob("*.json"),
            key=lambda p: p.stat().st_mtime,
            reverse=True,
        )
        for f in files[:limit]:
            data = safe_json(f, default={}) or {}
            info = data.get("info") or {}
            out.append({
                "id": info.get("id") or f.stem,
                "parent_id": info.get("parentID") or "",
                "title": info.get("title") or f.stem,
                "directory": info.get("directory") or "",
                "source": "imported",
                "created_at": (info.get("time") or {}).get("created", 0),
                "updated_at": (info.get("time") or {}).get("updated", 0),
            })
    except Exception:
        pass
    return out


def export_session(session_id):
    safe = safe_id(session_id)
    if OPENCODE_DB.exists():
        try:
            con = sqlite3.connect(f"file:{OPENCODE_DB}?mode=ro", uri=True, timeout=5)
            con.row_factory = sqlite3.Row
            row = con.execute("SELECT * FROM session WHERE id = ?", (session_id,)).fetchone()
            if row:
                info = {k: row[k] for k in row.keys()}
                msgs = con.execute(
                    "SELECT * FROM message WHERE session_id = ? ORDER BY time_created, id",
                    (session_id,),
                ).fetchall()
                messages = []
                for m in msgs:
                    try:
                        minfo = json.loads(m["data"])
                    except Exception:
                        minfo = {}
                    parts = con.execute(
                        "SELECT data FROM part WHERE message_id = ? ORDER BY time_created, id",
                        (m["id"],),
                    ).fetchall()
                    plist = []
                    for p in parts:
                        try:
                            plist.append(json.loads(p["data"]))
                        except Exception:
                            pass
                    messages.append({"info": minfo, "parts": plist})
                con.close()
                return {"info": info, "messages": messages}
        except Exception:
            pass
    f = SESSIONS_IMPORTED / f"{safe}.json"
    return safe_json(f)


def import_session(data):
    if not isinstance(data, dict):
        return None
    info = data.get("info")
    if not isinstance(info, dict) or not info.get("id"):
        return None
    sid = safe_id(info["id"])
    SESSIONS_IMPORTED.mkdir(parents=True, exist_ok=True)
    write_json(SESSIONS_IMPORTED / f"{sid}.json", data)
    return {
        "id": info.get("id"),
        "title": info.get("title") or sid,
        "messages": len(data.get("messages") or []),
    }


def _extract_parts_texts(obj, acc, limit=1200):
    if isinstance(obj, dict):
        if obj.get("type") == "text" and isinstance(obj.get("text"), str):
            txt = obj["text"].strip()
            if txt:
                acc.append(txt[:limit])
        for v in obj.values():
            _extract_parts_texts(v, acc, limit)
    elif isinstance(obj, list):
        for v in obj:
            _extract_parts_texts(v, acc, limit)


def session_summary(data, max_chars=900):
    info = data.get("info") or {}
    title = info.get("title") or info.get("id") or "session"
    updated = (info.get("time") or {}).get("updated") or 0
    acc = []
    for msg in (data.get("messages") or [])[:60]:
        minfo = msg.get("info") or {}
        role = minfo.get("role") or "?"
        texts = []
        _extract_parts_texts(msg.get("parts") or [], texts, 500)
        if texts:
            acc.append(f"{role}: " + " ".join(texts))
    body = "\n".join(acc)
    if len(body) > max_chars:
        body = body[-max_chars:]
    return title, updated, body


def build_context(task, req):
    """Write the context pack the opencode agent is told to read."""
    run_id = task["id"]
    ctx = CONTEXT_ROOT / run_id
    ctx.mkdir(parents=True, exist_ok=True)

    sessions_md = [
        "# Mémoire des conversations opencode",
        "",
        "Sessions récentes du runner (et exports importés depuis le poste du propriétaire).",
        "",
    ]

    entries = []
    for s in opencode_sessions(8):
        data = export_session(s["id"])
        if data:
            entries.append(data)
    for f in sorted(SESSIONS_IMPORTED.glob("*.json"), key=lambda p: p.stat().st_mtime, reverse=True)[:8]:
        data = safe_json(f)
        if data:
            entries.append(data)

    seen = set()
    kept = 0
    for data in entries:
        info = data.get("info") or {}
        sid = info.get("id") or ""
        if sid in seen:
            continue
        seen.add(sid)
        title, updated, body = session_summary(data)
        when = time.strftime("%Y-%m-%d %H:%M", time.gmtime((updated or 0) / 1000)) if updated else "?"
        sessions_md.append(f"## {title}")
        sessions_md.append(f"- id: `{sid}` — {when} UTC")
        sessions_md.append("")
        sessions_md.append(body or "_(vide)_")
        sessions_md.append("")
        kept += 1
        if kept >= 12:
            break

    (ctx / "sessions.md").write_text("\n".join(sessions_md), encoding="utf-8")

    self_md = [
        "# Auto-compréhension — runs Dakṣiṇā précédents",
        "",
        "Ces runs sont ta propre histoire. Lis-les pour éviter de répéter une erreur,",
        "retrouver une décision, ou reprendre un travail interrompu.",
        "",
    ]
    try:
        statuses = sorted(
            RUNS.glob("*/status.json"),
            key=lambda p: p.stat().st_mtime,
            reverse=True,
        )[:15]
        for p in statuses:
            st = safe_json(p, default={}) or {}
            if not st.get("id") or st.get("id") == task.get("id"):
                continue
            self_md.append(f"## {st.get('id')} · {st.get('status')} · {st.get('phase')}")
            self_md.append(f"- mission: {st.get('instruction') or st.get('title') or '—'}")
            if st.get("test_summary"):
                self_md.append(f"- tests: {st.get('test_summary')}")
            if st.get("summary"):
                self_md.append(f"- résumé: {st.get('summary')}")
            if st.get("error"):
                self_md.append(f"- erreur: {st.get('error')}")
            if st.get("pr_url"):
                self_md.append(f"- PR: {st.get('pr_url')}")
            self_md.append("")
    except Exception:
        pass
    (ctx / "self.md").write_text("\n".join(self_md), encoding="utf-8")

    readme = [
        "# Contexte Dakṣiṇā",
        "",
        "Tu es un agent de codage autonome dans le nœud GAFAM.",
        "",
        "## Sources lisibles",
        "- `/daksina/context/<run>/gafam-history.sqlite` — snapshot SQLite des SMS, contacts, missions, runs (lecture seule).",
        "- `/daksina/context/<run>/history.md` — résumé des derniers SMS et missions.",
        "- `/daksina/context/<run>/self.md` — tes runs précédents (résultats, erreurs, PRs).",
        "- `/daksina/context/<run>/sessions.md` — conversations opencode (runner + imports).",
        "- `/daksina/context/<run>/docker-logs.txt` — logs récents des conteneurs gafam-api/opencode.",
        "- `/daksina/logs/` — logs relay / Android / ADB (JSONL, un fichier par jour).",
        "- `/daksina/gafam-data/` — arborescence live du nœud (logs, browser, sandbox, Vault) en lecture seule.",
        "- `/daksina/vault/` — Vault de recherche (notes markdown, missions passées).",
        "- `/daksina/runs/` — historique complet de tes runs (status.json, run.log, diff.patch).",
        "- `/daksina/workspace/gafam/` — le dépôt Git de travail.",
        "- GitHub complet via `gh`/`git` (GH_TOKEN/GITHUB_TOKEN).",
        "",
        "## Mission",
        task.get("instruction", ""),
        "",
    ]
    (ctx / "README.md").write_text("\n".join(readme), encoding="utf-8")
    return ctx


# ─── opencode run ─────────────────────────────────────────────────────────

def opencode_version():
    try:
        out = subprocess.check_output(
            ["opencode", "--version"], text=True, timeout=15,
            env=git_env(""),
        ).strip()
        return out.splitlines()[0] if out else "unknown"
    except Exception:
        return "unknown"


def write_opencode_config(model, repo_dir):
    OPENCODE_CONFIG.parent.mkdir(parents=True, exist_ok=True)
    cfg = {
        "$schema": "https://opencode.ai/config.json",
        "autoupdate": False,
        "permission": {
            "edit": "allow",
            "bash": "allow",
            "webfetch": "allow",
        },
    }
    if model and model.get("base_url") and model.get("api_key") and model.get("model"):
        provider_id = "gafam"
        model_id = model["model"]
        cfg["model"] = f"{provider_id}/{model_id}"
        cfg["provider"] = {
            provider_id: {
                "npm": "@ai-sdk/openai-compatible",
                "name": "GAFAM Provider",
                "options": {
                    "baseURL": model["base_url"].rstrip("/"),
                    "apiKey": model["api_key"],
                },
                "models": {model_id: {"name": model_id}},
            }
        }
    write_json(OPENCODE_CONFIG, cfg)


def build_prompt(task, context_dir):
    return f"""Tu es **Dakṣiṇā**, le kāraka codeur autonome du projet GAFAM (manifeste 27).

Dépôt de travail : `/daksina/workspace/gafam`
Contexte : `/daksina/context/{task['id']}/` (`README.md`, `sessions.md`, `history.md`, `gafam-history.sqlite`, `self.md`, logs Docker).
Logs bruts : `/daksina/logs/` et `/daksina/gafam-data/` (lecture seule).
Vault : `/daksina/vault/`.
Tes runs précédents : `/daksina/runs/*/status.json` et `run.log`.

## Accès GitHub complet
Tu as un accès GitHub complet via `gh` / `git` (GH_TOKEN et GITHUB_TOKEN sont dans l'environnement).
Tu peux lire les issues, PRs, releases, workflows, l'historique du dépôt, et écrire (branches, PRs, merges) si la mission le demande.
Le harnais fera aussi un push/PR/merge après tes tests : évite de dupliquer son travail, sauf demande explicite de l'utilisateur.

## Mission
{task['instruction']}

## Règles absolues
1. Modifie uniquement le dépôt dans `/daksina/workspace/gafam`. Jamais de secrets, télémétrie, dark pattern ou dépendance fermée nouvelle (charte de non-régression souveraine, manifeste 27).
2. Lis d'abord les logs, `history.md`, `gafam-history.sqlite`, `sessions.md` et `self.md`. Ne repose pas une question déjà tranchée. Utilise l'historique pour t'auto-comprendre et éviter les erreurs passées.
3. Garde le changement focalisé et vérifiable. Respecte le style existant (Go stdlib, Svelte 5 runes, Kotlin, manifests en français).
4. Lance toi-même `go test ./...` dans `vpc-relay/` si tu touches au Go. Si tu touches au frontend, vérifie le build.
5. Termine par un court rapport en français : ce qui a changé, pourquoi, tests lancés, risques.

Commence par inspecter le dépôt et le contexte, puis implémente la mission.
"""


def extract_texts(obj, acc):
    if isinstance(obj, dict):
        part = obj.get("part")
        if isinstance(part, dict) and part.get("type") == "text" and isinstance(part.get("text"), str):
            acc.append(part["text"])
            return
        if obj.get("type") == "text" and isinstance(obj.get("text"), str):
            acc.append(obj["text"])
            return
        for v in obj.values():
            extract_texts(v, acc)
    elif isinstance(obj, list):
        for v in obj:
            extract_texts(v, acc)


def run_opencode(task, prompt, log_path, token):
    # Full GitHub access, as requested: the agent gets the same token the
    # harness uses via GH_TOKEN/GITHUB_TOKEN, so `gh` can read issues/PRs/
    # history and write (branches, PRs, merges, releases...).
    env = git_env(token)
    env["OPENCODE_DISABLE_AUTOUPDATE"] = "1"
    env.pop("OPENCODE_SECRET", None)
    args = [
        "opencode", "run",
        "--auto",
        "--agent", "build",
        "--format", "json",
        "--title", f"Dakṣiṇā {task['id']}",
        prompt,
    ]
    append_log(log_path, f"\n$ opencode run --auto (model={task.get('model_name') or 'default'})\n")
    proc = subprocess.Popen(
        args,
        cwd=str(REPO_DIR),
        env=env,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        start_new_session=True,
        text=True,
        bufsize=1,
    )
    with ACTIVE_LOCK:
        ACTIVE[task["id"]] = {"proc": proc, "cancelled": False}

    texts = []

    def pump():
        try:
            for line in proc.stdout:
                append_log(log_path, redact(line, token))
                line = line.strip()
                if not line:
                    continue
                try:
                    ev = json.loads(line)
                    extract_texts(ev, texts)
                except Exception:
                    pass
        except Exception:
            pass

    t = threading.Thread(target=pump, daemon=True)
    t.start()
    try:
        rc = proc.wait(timeout=AGENT_TIMEOUT_S)
    except subprocess.TimeoutExpired:
        proc_group_kill(proc)
        rc = -9
        append_log(log_path, "\n[timeout] opencode killed\n")
    t.join(timeout=5)
    with ACTIVE_LOCK:
        ACTIVE.pop(task["id"], None)
    return rc, "\n".join(texts[-20:])


# ─── tests + github ───────────────────────────────────────────────────────

def changed_files(log_path, token):
    rc, out = run_cmd(["git", "status", "--porcelain"], REPO_DIR, 60, log_path, token)
    if rc != 0:
        return []
    files = []
    for line in out.splitlines():
        if len(line) < 4:
            continue
        path = line[3:].strip()
        if " -> " in path:
            path = path.split(" -> ", 1)[1]
        files.append(path)
    return files


def run_test_suite(task, log_path, token):
    files = changed_files(log_path, token)
    if not files:
        return True, "aucun changement", False
    touch(task, phase="test")
    results = []
    manual_review = False

    def go_tests():
        rc, _ = run_cmd(
            ["go", "test", "./..."], REPO_DIR / "vpc-relay", TEST_TIMEOUT_S, log_path, token
        )
        results.append(("go test ./...", rc == 0))
        if rc == 0:
            rc2, _ = run_cmd(
                ["go", "vet", "./..."], REPO_DIR / "vpc-relay", TEST_TIMEOUT_S, log_path, token
            )
            results.append(("go vet ./...", rc2 == 0))

    if any(p.startswith("vpc-relay/") for p in files):
        go_tests()

    if any(p.startswith("frontend/") for p in files):
        frontend = REPO_DIR / "frontend"
        if not (frontend / "node_modules").exists():
            run_cmd(["npm", "ci", "--no-audit", "--no-fund"], frontend, TEST_TIMEOUT_S, log_path, token)
        rc, _ = run_cmd(["npm", "run", "build"], frontend, TEST_TIMEOUT_S, log_path, token)
        results.append(("frontend npm run build", rc == 0))

    if any(p.startswith("android/") for p in files):
        # No Android SDK in the runner: propose the change but never auto-merge
        # code we could not build.
        manual_review = True
        results.append(("android build (no SDK — PR only)", True))

    rc, _ = run_cmd(["git", "diff", "--check"], REPO_DIR, 60, log_path, token)
    results.append(("git diff --check", rc == 0))

    ok = all(ok for _, ok in results)
    summary = ", ".join(f"{name}={'ok' if ok else 'FAIL'}" for name, ok in results) or "aucun test requis"
    return ok, summary, manual_review


def git_remote_url(repo):
    return f"{GIT_BASE}/{repo}.git"


def git_prepare_repo(task, repo, token, log_path):
    touch(task, phase="prepare")
    REPO_DIR.parent.mkdir(parents=True, exist_ok=True)
    if not (REPO_DIR / ".git").exists():
        if REPO_DIR.exists():
            shutil.rmtree(REPO_DIR, ignore_errors=True)
        rc, _ = run_cmd(
            ["git", "clone", "--depth=30", git_remote_url(repo), str(REPO_DIR)],
            REPO_DIR.parent, TEST_TIMEOUT_S, log_path, token,
        )
        if rc != 0:
            raise RuntimeError("git clone failed")
    run_cmd(["git", "remote", "set-url", "origin", git_remote_url(repo)],
            REPO_DIR, 60, log_path, token)
    run_cmd(["git", "config", "user.name", "Dakṣiṇā"], REPO_DIR, 60, log_path, token)
    run_cmd(["git", "config", "user.email", "daksina@gafam.cloud"], REPO_DIR, 60, log_path, token)
    rc, _ = run_cmd(["git", "fetch", "--depth=30", "origin", "main"],
                    REPO_DIR, TEST_TIMEOUT_S, log_path, token)
    if rc != 0:
        raise RuntimeError("git fetch failed")
    run_cmd(["git", "checkout", "-B", "main", "FETCH_HEAD"], REPO_DIR, 120, log_path, token)
    run_cmd(["git", "reset", "--hard", "FETCH_HEAD"], REPO_DIR, 120, log_path, token)
    run_cmd(["git", "clean", "-fd"], REPO_DIR, 120, log_path, token)


SECRET_PATTERNS = [
    re.compile(r"gh[pousr]_[A-Za-z0-9]{30,}"),
    re.compile(r"github_pat_[A-Za-z0-9_]{20,}"),
    re.compile(r"sk-[A-Za-z0-9]{20,}"),
    re.compile(r"AKIA[0-9A-Z]{16}"),
    re.compile(r"-----BEGIN [A-Z ]*PRIVATE KEY-----"),
]


def scan_diff_for_secrets(log_path, token):
    run_cmd(["git", "add", "-A"], REPO_DIR, 120, log_path, token)
    rc, diff = run_cmd(["git", "diff", "--cached", "HEAD"], REPO_DIR, 120, log_path, token)
    if rc != 0:
        return []
    hits = []
    for line in diff.splitlines():
        if line.startswith("+") and not line.startswith("+++"):
            for pat in SECRET_PATTERNS:
                if pat.search(line):
                    hits.append(line.strip()[:160])
                    break
    return hits


def git_commit_and_push(task, repo, token, log_path):
    touch(task, phase="push")
    branch = f"daksina/{task['id']}"
    run_cmd(["git", "add", "-A"], REPO_DIR, 120, log_path, token)
    msg = f"Dakṣiṇā: {task.get('title') or task['instruction'][:60]}"
    rc, _ = run_cmd(["git", "commit", "-m", msg], REPO_DIR, 120, log_path, token)
    if rc != 0:
        raise RuntimeError("git commit failed (no changes?)")
    run_cmd(["git", "checkout", "-B", branch], REPO_DIR, 120, log_path, token)
    rc, _ = run_cmd(["git", "push", "--force", "origin", branch], REPO_DIR, TEST_TIMEOUT_S, log_path, token)
    if rc != 0:
        raise RuntimeError("git push failed")
    save_status(task, branch=branch)
    return branch


def gh_pr_create(task, repo, branch, token, log_path):
    touch(task, phase="pull_request")
    title = "Dakṣiṇā: " + (task.get("title") or task["instruction"].splitlines()[0])[:90]
    body = (
        "## Mission Dakṣiṇā\n\n"
        f"{task['instruction']}\n\n"
        "## Rapport de l'agent\n\n"
        f"{task.get('agent_summary') or '_(pas de rapport)_'}\n\n"
    )
    if task.get("manual_review"):
        body += (
            "> ⚠️ Ce changement touche Android : le runner n'a pas de SDK Android, "
            "donc la PR reste ouverte pour revue/build manuel.\n\n"
        )
    body += (
        "---\n"
        f"Run: `{task['id']}` — généré et testé par le runner Dakṣiṇā "
        "(auto-merge si les tests sont verts, manifeste 27).\n"
    )
    rc, out = run_cmd(
        ["gh", "pr", "create", "--repo", repo, "--base", "main", "--head", branch,
         "--title", title, "--body", body],
        REPO_DIR, TEST_TIMEOUT_S, log_path, token,
    )
    if rc != 0:
        raise RuntimeError("gh pr create failed")
    m = re.search(r"https://github\.com/[^\s]+/pull/\d+", out)
    if not m:
        raise RuntimeError("gh pr create: URL not found")
    url = m.group(0)
    save_status(task, pr_url=url)
    return url


def gh_pr_merge(task, repo, pr_url, token, log_path):
    touch(task, phase="merge")
    # Prefer GitHub auto-merge: it waits for required checks.
    rc, out = run_cmd(
        ["gh", "pr", "merge", pr_url, "--squash", "--delete-branch", "--auto"],
        REPO_DIR, 300, log_path, token,
    )
    fallback_done = rc == 0
    deadline = time.time() + MERGE_WAIT_S
    while time.time() < deadline:
        rc2, out2 = run_cmd(
            ["gh", "pr", "view", pr_url, "--json", "state,mergeable,statusCheckRollup"],
            REPO_DIR, 120, log_path, token,
        )
        info = {}
        if rc2 == 0:
            try:
                info = json.loads(out2[out2.index("{"):])
            except Exception:
                info = {}
        state = (info.get("state") or "").upper()
        if state == "MERGED":
            return True, "merged"
        if state == "CLOSED":
            return False, "closed"
        if not fallback_done:
            # Auto-merge not enabled (or token without permission): wait for
            # checks then merge directly, once.
            fallback_done = True
            run_cmd(["gh", "pr", "checks", pr_url, "--watch", "--interval", "15"],
                    REPO_DIR, TEST_TIMEOUT_S, log_path, token)
            run_cmd(["gh", "pr", "merge", pr_url, "--squash", "--delete-branch"],
                    REPO_DIR, 300, log_path, token)
        time.sleep(15)
    return False, "merge pending / timeout"


# ─── task runner ──────────────────────────────────────────────────────────

def execute_task(task, req):
    run_id = task["id"]
    run_dir = RUNS / run_id
    run_dir.mkdir(parents=True, exist_ok=True)
    log_path = run_dir / "run.log"
    token = req.get("github_token") or ""
    repo = req.get("repo") or "Garletz/gafam"
    model = req.get("model") or {}
    task["repo"] = repo
    task["model_name"] = model.get("model") or ""
    task["title"] = (req.get("title") or task["instruction"].splitlines()[0])[:100]
    task["started_at"] = utc_ms()
    save_status(task, status="running", phase="prepare")

    try:
        touch(task, phase="prepare")
        git_prepare_repo(task, repo, token, log_path)
        context_dir = build_context(task, req)
        prompt = build_prompt(task, context_dir)
        (run_dir / "prompt.md").write_text(prompt, encoding="utf-8")
        write_opencode_config(model, REPO_DIR)
        append_log(
            log_path,
            f"\n[daksina] provider={model.get('model') or 'default'} "
            f"base_url={model.get('base_url') or '-'}\n",
        )

        for attempt in range(3):
            if is_cancelled(run_id):
                mark_cancelled(task)
                return
            touch(task, phase="agent" if attempt == 0 else f"repair_{attempt}")
            rc, agent_summary = run_opencode(task, prompt, log_path, token)
            task["agent_summary"] = agent_summary[-6000:]
            save_status(task, agent_summary=task["agent_summary"])
            if is_cancelled(run_id):
                mark_cancelled(task)
                return
            if rc != 0:
                raise RuntimeError(f"opencode exited {rc}")

            ok, test_summary, manual_review = run_test_suite(task, log_path, token)
            task["test_summary"] = test_summary
            task["manual_review"] = manual_review
            save_status(task, test_summary=test_summary, manual_review=manual_review)
            if is_cancelled(run_id):
                mark_cancelled(task)
                return
            if ok:
                break
            if attempt >= 2:
                raise RuntimeError(f"tests still failing: {test_summary}")
            prompt = f"""Les tests ont échoué après ta première passe.

Mission d'origine :
{task['instruction']}

Résultat des tests : {test_summary}

Lis la fin de `/daksina/runs/{run_id}/run.log`, corrige les erreurs, puis relance les tests.
Ne pousse rien toi-même. Termine par un court rapport.
"""

        if is_cancelled(run_id):
            mark_cancelled(task)
            return
        files = changed_files(log_path, token)
        if not files:
            save_status(task, status="done", phase="done", summary="Aucun changement nécessaire.")
            return
        run_cmd(["git", "add", "-A"], REPO_DIR, 120, log_path, token)
        (run_dir / "diff.patch").write_text(
            run_cmd(["git", "diff", "--cached", "HEAD"], REPO_DIR, 120, log_path, token)[1],
            encoding="utf-8",
        )
        if is_cancelled(run_id):
            mark_cancelled(task)
            return
        secrets = scan_diff_for_secrets(log_path, token)
        if secrets:
            raise RuntimeError(
                "diff contains what looks like a secret — refusing to push: "
                + " | ".join(secrets[:3])
            )
        branch = git_commit_and_push(task, repo, token, log_path)
        pr_url = gh_pr_create(task, repo, branch, token, log_path)

        auto_merge = bool(req.get("auto_merge", True)) and not task.get("manual_review")
        if task.get("manual_review"):
            append_log(
                log_path,
                "\n[daksina] android changes detected — auto-merge disabled (manual build required)\n",
            )
        if auto_merge:
            merged, merge_state = gh_pr_merge(task, repo, pr_url, token, log_path)
            summary = f"PR {pr_url} — {merge_state}."
            save_status(
                task,
                status="done" if merged else "pr_open",
                phase="done" if merged else "merge_pending",
                pr_url=pr_url, summary=summary,
                merged=merged,
            )
        else:
            reason = "revue manuelle requise (Android)" if task.get("manual_review") else "auto-merge désactivé"
            save_status(
                task, status="pr_open", phase="done", pr_url=pr_url,
                summary=f"PR ouverte ({reason}) : {pr_url}",
            )
    except Exception as e:
        msg = redact(str(e), token)
        save_status(task, status="failed", phase="failed", error=msg,
                    summary=f"Échec : {msg}")
        append_log(log_path, f"\n[failed] {msg}\n")
        try:
            rc, patch = run_cmd(["git", "diff", "HEAD"], REPO_DIR, 120, log_path, token)
            (run_dir / "failed.patch").write_text(patch, encoding="utf-8")
        except Exception:
            pass
    finally:
        with ACTIVE_LOCK:
            ACTIVE.pop(run_id, None)
            CANCELLED.discard(run_id)
            BUSY["id"] = ""
        prune_runs()


def start_task(task, req):
    with ACTIVE_LOCK:
        if BUSY["id"]:
            return False
        BUSY["id"] = task["id"]
    t = threading.Thread(target=execute_task, args=(task, req), daemon=True)
    t.start()
    return True


# ─── HTTP API ─────────────────────────────────────────────────────────────

class Handler(BaseHTTPRequestHandler):
    server_version = "Daksina/" + VERSION

    def log_message(self, fmt, *args):
        # Keep the container logs quiet; the action log is in /daksina/runs.
        pass

    def _send(self, status, obj):
        body = json.dumps(obj, ensure_ascii=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _auth(self):
        if not SHARED_SECRET:
            return True
        return self.headers.get("X-GAFAM-DAKSINA", "") == SHARED_SECRET

    def _body(self, limit=25 * 1024 * 1024):
        try:
            n = int(self.headers.get("Content-Length", "0"))
        except Exception:
            n = 0
        if n > limit:
            return None
        return self.rfile.read(n)

    def do_GET(self):
        if not self._auth():
            return self._send(403, {"error": "forbidden"})
        path = urlparse(self.path).path
        if path == "/health":
            with ACTIVE_LOCK:
                busy = BUSY["id"]
            return self._send(200, {
                "ok": True, "version": VERSION, "busy": busy,
                "opencode": opencode_version(),
            })
        if path == "/sessions":
            return self._send(200, {
                "sessions": opencode_sessions() + imported_sessions(),
            })
        if path.startswith("/sessions/"):
            sid = path[len("/sessions/"):].strip("/")
            data = export_session(sid)
            if not data:
                return self._send(404, {"error": "session not found"})
            return self._send(200, data)
        if path == "/tasks":
            tasks = []
            for p in sorted(RUNS.glob("*/status.json"), key=lambda x: x.stat().st_mtime, reverse=True)[:50]:
                st = safe_json(p, default={})
                if st.get("id"):
                    tasks.append(_public_status(st))
            return self._send(200, {"tasks": tasks})
        if path.startswith("/task/"):
            run_id = path[len("/task/"):].strip("/")
            if not RUN_ID_RE.match(run_id):
                return self._send(400, {"error": "invalid run id"})
            st = load_status(run_id)
            if not st:
                return self._send(404, {"error": "task not found"})
            st["log_tail"] = tail_text(RUNS / run_id / "run.log")
            return self._send(200, _public_status(st, with_log=True))
        return self._send(404, {"error": "not found"})

    def do_POST(self):
        if not self._auth():
            return self._send(403, {"error": "forbidden"})
        path = urlparse(self.path).path
        raw = self._body()
        if raw is None:
            return self._send(413, {"error": "payload too large"})

        if path == "/task":
            try:
                req = json.loads(raw.decode("utf-8"))
            except Exception:
                return self._send(400, {"error": "invalid JSON"})
            run_id = str(req.get("id") or "")
            instruction = str(req.get("instruction") or "").strip()
            repo = str(req.get("repo") or "Garletz/gafam").strip()
            if not RUN_ID_RE.match(run_id):
                return self._send(400, {"error": "invalid id"})
            if not instruction or len(instruction) > MAX_INSTRUCTION:
                return self._send(400, {"error": "instruction missing or too long"})
            if not REPO_RE.match(repo):
                return self._send(400, {"error": "invalid repo"})
            if not req.get("github_token"):
                return self._send(400, {"error": "github_token required"})
            run_dir = RUNS / run_id
            run_dir.mkdir(parents=True, exist_ok=True)
            task = {
                "id": run_id,
                "instruction": instruction,
                "status": "queued",
                "phase": "queued",
                "created_at": utc_ms(),
            }
            save_status(task)
            if not start_task(task, req):
                return self._send(409, {"error": "a Dakṣiṇā run is already active", "busy": BUSY["id"]})
            return self._send(202, {"accepted": True, "id": run_id})

        if path.startswith("/task/") and path.endswith("/cancel"):
            run_id = path[len("/task/"):-len("/cancel")].strip("/")
            with ACTIVE_LOCK:
                CANCELLED.add(run_id)
                entry = ACTIVE.get(run_id)
                if entry:
                    entry["cancelled"] = True
            if entry and entry.get("proc"):
                proc_group_kill(entry["proc"])
            st = load_status(run_id)
            if st:
                save_status(st, status="cancelled", phase="cancelled",
                            summary="Annulé par l'utilisateur.")
            return self._send(200, {"cancelled": bool(entry)})

        if path == "/sessions/import":
            try:
                data = json.loads(raw.decode("utf-8"))
            except Exception:
                return self._send(400, {"error": "invalid JSON"})
            items = data if isinstance(data, list) else [data]
            imported = []
            for item in items[:20]:
                out = import_session(item)
                if out:
                    imported.append(out)
            return self._send(200, {"imported": imported})

        return self._send(404, {"error": "not found"})


def _public_status(st, with_log=False):
    out = {
        "id": st.get("id"),
        "status": st.get("status"),
        "phase": st.get("phase"),
        "instruction": st.get("instruction"),
        "title": st.get("title"),
        "repo": st.get("repo"),
        "branch": st.get("branch"),
        "pr_url": st.get("pr_url"),
        "summary": st.get("summary"),
        "error": st.get("error"),
        "test_summary": st.get("test_summary"),
        "agent_summary": st.get("agent_summary"),
        "merged": st.get("merged", False),
        "manual_review": st.get("manual_review", False),
        "created_at": st.get("created_at"),
        "updated_at": st.get("updated_at"),
    }
    if with_log:
        out["log_tail"] = st.get("log_tail", "")
    return out


def main():
    ensure_layout()
    print(f"daksina: runner ready on :{HTTP_PORT} (opencode {opencode_version()})", flush=True)
    server = ThreadingHTTPServer(("0.0.0.0", HTTP_PORT), Handler)
    server.serve_forever()


if __name__ == "__main__":
    main()
