# 24. Yantraśālā — यन्त्रशाला · l'établi du VPC

> **Statut :** manifeste de conception.
> **Prérequis :** [Vātāyana](22_vatayana_remote_browser.md) (browser distant), Docker sidecar pattern.
> **Lien vision :** le premier pilier du [Nœud Personnel](19_personal_node.md) — la capacité d'exécuter (`/intents`).
> **Suite :** [Saṃyojaka — l'orchestre](25_samyojaka_agent_orchestrator.md) qui connectera les agents à cet atelier.

---

## Pourquoi « Yantraśālā »

**Yantraśālā** (यन्त्रशाला, *yantra* « machine, instrument » + *śālā* « salle, atelier ») désigne en sanskrit l'atelier, le laboratoire, la salle des machines — l'endroit où les instruments sont rangés et utilisés.

Le VPC a déjà des organes : Suparna lit les logs, Vātāyana voit le web, Edge L2 pense, Scrcpy touche. Mais ces organes existent en silos. Le VPC n'a pas d'**atelier** — pas d'endroit où un humain (ou une future luciole) peut venir, prendre un outil, exécuter une commande, poser un fichier.

> *Yantraśālā est l'établi du VPC. C'est là qu'on travaille. Terminal, fichiers, stockage — les mains du nœud.*

---

## Le problème en une phrase

> **Le VPC a des organes mais pas de mains. Il expose des API mais pas d'atelier. Yantraśālā donne au VPC un terminal, un filesystem, et un tableau de bord de stockage — l'établi qui manquait.**

---

## Ce que le VPC peut faire aujourd'hui — et ce qu'il ne peut pas faire

| Le VPC peut... | Mais il ne peut pas... |
|---|---|
| Lire les SMS (via APK relay) | Exécuter une commande shell sur lui-même |
| Afficher un Firefox distant (Vātāyana) | Télécharger un fichier depuis ce Firefox et le stocker |
| Analyser les logs (Suparna) | Traiter un fichier JSON avec `jq` |
| Gérer des conteneurs Docker (Qwen, Browser) | Gérer ses propres fichiers, scripts, données temporaires |
| Exposer des API REST | Offrir un terminal interactif à l'utilisateur |

**Le gap :** le VPC est un serveur qui répond à des requêtes. Il n'est pas un **environnement de travail**. Yantraśālā le transforme en atelier.

---

## Architecture

```
┌─────────────────────────────────────────────┐
│              YANTRAŚĀLĀ (l'atelier)           │
│                                               │
│  gafam-sandbox (Alpine, 128 Mo, stoppé)       │
│  ┌─────────────────────────────────────────┐  │
│  │  /sandbox/                               │  │
│  │  ├── files/       persistant, volumes    │  │
│  │  ├── tmp/         tmpfs, 64 Mo           │  │
│  │  ├── downloads/   depuis Vātāyana         │  │
│  │  ├── screenshots/ captures browser        │  │
│  │  └── scripts/     scripts utilisateur     │  │
│  │                                          │  │
│  │  Terminal : bash -i  (WebSocket :6090)    │  │
│  │  File API : CRUD   (HTTP :6091)           │  │
│  │  Exec API : POST /exec                    │  │
│  │  Storage  : GET /storage                  │  │
│  └─────────────────────────────────────────┘  │
│                                               │
│  Outils : bash, curl, python3, jq, sqlite3,   │
│           git, vim, tmux, ffmpeg               │
└─────────────────────────────────────────────┘
```

---

## Composants en détail

### Terminal

Le terminal est accessible depuis un onglet "Sandbox" dans le dashboard, à côté de Browser, Logs, Suparna.

- WebSocket endpoint `/ws/sandbox/terminal` (bash interactif dans le conteneur)
- Le frontend utilise un pattern HTTP streaming (comme AdbTerminal) ou WebSocket natif
- Session persistante : le shell survit aux refreshs de page
- Historique sauvegardé dans `/sandbox/files/.bash_history`

### File System

```
/sandbox/
├── files/          ← persistant, survit aux restart
│   ├── notes.md
│   ├── data.json
│   └── ...
├── tmp/            ← tmpfs, effacé au stop du conteneur
│   └── processing/
├── downloads/      ← fichiers téléchargés via Vātāyana
│   ├── facture.pdf
│   └── image.png
├── screenshots/    ← captures d'écran du browser
│   └── 2026-07-14-1832.jpg
└── scripts/        ← scripts utilisateur exécutables
    ├── backup.sh
    └── process.py
```

API REST :
```
GET    /api/sandbox/files?path=/files/           ← lister
GET    /api/sandbox/files?path=/files/notes.md   ← lire
PUT    /api/sandbox/files?path=/files/notes.md   ← écrire
DELETE /api/sandbox/files?path=/tmp/old.txt      ← supprimer
```

### Command execution

Pour les commandes non-interactives :
```
POST /api/sandbox/exec
{ "command": "ls -la /sandbox/files", "timeout": 30 }
→ { "stdout": "...", "stderr": "", "exit_code": 0 }
```

### Outils disponibles

Le conteneur sandbox inclut :
- `bash`, `coreutils` — shell standard
- `curl`, `wget` — HTTP
- `python3` — scripts
- `sqlite3` — base de données légère
- `jq` — JSON
- `git` — versionnement des scripts
- `vim` — éditeur
- `tmux` — sessions persistantes
- `ffmpeg` — traitement média

---

## Les espaces de stockage

Yantraśālā affiche en un coup d'œil ce qui est occupé où. Deux sources distinctes, côte à côte dans le dashboard.

### Stockage VPC

Le VPC héberge plusieurs volumes Docker. L'API expose un snapshot :

```
GET /api/web/sandbox/storage-vpc

{
  "volumes": [
    { "name": "sandbox_files",      "used_mb": 24,  "quota_mb": 512 },
    { "name": "sandbox_downloads",  "used_mb": 145, "quota_mb": 1024 },
    { "name": "sandbox_screenshots","used_mb": 8,   "quota_mb": 256 },
    { "name": "gafam_data",         "used_mb": 780, "quota_mb": 2048 },
    { "name": "qwen_model",         "used_mb": 520, "quota_mb": 1024 },
    { "name": "browser_data",       "used_mb": 12,  "quota_mb": 256 }
  ]
}
```

Les quotas sont définis dans les `docker-compose.yml` respectifs. La taille réelle est mesurée sur le disque.

### Stockage Android

L'APK relay déclare ce qu'elle utilise via le endpoint Edge sync :

```
POST /api/auth/edge/sync

{
  "storage": {
    "apk_size_mb": 12,
    "model_size_mb": 0,
    "cache_mb": 3,
    "logs_mb": 8,
    "total_used_mb": 23,
    "device_total_gb": 128,
    "device_free_gb": 45
  }
}
```

### Dashboard côte à côte

```
┌─ VPC ───────────────────────┐  ┌─ Phone (128 Go) ────────────┐
│ sandbox       24 / 512 MB   │  │ APK              12 MB      │
│ downloads    145 / 1024 MB  │  │ Model (Edge L2)    0 MB      │
│ screenshots    8 / 256 MB   │  │ Cache              3 MB      │
│ gafam_data   780 / 2048 MB  │  │ Logs               8 MB      │
│ qwen_model   520 / 1024 MB  │  │ GAFAM total       23 MB      │
│ browser       12 / 256 MB   │  │ Libre          45 Go dispo   │
└──────────────────────────────┘  └──────────────────────────────┘
```

### Pont ADB pull

Le sandbox peut pull des fichiers du téléphone (lecture seule) :

```
$ sandbox-phone-pull /sdcard/Download/facture.pdf
→ /sandbox/downloads/facture.pdf

$ sandbox-phone-ls /sdcard/Download/
→ facture.pdf (2.3 MB)
→ photo.jpg (4.1 MB)
```

L'APK déclare les dossiers qu'elle expose via le endpoint Edge sync. Pull autorisé, push interdit.

---

## Cycle de vie

Comme Qwen et Browser, le sandbox est un sidecar stoppé par défaut :

| État | RAM | Action |
|---|---|---|
| **Stoppé** | 0 Mo | Rien ne tourne |
| **Wake** | ~30-50 Mo | Alpine + bash + python3 actif |
| **Sous charge** | ~80-100 Mo | Selon les commandes exécutées |

- Wake : `POST /api/web/sandbox/wake` → démarre le conteneur, attend le healthcheck
- Stop : `POST /api/web/sandbox/stop` → arrête le conteneur, libère la RAM
- Le sandbox et Qwen ne tournent jamais ensemble sur un VPS 1 Go (même mutex `heavyBusy` que Suparna/Browser)

---

## Scénarios

### Scénario 1 — Terminal sandbox

```
1. Humain ouvre l'onglet Sandbox → wake → Terminal
2. $ cat /sandbox/files/sms_export.json | jq '.[].sender' | sort | uniq -c
3. $ python3 /sandbox/scripts/backup_contacts.py
4. $ curl -X POST http://localhost:5150/api/settings -d '{"key":"backup_enabled","value":"true"}'
```

### Scénario 2 — Téléchargement depuis le browser

```
1. Humain navigue sur Vātāyana → trouve une facture PDF
2. Vātāyana télécharge → /sandbox/downloads/facture.pdf
3. Humain ouvre le sandbox → Files → Downloads → voit facture.pdf (145 KB)
4. $ python3 /sandbox/scripts/parse_invoice.py /sandbox/downloads/facture.pdf
```

### Scénario 3 — Stockage plein

```
1. Humain ouvre Sandbox → Storage → voit gafam_data à 780/2048 MB
2. qwen_model à 520/1024 MB (modèle GGUF)
3. Décide de supprimer les vieux screenshots : $ rm /sandbox/screenshots/2026-06-*.jpg
4. Storage se met à jour automatiquement
```

---

## Principe : rien de caché

Le stockage n'est pas un dossier système abstrait. C'est **visible**, **divisible**, et **allouable** :
- L'humain voit exactement ce qui prend de la place
- Il peut décider d'allouer plus de quota aux screenshots ou aux downloads
- Les futures lucioles (Khadyota) sauront combien d'espace elles ont pour poser leurs résultats
- Les agents pourront détecter un disque plein et suggérer un nettoyage

---

## Intégration code

| Composant | Fichier |
|---|---|
| **Sandbox container** | `vpc-relay/Dockerfile.sandbox`, `docker-compose.sandbox.yml` |
| **Serveur sandbox** | `vpc-relay/sandbox_server.py` (HTTP files/storage + WebSocket terminal) |
| **Docker lifecycle** | `vpc-relay/sandbox/docker.go` |
| **Handlers API** | `vpc-relay/sandbox/handlers.go` (status, wake, stop, storage) |
| **Proxy** | `vpc-relay/sandbox/proxy.go` (reverse proxy vers conteneur) |
| **Routes** | `vpc-relay/main.go` (`/api/web/sandbox/*`) |
| **Frontend** | `frontend/src/lib/SandboxView.svelte` (Terminal + Files + Storage) |
| **Proxy CF** | `frontend/src/routes/api/proxy/sandbox/+server.ts` |
| **Dashboard** | `frontend/src/routes/[phone]/+page.svelte` (onglet "Sandbox") |
| **Deploy** | `deploy-vpc.sh` (install sandbox sidecar) |

---

## Questions ouvertes

1. Le sandbox doit-il avoir accès en **lecture** aux données GAFAM (SMS, logs) ou être totalement isolé ?
2. Faut-il un mode « sandbox partagé » où plusieurs sessions web voient le même terminal ?
3. Les scripts sandbox doivent-ils pouvoir appeler l'API GAFAM (ex: envoyer un SMS) ?
4. Faut-il des quotas par dossier ou un quota global sandbox ?

---

## Refus explicites

- Sandbox avec accès aux données privées (SMS, contacts) sans consentement
- Terminal exposé sans auth (sessionMiddleware obligatoire)
- Scripts sandbox qui accèdent au Docker socket
- Sandbox qui survit à un `rm -rf /` (isolation Docker)
- Push de fichiers vers l'Android (pull seulement)

---

## Liens manifestes

| # | Relation |
|---|---|
| **19** | Le Nœud Personnel — Yantraśālā est `/intents` côté humain |
| **22** | Vātāyana — le browser alimente `/sandbox/downloads/` et `/sandbox/screenshots/` |
| **23** | Khadyota — les lucioles utiliseront le sandbox comme espace de travail |
| **25** | Saṃyojaka — l'orchestrateur qui connectera les agents à cet atelier |

---

## Synthèse

> **Le VPC avait des yeux (Vātāyana), une voix (Suparna), une mémoire (SQLite). Il lui manquait des mains.**
>
> **Yantraśālā est l'établi — un terminal, des fichiers, du stockage visible. L'endroit où l'humain travaille, où les scripts s'exécutent, où les téléchargements se posent.**
>
> **Ce n'est pas un orchestrateur d'agents. C'est l'atelier que les agents utiliseront — quand Saṃyojaka les y connectera.**
