# GAFAM — Architecture Guide

> Accurate as of 2026-07-19. Read this first before touching any code.

## What is GAFAM?

A self-hosted sovereign personal node (VPC) running on a $6/mo VPS. Your Android phone relays SMS to it, you control everything from a web dashboard, and agents live inside it. Federation allows multiple nodes to publish/scan each other's feeds.

**Core flow:**

1. Desktop Manager (Tauri) creates a VPC on DigitalOcean in 1 click
2. VPC runs the Go relay binary inside Docker (ports 5150/5151)
3. Android APK forwards every incoming SMS via AES-GCM + cert pinning
4. Web dashboard (Cloudflare + SvelteKit) connects via TCP socket proxy
5. VPC nodes federate via publish/scan (Poneglyph model)

## Project Map

```
GAFAM/
├── vpc-relay/         ← Go API server (the VPC brain)
│   ├── main.go        ← Entry point, ~60 routes, initDB
│   ├── api.go         ← Crypto, handlers, sessionMiddleware
│   ├── feed.go        ← VPC↔VPC federation (links, envelopes, inbox, circles)
│   ├── llm.go         ← LLM provider router (OpenAI-compat, Qwen local)
│   ├── orchestrator.go← Saṃyojaka agent loop (plan → execute → repair → synthesize)
│   ├── action_tools.go← Organic action tools (sms.send, feed.publish, vault.remember…)
│   ├── cron.go        ← Scheduled missions (SQLite table + 60s ticker)
│   ├── scrcpy_hub.go  ← Remote phone control (H.264 + ADB shell)
│   ├── research.go    ← The Vault (markdown notes + FTS5 search)
│   ├── browser/       ← Vātāyana: Firefox sidecar control
│   ├── sandbox/       ← Yantraśālā: terminal + file sandbox
│   ├── edge/          ← L2 inference on the phone (ONNX Qwen3)
│   ├── suparna/       ← L1 log analyst (Qwen3 GGUF via llama.cpp)
│   ├── karaka/        ← Tool registry + permissions
│   ├── moksa/         ← Quest board (pose, claim, run, reward, synthesize)
│   ├── scripts/       ← qwen-install.sh, qwen-ctl.sh, etc.
│   ├── Dockerfile     ← Multi-stage: golang:alpine → debian:bookworm-slim
│   ├── Dockerfile.browser  ← Firefox ESR + Xvfb + MJPEG streamer
│   └── Dockerfile.sandbox  ← Alpine + bash/python/sqlite/jq/tmux
│
├── frontend/          ← Web dashboard (SvelteKit 5 + Cloudflare Workers)
│   ├── src/routes/[phone]/+page.svelte  ← Main dashboard (1870 lines)
│   ├── src/routes/+page.svelte          ← Landing (phone input)
│   ├── src/routes/api/proxy/            ← TCP socket proxy routes
│   ├── src/lib/*                        ← Components + helpers
│   └── wrangler.jsonc                   ← Deploy config (gafam.cloud + D1)
│
├── frontend-old/      ← Legacy 3D deck POC (archived)
│
├── android/           ← Kotlin APK: SMS relay + edge inference
│   └── app/src/../relay/
│       ├── SmsReceiver.kt        ← AES-GCM SMS forwarder (cert-pinned)
│       ├── SmsDeliverReceiver.kt ← Default SMS handler (unified)
│       ├── ApiClient.kt          ← OkHttp + SHA-256 cert pinning
│       ├── RelayForegroundService.kt ← Outbox poller + edge sync
│       ├── EdgeInferenceService.kt   ← ONNX Qwen3 on-device
│       └── ...
│
├── gafam-manager/     ← Tauri v2 desktop (macOS)
│   ├── src/routes/+page.svelte  ← UI: DO OAuth + manual deploy
│   ├── src/routes/tray/         ← Tray window (QR + ADB status)
│   └── src-tauri/src/
│       ├── lib.rs               ← DO droplet creation + cert gen
│       └── scrcpy_bridge.rs     ← ADB↔VPC bridge (H.264 + touch)
│
├── manifest/          ← 27 architecture specs (the nervous system)
│   ├── README.md              ← Index + layer map
│   ├── 9b_tcp_socket_tls_solution.md  ← Cloudflare proxy design
│   ├── 12_synchronous_mechanical_rendezvous.md ← Auth challenge
│   ├── 17_poneglyph_conjugation_channel.md    ← Federation (3 parts)
│   ├── 20-27_*.md             ← Sanskrit agent layer (Suparna→Dakṣiṇā)
│   └── moksa/                 ← Method specs for agent orchestration
│
├── ghostd/            ← [WIP] Go daemon (ADB log stream → LLM, skeleton)
├── device/            ← Concept: hardware relay (ESP32 + eSIM)
├── META-INF/          ← Residual AAR metadata (candidate for removal)
│
├── deploy-vpc.sh      ← One-liner: Docker + sidecars + swap + Watchtower
└── .github/workflows/
    └── docker-publish.yml ← CI: build api/browser/sandbox → GHCR
```

---

## Components

### 1. `vpc-relay/` — Go API Server

| | |
|---|---|
| **Tech** | Go 1.26, stdlib `net/http`, `modernc.org/sqlite` |
| **Size** | ~11,750 lines (9,100 prod + 650 tests + 1,060 Python sidecars) |
| **Entry** | `main.go` |
| **Run** | `go run .` (port 5150), TLS optional on 5151 |
| **Docker** | `ghcr.io/garletz/gafam:latest` |

**Key routes (non-exhaustive):**

| Group | Endpoint | Purpose |
|---|---|---|
| **Public** | `GET /api/_ping` | Health check |
| **Public** | `GET /feed` | VPC↔VPC federation feed |
| **APK** | `POST /api/auth/sms/` | Encrypted SMS receive |
| **APK** | `GET /api/auth/sms/outbox` | Outbox polling |
| **APK** | `POST /api/auth/edge/sync` | L2 inference sync |
| **Web** | `GET /api/web/sms` | SMS inbox |
| **Web** | `GET /api/web/logs` | Log viewer |
| **Web** | `GET /api/web/llm/providers` | LLM provider CRUD |
| **Web** | `POST /api/web/llm/chat` | Single LLM entry point |
| **Web** | `POST /api/web/orchestrator/run` | Saṃyojaka mission runner |
| **Web** | `GET/POST/DELETE /api/web/cron` | Scheduled missions (≥5 min interval) |
| **Web** | `POST …/quest/{qid}/approve` | Human approve/reject of `ask` quests |
| **Web** | `GET/POST /api/web/links` | Federation: link CRUD + scan |
| **Web** | `GET /api/web/inbox` | Federation: inbox |
| **Web** | `GET/POST /api/web/feed/publish` | Federation: publish |
| **Web** | `*/browser/*`, `*/sandbox/*` | Sidecar proxy |
| **Web** | `GET /ws/scrcpy/bridge` | Remote phone control bridge |

**Sidecars (wake-on-demand, all stopped by default):**

| Container | RAM | Purpose |
|---|---|---|
| `gafam-browser` (Vātāyana) | 600 MB | Firefox ESR + Xvfb + MJPEG/noVNC |
| `gafam-sandbox` (Yantraśālā) | 384 MB | Terminal + file API + bash sessions + Python/Node runtimes (agents pip/npm-install packages on demand into the persistent `/sandbox/files` volume) |
| `gafam-qwen` | 520 MB | llama.cpp server (Qwen3-0.6B GGUF) |

**LLM engine tiers:**

| Tier | Model | Location |
|---|---|---|
| L1 | Qwen3-0.6B GGUF | VPC (Suparna analyst) |
| L2 | Qwen3-0.6B ONNX INT4 | Phone (EdgeInferenceService) |
| L3 | DeepSeek V4 / Kimi K3 | Cloud provider (orchestration) |

**Agent layer behavior (Saṃyojaka, Manifests 25/26):**
- Loop: PLAN (LLM → quest DAG) → EXECUTE (parallel levels, `{{qN.result.field}}` interpolation) → OBSERVE → REPLAN (max 3) → SYNTHESIZE (report → sandbox + vault FTS5).
- **Agentic repair:** a failed quest is retried up to 2× with LLM-corrected params (error fed back to `light_task` scope) — the quest-level agent↔tool↔observation loop.
- **Permissions are real:** `deny` blocks; with `require_approval`, `ask` tools park the quest as `waiting_approval` (dependents wait, no false "cycle" cancel) until `POST …/quest/{qid}/approve`. Auto-rewards (`done`/`failed`) are written to Mokṣa for trajectory filtering.
- **Action tools:** `sms.send` (ask), `sms.history`, `contacts.search`, `feed.publish` (ask), `vault.remember` — agents can speak, read context and persist long-term memories.
- **Self-made tools:** agents write `.sh`/`.py` scripts into the sandbox `/files/tools/` (with a `# desc:` header) → auto-registered as `custom.*` tools, re-scanned before every planning round. JSON in on stdin, JSON out on stdout. This is the safe self-improvement level: capabilities grow, the Go binary is never touched.
- **Memory:** planner prompt injects recent vault notes + FTS5 search of past missions/research relevant to the instruction.
- **Cron:** `gafam_cron` jobs fire missions on interval (≥5 min), optional SMS report to the self phone (`notify_phone: "self"`). Managed from the Quests tab.
- Triggers: dashboard, SMS `/q` `/r` from the self phone, cron, sub-agents (`karaka.delegate`).

---

### 2. `frontend/` — Web Dashboard

| | |
|---|---|
| **Tech** | SvelteKit 5 + Svelte 5 runes + TypeScript |
| **Deploy** | Cloudflare Workers (`gafam.cloud` + `*.gafam.cloud`) |
| **Database** | Cloudflare D1 (`gafam-directory`) for safe deposits |
| **Build** | `npm run build && npx wrangler deploy` |

**Key components:**

| Component | Tab | Purpose |
|---|---|---|
| `+page.svelte` (main) | Chats, Contacts | SMS conversations + contact list |
| `Settings` | Settings | Node, recovery guardians, contacts sync |
| `QuestBoard` | Quests | Mokṣa mission board |
| `SuparnaPanel` | Suparna | Edge & VPC AI status |
| `BrowserView` | Browser | Vātāyana (remote Firefox via noVNC) |
| `SandboxView` | Sandbox | Yantraśālā (terminal + files) |
| `VaultView` | Vault | Research memory search |
| `FederationView` | Federation | Links, Inbox, Circles |
| `RemoteControl` | /[phone]/remote | Screen mirroring (scrcpy overlay) |

**Proxy architecture:** All API calls go through SvelteKit server routes that open raw TCP sockets to the VPC (bypassing Cloudflare Error 1003 on raw IPs). Supports `X-GAFAM-E2E: 1` for AES-256-GCM encryption.

---

### 3. `android/` — SMS Relay Agent

| | |
|---|---|
| **Tech** | Kotlin, SDK 34, OkHttp, ONNX Runtime GenAI |
| **Build** | `./gradlew assembleDebug` |
| **APK** | ~38 MB (dominated by ONNX native libs) |

**Key capabilities:**
- AES-256-GCM encrypted SMS relay to VPC
- SHA-256 certificate pinning + DNS spoofing (`wikipedia.org` → VPC IP)
- Outbox polling (send SMS from web)
- SMS history sync (last 400 messages)
- Contact sync
- Log shipping (logcat → VPC, batch every 5s)
- Edge inference: Qwen3-0.6B ONNX INT4, wake/stop/infer from VPC
- Challenge-based web login (time + click count)
- Self-phone SMS triggers for Saṃyojaka (`/q`, `/r`)
- Recovery SMS forwarding to guardians

---

### 4. `gafam-manager/` — Desktop App

| | |
|---|---|
| **Tech** | Tauri v2 (Rust) + SvelteKit 5 + TypeScript |
| **Run** | `npm run tauri dev` |
| **Build** | `npm run tauri build` → `.dmg` |

**Key flows:**
- **VPC creation:** OAuth DigitalOcean → create droplet → cloud-init `deploy-vpc.sh`
- **Crypto setup:** Generate JWT + self-signed cert (SAN `wikipedia.org`) + SHA-256 fingerprint
- **Pairing:** Display QR code → scanned by Android APK
- **ADB bridge:** scrcpy H.264 video + touch injection → WebSocket to VPC
- **Tray:** Persistent icon with QR + ADB status + quick controls

---

### 5. DevOps / CI/CD

| File | Purpose |
|---|---|
| `.github/workflows/docker-publish.yml` | Push to `main` (vpc-relay changes) → build 3 images → push to GHCR |
| `deploy-vpc.sh` | One-liner: install Docker, swap 4G, pull images, start API + Watchtower + sidecars |
| `docker-compose.{browser,sandbox,qwen}.yml` | Sidecar definitions (stopped by default, woke on demand) |

**Docker images:** `ghcr.io/garletz/gafam:{latest,browser,sandbox}`

**Auto-update:** Watchtower polls GHCR every 5 min → auto-restart `gafam-api` + sidecars

---

## Data Flow

```
┌──────────────┐   AES-GCM HTTPS:5151    ┌──────────────────┐
│ Android APK   │ ──────────────────────→ │  Go relay (VPC)   │
│ cert-pinned   │   SMS + logs + edge sync │  port 5150/5151   │
└──────┬───────┘                          └────────┬─────────┘
       │ ADB H.264                                 │
┌──────▼───────┐   WSS:5150              ┌────────▼─────────┐
│ Tauri Manager │ ──────────────────────→ │  scrcpy hub      │
│ scrcpy bridge │   video + touch + shell │  SQLite WAL      │
└──────────────┘                          └────────┬─────────┘
                                                   │ TCP socket
┌──────────────┐   HTTP + AES-GCM        ┌────────▼─────────┐
│ Cloudflare    │ ←────────────────────── │  Web dashboard   │
│ Worker proxy  │   (cloudflare:sockets)  │  SvelteKit 5     │
└──────┬───────┘                          └──────────────────┘
       │                                         │ GET /feed
┌──────▼───────┐                          ┌──────▼──────────┐
│ gafam.cloud   │   D1 directory          │  Other GAFAM     │
│ (directory)  │   safe deposits           │  VPC nodes       │
└──────────────┘                          └──────────────────┘
```

---

## Tech Stack Summary

| Layer | Technology |
|---|---|
| Backend | Go 1.26 + `net/http` + `modernc.org/sqlite` |
| Desktop | Tauri v2 + Rust + SvelteKit 5 |
| Android | Kotlin + OkHttp + ONNX Runtime GenAI |
| Frontend | SvelteKit 5 + Svelte 5 runes + Cloudflare Workers |
| Frontend DB | Cloudflare D1 |
| Database | SQLite (WAL mode, FTS5) |
| Container | Docker (multi-stage, 3 images) |
| CI/CD | GitHub Actions → GHCR + Watchtower |
| Sidecars | Firefox ESR / Alpine / llama.cpp |
| LLM (L1) | Qwen3-0.6B GGUF (llama.cpp) |
| LLM (L2) | Qwen3-0.6B ONNX INT4 (onnxruntime-genai) |
| LLM (L3) | DeepSeek V4 / Kimi K3 (OpenAI-compatible API) |

---

## Current Status (2026-07-19)

- **Backend:** Compiles, ~60 routes functional, federated, tests passing. Docker image on GHCR.
- **Frontend:** Deployed on gafam.cloud with all Organic Tools tabs.
- **Android:** Compiled, installed on device, SMS + edge inference operational.
- **Desktop Manager:** Runs on macOS, provisions VPCs in 1 click.
- **CI/CD:** Pushes to main auto-build 3 images → Watchtower auto-updates VPS.
- **Federation:** VPC↔VPC publish/scan implemented (links, envelopes, inbox, circles).
- **Security:** AES-256-GCM E2E (Web↔VPC + APK↔VPC), Ed25519 node keypairs.
