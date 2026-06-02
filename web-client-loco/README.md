# GAFAM Backend — Loco Rust API Node (Personal VPC)

The GAFAM backend is built using the **[Loco Rust Framework](https://loco.rs/)** (powered by Axum, Tokio, and SeaORM). 

In the new architecture, this backend is deployed inside an **Ubuntu Docker container (or Cloudron)** as part of the user's personal VPC. It acts as the buffer relay and SQL server for the user's personal page, securely receiving and storing SMS and contacts sent from the Android relay. It can be paired with an Outline VPN relay for maximum security.

## 🛠️ Installed APIs & Controllers

All core VPC-orchestration and authentication delegation controllers are declared in `src/controllers/gafam.rs` and mapped onto the Axum server:

| Endpoint | Method | Payload | Response | Description |
| :--- | :--- | :--- | :--- | :--- |
| `/api/gafam/trust-device` | `POST` | `TrustDeviceParams` | `TrustDeviceResponse` | Validates device hardware signatures, IPs, and cryptographic user fingerprints (RankMyAura-inspired). |
| `/api/gafam/reserve-vpc` | `POST` | `ReserveVpcParams` | `ReserveVpcResponse` | Provisions a virtual mini-VPC container. |
| `/api/gafam/vpc-status` | `GET` | *None* | `VpcStatusResponse` | Streams simulated network logs, SMTP status, and eSIM emulator activity. |
| `/api/gafam/request-delegation-token` | `POST` | `DelegationParams` | `DelegationResponse` | Triggers a P2P authentication delegation token to be relayed to a trusted partner node. |

---

## 📋 Features & Roadmap (Backlog)

While the local POC simulates backend virtualization nodes, the full system architecture roadmap includes:

### 🟩 Phase 1: Local POC (Completed)
*   [x] Set up Loco Rust environment on SQLite storage.
*   [x] Configured CORS middleware for local frontend cross-origin requests.
*   [x] Implemented cryptographic device fingerprint validation models.
*   [x] Mocked P2P delegation token generation relays.

### 🟨 Phase 2: Dockerized VPC Orchestration (Planned / Next Steps)
*   [ ] **Ubuntu Docker / Cloudron Integration:** Package the backend and SQLite into a deployable Docker container.
*   [ ] **Outline VPN Relay:** Configure an Outline server alongside the Loco API to encrypt all incoming traffic from the Android relay and the frontend client.
*   [ ] **SMTP/SMS Listener Daemon:** Build a listener daemon using Axum WebSockets to parse incoming SMS verification codes directly from the Android relay and route them to the client in real time.

---

## 🚀 Execution & CLI Commands

Start the development database and API server:

```bash
# Verify system compilation
cargo check

# Start the Loco Axum server
cargo run -- start
```
*Loco server will boot in development mode on `http://localhost:5150`*
