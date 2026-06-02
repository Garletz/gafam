# GAFAM — Architecture Guide

> Quick reference for any developer or agent working on this project.
> Read this first before touching any code.

## What is GAFAM?

A self-hosted Personal VPC (Virtual Private Cloud) system. The user deploys their own private server on a cloud provider, their Android phone silently relays incoming SMS to it, and they control everything from a desktop app or web dashboard.

**Core flow:**
1. User launches the Desktop Manager → creates a VPC on DigitalOcean (or manually)
2. The VPC runs a Rust API server inside Docker
3. User installs the Android relay on their phone → it silently forwards every incoming SMS to the VPC
4. User opens the Web Frontend from any browser → reads SMS, contacts, notes stored on their personal VPC

## Project Map

```
GAFAM/
├── backend/          ← Rust API server (the VPC brain)
├── gafam-manager/    ← Desktop app for macOS (creates the VPC)
├── android/          ← Android SMS relay agent
├── frontend/         ← Web dashboard (Svelte)
├── deploy-vpc.sh     ← One-liner deploy script for any VPS
├── docker-compose.yml← Local dev setup
├── .github/workflows/← CI/CD (Docker image build + push to GHCR)
├── outline-server/   ← [Phase 4] Outline VPN server (not started)
└── outline-apps/     ← [Phase 4] Outline VPN client (not started)
```

---

## Components

### 1. `backend/` — Rust API Server

| | |
|---|---|
| **Tech** | Rust, Loco framework, SeaORM, SQLite |
| **Entry point** | `src/bin/main.rs` |
| **Config** | `config/development.yaml`, `config/production.yaml` |
| **Run locally** | `cargo loco start` (port 5150) |
| **Build Docker** | `docker build -t gafam ./backend` |

**Key files:**
- `src/controllers/gafam.rs` — Main API: trust-device, reserve-vpc, contacts, notes
- `src/controllers/sms.rs` — SMS receive + list (SeaORM, single source of truth)
- `src/controllers/auth.rs` — User auth (Loco default)
- `src/models/_entities/sms.rs` — SMS database entity (auto-generated)
- `migration/` — Database migrations (SeaORM)
- `Dockerfile` — Multi-stage build: `rust:latest` → `debian:bookworm-slim`
- `.dockerignore` — Excludes `target/` from Docker context

**API Routes:**

| Route | Method | What it does |
|---|---|---|
| `POST /api/sms/` | POST | Receive an SMS (from Android relay) |
| `GET /api/sms/` | GET | List all stored SMS |
| `POST /api/gafam/trust-device` | POST | Device trust handshake (mock) |
| `POST /api/gafam/reserve-vpc` | POST | Reserve a VPC (mock) |
| `GET /api/gafam/vpc-status` | GET | VPC health check (mock) |
| `GET /api/gafam/contacts` | GET | List contacts |
| `POST /api/gafam/contacts` | POST | Create a contact |
| `GET /api/gafam/notes` | GET | Read personal note |
| `POST /api/gafam/notes` | POST | Save personal note |

> Routes marked "mock" return hardcoded/simulated data. Real implementation pending.

---

### 2. `gafam-manager/` — Desktop App (macOS)

| | |
|---|---|
| **Tech** | Tauri v2 (Rust) + SvelteKit (TypeScript) |
| **Run** | `cd gafam-manager && npm run tauri dev` |
| **Build** | `npm run tauri build` (produces `.dmg`) |

**Key files:**
- `src/routes/+page.svelte` — Main UI (cloud provider selection, deploy flow)
- `src/routes/+layout.svelte` — Layout with CSS import
- `src/app.css` — Styling (white minimalist theme)
- `src/i18n.ts` + `src/locales/` — i18n setup (English, translation-ready)
- `src-tauri/src/lib.rs` — Rust backend commands (OAuth simulation, crypto)
- `src-tauri/Cargo.toml` — Rust dependencies

**What the UI does:**
1. Shows two cards: "DigitalOcean" (auto) or "Advanced" (manual)
2. DigitalOcean path → calls `start_do_oauth` Rust command → simulated OAuth
3. Advanced path → shows `curl` deploy script + JSON config paste area

---

### 3. `android/` — SMS Relay Agent

| | |
|---|---|
| **Tech** | Kotlin, Android SDK 34, Gradle |
| **Build** | `cd android && ./gradlew assembleDebug` |
| **APK output** | `app/build/outputs/apk/debug/` |

**Key files:**
- `app/src/main/java/com/gafam/relay/SmsReceiver.kt` — BroadcastReceiver that intercepts every SMS and POSTs it to the VPC as JSON
- `app/src/main/java/com/gafam/relay/MainActivity.kt` — Requests RECEIVE_SMS permission on launch
- `app/src/main/AndroidManifest.xml` — Permissions (RECEIVE_SMS, INTERNET) + receiver registration

**Important:** The VPC URL is currently hardcoded to `http://10.0.2.2:5150/api/sms` (Android emulator localhost). This needs to be made configurable before real use.

---

### 4. `frontend/` — Web Dashboard

| | |
|---|---|
| **Tech** | Svelte 4 (Vite), TypeScript |
| **Run** | `cd frontend && npm run dev` |
| **Main file** | `src/App.svelte` (29KB, substantial) |

This is a web-based dashboard that connects to the VPC API to display SMS, contacts, and notes. Pre-existing code, not yet integrated with the live VPC flow.

---

### 5. DevOps / CI/CD

| File | What it does |
|---|---|
| `.github/workflows/docker-publish.yml` | On push to `main` (backend changes): builds Docker image → pushes to `ghcr.io/garletz/gafam:latest` |
| `deploy-vpc.sh` | Bash script to run on any VPS: installs Docker, pulls GHCR image, runs the API on port 5150 |
| `docker-compose.yml` | Local dev: builds backend, mounts SQLite volume, exposes port 5150 |

**GitHub secrets required:**
- `CR_PAT` — GitHub Personal Access Token with `write:packages` scope (for pushing Docker images)

**Docker image:** `ghcr.io/garletz/gafam:latest`

---

## Data Flow

```
┌─────────────────┐     SMS arrives      ┌──────────────────┐
│  Android Phone   │ ──────────────────→ │   SmsReceiver.kt  │
│  (physical)      │                      │   (BroadcastRcvr) │
└─────────────────┘                      └────────┬─────────┘
                                                   │ POST /api/sms/
                                                   ▼
┌─────────────────┐     GET /api/*       ┌──────────────────┐
│  Web Frontend    │ ←─────────────────→ │   Rust Loco API   │
│  (any browser)   │                      │   (Docker on VPC) │
└─────────────────┘                      └────────┬─────────┘
                                                   │ SQLite
┌─────────────────┐     OAuth / Script   ┌────────▼─────────┐
│  Desktop Manager │ ──────────────────→ │   DigitalOcean     │
│  (Tauri macOS)   │                      │   (creates VPC)    │
└─────────────────┘                      └──────────────────┘
```

---

## Tech Stack Summary

| Layer | Technology |
|---|---|
| Backend API | Rust + Loco + SeaORM + SQLite |
| Desktop App | Tauri v2 + SvelteKit + TypeScript |
| Android App | Kotlin + Android SDK 34 |
| Web Frontend | Svelte 4 + Vite + TypeScript |
| Database | SQLite (embedded in backend) |
| Container | Docker (multi-stage build) |
| CI/CD | GitHub Actions → GHCR |
| VPN (planned) | Outline Server/Client |

---

## Current Status

- **Backend:** Compiles, runs locally, API routes functional. Docker build being fixed on CI.
- **Desktop Manager:** Runs on macOS via `npm run tauri dev`. OAuth is simulated.
- **Android Relay:** Code written, not yet compiled/tested on device.
- **Web Frontend:** Pre-existing code, not yet wired to live VPC.
- **CI/CD:** GitHub Actions pipeline active, Docker image publishing in progress.
- **VPN (Phase 4):** Outline submodules present, not started.
