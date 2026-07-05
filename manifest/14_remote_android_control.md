# Manifest 14 — Remote Android Control via Scrcpy Bridge

## Problème

Contrôler totalement son Android à distance, depuis n'importe quel navigateur web, via le VPS GAFAM personnel.

## Solution

Le **GAFAM Manager (Tauri)** sert de pont ADB entre l'Android connecté localement et le VPS distant. Le flux vidéo H.264 de scrcpy est relayé via WebSocket au VPS, qui le redistribue aux navigateurs web. Les inputs (touch, clavier, shell ADB) suivent le chemin inverse.

## Architecture

```
Android ←USB/WiFi ADB→ GAFAM Manager (Tauri) ←WSS→ VPS (Go) ←WS→ Web Client
```

### Composants

| Composant | Fichier | Rôle |
|---|---|---|
| **Scrcpy Bridge** | `gafam-manager/src-tauri/src/scrcpy_bridge.rs` | ADB management, scrcpy-server launch, H.264 capture, WebSocket bridge |
| **Tauri Commands** | `gafam-manager/src-tauri/src/lib.rs` | 5 nouvelles commandes Tauri pour le bridge |
| **Manager UI** | `gafam-manager/src/routes/+page.svelte` | Section "Remote Control" dans la vue paired |
| **WS Hub** | `vpc-relay/scrcpy_hub.go` | WebSocket hub (1 bridge + N viewers) |
| **WS Routes** | `vpc-relay/main.go` | 4 routes ajoutées |
| **Remote View** | `frontend/src/routes/[phone]/remote/+page.svelte` | Vue Remote Control |
| **Screen Mirror** | `frontend/src/lib/RemoteControl.svelte` | H.264 decode via WebCodecs + Canvas |
| **ADB Terminal** | `frontend/src/lib/AdbTerminal.svelte` | Terminal ADB distant |
| **Status Proxy** | `frontend/src/routes/api/proxy/scrcpy-status/+server.ts` | Proxy Cloudflare TCP Socket |

## Protocole WebSocket

### Routes

| Route | Auth | Direction | Usage |
|---|---|---|---|
| `GET /ws/scrcpy/bridge` | JWT_SECRET (Bearer) | Bridge → VPS | Le Manager se connecte ici |
| `GET /ws/scrcpy/view` | session_token | VPS → Viewer | Les navigateurs se connectent ici |
| `GET /ws/scrcpy/shell` | session_token | Bidirectionnel | Terminal ADB |
| `GET /api/scrcpy/status` | session_token | VPS → Client | État du hub |

### Types de messages (binaire WebSocket)

| Byte | Type | Contenu | Direction |
|---|---|---|---|
| `0x01` | Video | H.264 NAL unit | Bridge → VPS → Viewers |
| `0x02` | Input | JSON event | Viewer → VPS → Bridge |
| `0x03` | DeviceInfo | JSON metadata | Bridge → VPS → Viewers |
| `0x04` | Shell | UTF-8 text | Bidirectionnel |
| `0x05` | Heartbeat | Keepalive | Bridge → VPS |

### Format Input Events

```json
{"type": "touch", "action": "down|move|up", "x": 540, "y": 1200}
{"type": "key", "action": "down|up|press", "keycode": 66}
{"type": "scroll", "x": 540, "y": 1200, "dx": 0, "dy": -120}
```

## Sécurité

- **Bridge → VPS** : JWT_SECRET (même token que l'APK Android)
- **Viewer → VPS** : session_token (même mécanisme que le Manifest 12, rendez-vous synchrone)
- **Transit** : WSS (TLS) pour le bridge, TCP Socket Cloudflare pour les viewers
- **1 bridge max** : Le VPS refuse un 2e bridge tant que le 1er est connecté
- **Input control** : Seul le 1er viewer connecté peut envoyer des inputs, les autres sont spectateurs
- **Shell ADB** : Désactivé par défaut, activable via `gafam_settings` table

## Flux de démarrage

1. L'utilisateur ouvre GAFAM Manager → sélectionne un VPC
2. Branche son Android en USB (ou entre l'IP WiFi ADB)
3. Clique "Scan ADB Devices" → le Manager liste les devices
4. Sélectionne son device → clique "Start Bridge"
5. Le Manager push `scrcpy-server.jar`, ouvre le tunnel, capture H.264
6. Le Manager ouvre un WebSocket vers le VPS (`/ws/scrcpy/bridge`)
7. Le VPS reçoit le flux et attend les viewers
8. L'utilisateur ouvre gafam.cloud → onglet 📱 → page Remote Control
9. Le navigateur se connecte via WebSocket (`/ws/scrcpy/view`)
10. Le flux H.264 est décodé via WebCodecs API → rendu Canvas
11. Les inputs souris/clavier sont capturés et envoyés au VPS → bridge → Android

## Dépendances ajoutées

### VPS (Go)
- `github.com/gorilla/websocket` — WebSocket server

### GAFAM Manager (Rust)
- `tokio-tungstenite` — WebSocket client async
- `futures-util` — Stream utilities
- `log` — Logging
- `http` — HTTP request builder

### Frontend
- Aucune dépendance npm ajoutée (WebCodecs est natif au navigateur)
