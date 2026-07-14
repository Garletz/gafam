# 22. Vātāyana — वातायन · la fenêtre distante

> **Statut :** manifeste de conception.
> **Prérequis opérationnels :** VPC Docker (1 Go + swap), frontend Cloudflare, proxy TCP Socket.
> **Lien vision :** le pendant « navigateur » du [Remote Control scrcpy](14_remote_android_control.md), le petit frère web de [Suparna](20_suparna.md).

---

## Pourquoi « Vātāyana »

**Vātāyana** (वातायन, *vāta* « vent » + *ayana* « passage ») signifie **fenêtre** en sanskrit — l'ouverture par laquelle passe le vent et la lumière.

Le VPC a déjà une voix (Suparna lit les logs), un corps (scrcpy voit le téléphone), une mémoire (SQLite). Il lui manquait **une fenêtre** — un navigateur réel, pas un scraper headless, pas une API HTTP — un vrai Firefox qui voit le web comme un humain.

> *Vātāyana n'est pas un bot. C'est une fenêtre que l'utilisateur ouvre depuis son tableau de bord. Et un jour, s'il le permet, ses agents regarderont par cette fenêtre pour lui.*

---

## L'idée en une phrase

> **Le VPC héberge un Firefox réel, l'utilisateur le contrôle depuis son client web via noVNC, et demain ses agents d'inférence (L1/L2) pourront l'utiliser comme bac à sable web pour des recherches et tâches basiques.**

---

## Problème adressé

| Aujourd'hui | Limite |
|---|---|
| Pas de navigateur sur le VPC | Impossible de consulter un site, remplir un formulaire, vérifier une page |
| Les agents (Suparna, Edge L2) sont confinés aux logs/SMS | Aucun accès au web pour recherche, vérification de liens, extraction |
| Scrcpy donne le téléphone | Mais le téléphone n'est pas toujours branché au Manager |
| Headless Chrome en CLI (`curl`, `wget`) | Pas de JS, pas de rendu, pas de sessions, pas de login web |

**Besoin immédiat :** un vrai navigateur GUI, ouvrable à la demande depuis gafam.cloud.

**Besoin futur :** que les agents GAFAM puissent utiliser ce navigateur — sous supervision humaine — pour exécuter des tâches web simples.

---

## Architecture

```
┌──────────────┐     TCP Socket         ┌─────────────────┐   Docker sock    ┌──────────────────────┐
│  Frontend    │ ◄─────────────────────► │  Go API          │ ◄──────────────► │  gafam-browser       │
│  SvelteKit   │   Cloudflare Proxy     │  (port 5150)     │  start/stop      │  (sidecar, stoppé)   │
│              │                        │                  │                  │                       │
│ BrowserView  │── iframe ──────────────│► /browser/*      │── reverse ──────│► :6080 (websockify)   │
│   .svelte    │   ou canvas WebCodecs  │  reverse proxy   │   proxy (HTTP+WS)│                       │
│              │                        │                  │                  │ Xvfb + Openbox        │
│              │                        │ /api/web/        │                  │ + Firefox ESR          │
│              │                        │   browser/*      │                  │ + PulseAudio           │
│              │                        │   wake/stop      │                  │ + x11vnc               │
└──────────────┘                        └─────────────────┘                  └───────────────────────┘
```

### Composants

| Composant | Emplacement | Rôle |
|---|---|---|
| **Dockerfile.browser** | `vpc-relay/` | Image custom : debian:bookworm-slim + Firefox ESR + Xvfb + websockify |
| **docker-compose.browser.yml** | `vpc-relay/` | Sidecar `gafam-browser`, `mem_limit: 600m`, stoppé par défaut |
| **browser/docker.go** | `vpc-relay/browser/` | Contrôle cycle de vie container via Docker socket (même pattern que Suparna) |
| **browser/proxy.go** | `vpc-relay/browser/` | Reverse proxy `httputil.ReverseProxy` vers `gafam-browser:6080` (HTTP + WS natif) |
| **browser/handlers.go** | `vpc-relay/browser/` | Status, wake, stop |
| **BrowserView.svelte** | `frontend/src/lib/` | iframe noVNC ou canvas WebCodecs |
| **Proxy CF** | `frontend/src/routes/api/proxy/browser/` | TCP Socket tunnel pour le flux noVNC (HTTP + WebSocket) |
| **deploy-vpc.sh** | racine | Installation auto du sidecar browser |

---

## Pourquoi noVNC et pas un protocole custom ?

| Option | Pour | Contre |
|---|---|---|
| **noVNC (choisi)** | Mature, standard, Firefox réel, audio PulseAudio, WebSocket natif | ~50 Mo overhead (xvfb + x11vnc + websockify) |
| Custom H.264 (comme scrcpy) | Léger, réutilise WebCodecs | Firefox en headless ne rend pas comme un vrai écran, pas d'audio facile |
| Xpra | Window-level forwarding, efficace | Client HTML5 moins mature, complexe |
| KasmVNC | WebRTC, optimisé web | Image Docker lourde, overkill 1 Go |

**Choix :** noVNC en sidecar stoppé, wake à la demande. Pattern identique à [Suparna](20_suparna.md) — disque uniquement, RAM uniquement quand actif.

---

## Configuration Firefox ESR « ultra-lite »

Ajustements `about:config` pour tenir dans ~300 Mo RAM :

| Paramètre | Valeur | Effet |
|---|---|---|
| `browser.cache.disk.enable` | `false` | Pas de cache disque (le conteneur est jetable) |
| `browser.cache.memory.capacity` | `32768` | 32 Mo cache mémoire max |
| `browser.sessionhistory.max_entries` | `5` | Historique par onglet réduit |
| `browser.sessionstore.interval` | `600000` | Sauvegarde session toutes les 10 min |
| `gfx.webrender.all` | `false` | Pas de GPU (pas de carte graphique sur VPS) |
| `layers.acceleration.disabled` | `true` | Rendu software |
| `dom.ipc.processCount` | `1` | Single content process |
| `javascript.options.mem.max` | `131072` | 128 Mo max pour JS |
| `media.autoplay.default` | `1` | Bloque autoplay (économie CPU) |
| `privacy.trackingprotection.enabled` | `true` | Anti-trackers |

---

## Budget VPS 1 Go

| Composant | RAM idle | RAM actif |
|---|---|---|
| gafam-api | ~30 Mo | ~30 Mo |
| gafam-browser (stoppé) | 0 | 0 |
| gafam-qwen (stoppé) | 0 | 0 |
| **gafam-browser actif :** | | |
| Firefox ESR (allégé) | — | ~250–350 Mo |
| Xvfb (1920×1080×24) | — | ~30 Mo |
| Openbox | — | ~10 Mo |
| PulseAudio | — | ~15 Mo |
| x11vnc | — | ~20 Mo |
| websockify (+ noVNC statique) | — | ~10 Mo |
| **Total browser actif** | — | **~350–450 Mo** |
| **Total VPS (browser seul, pas Qwen)** | ~30 Mo | **~400–500 Mo** |

> Le browser et Qwen ne tournent **jamais ensemble** sur un VPS 1 Go. Le Go API verrouille : si Qwen est actif, le browser refuse de démarrer, et inversement. Même pattern `heavyBusy()` que Suparna.

---

## Phases

### Phase 1 — La fenêtre s'ouvre *(ce manifeste)*

- Onglet **Browser** dans le dashboard web (à côté de Chats, Contacts, Logs, Suparna).
- Bouton **Wake** → démarre le conteneur, lance Firefox ESR.
- iframe noVNC dans le panel principal.
- Audio PulseAudio streamé via le canal VNC.
- Bouton **Stop** → éteint le conteneur, libère la RAM.
- **Rôle :** navigation humaine, manuelle. L'utilisateur ouvre sa fenêtre et surfe.

### Phase 2 — La fenêtre assistée *(si Phase 1 OK)*

- Les agents (Suparna L1, Edge L2) peuvent **proposer** des actions navigateur.
- Exemple : Suparna lit un SMS contenant un lien de vérification → propose « Ouvrir ce lien dans Vātāyana ».
- Le frontend affiche une carte « Action suggérée », l'utilisateur valide ou ignore.
- L'agent ne pilote **pas** le navigateur directement — il demande à l'humain la permission d'ouvrir un onglet.

| Scénario | Agent | Action |
|---|---|---|
| SMS de vérification bancaire | Suparna | « Ouvrir le lien de vérification ? » |
| Logcat montre une erreur réseau | Suparna | « Tester la connexion sur example.com ? » |
| Code 2FA reçu | SMS Codes | « Auto-remplir le formulaire web ? » |

### Phase 3 — Les agents regardent par la fenêtre *(vision)*

> **Uniquement si Phase 2 convainc.** Les agents pourront **naviguer de manière semi-autonome** dans le bac à sable Vātāyana.

| Capacité | Garde-fou |
|---|---|
| Recherche web (moteur de recherche) | Résultat affiché, humain choisit le lien |
| Remplissage de formulaire | Pré-remplissage, humain valide avant submit |
| Extraction de données structurées | JSON → affiché en carte dans le dashboard |
| Navigation multi-pages | Limite de profondeur (3 pages max) |
| Capture d'écran | Sauvegardée dans l'onglet Logs |

**Agents habilités à utiliser Vātāyana :**

| Agent | Tier | Usage typique |
|---|---|---|
| **Suparna** | L1 (VPC) | Vérifier un lien reçu par SMS |
| **Edge L2** | L2 (Téléphone) | Recherche longue, multi-pages |
| **Futur agent VPC** | L1 | Tâche planifiée nocturne (ex: vérifier 3 prix) |

**Protocole agent → navigateur (esquisse) :**

```
Agent → Go API     POST /api/web/browser/agent/task
                    { "agent": "suparna", "action": "navigate", "url": "..." }
Go API → Browser   CDP / WebDriver BiDi (Firefox marionette)
Browser → Go API   { "screenshot": "...", "dom_summary": "...", "forms": [...] }
Go API → Frontend  Carte "Vātāyana suggère" — humain valide
```

---

## Refus explicites (toutes phases)

- Navigation automatique sans validation humaine (jamais en Phase 1–2, jamais sans paramètre révocable en Phase 3)
- Accès aux sessions VPC/admin du navigateur par les agents
- Crawl massif / aspiration de sites
- Utilisation du navigateur pour contourner des paywalls
- Login automatique sur des sites tiers sans consentement
- Agents qui modifient la config Firefox

---

## Sécurité

| Surface | Mesure |
|---|---|
| Container browser | Réseau `gafam-net` interne, pas de port exposé sur l'hôte |
| Proxy `/browser/*` | `sessionMiddleware` (même auth que tout le dashboard) |
| WebSocket VNC | Via reverse proxy Go (même session token) |
| Isolement agents | Les agents n'ont pas accès direct au conteneur, seulement via l'API Go |
| Firefox profile | Volatile (tmpfs ou overlay), aucun cookie/state ne survit au stop |
| RAM | Mutex browser/Qwen — un seul sidecar lourd à la fois |
| Audio | PulseAudio interne container uniquement, pas d'écoute réseau |

---

## Intégration code

| Composant | Fichier |
|---|---|
| Image navigateur | `vpc-relay/Dockerfile.browser` |
| Sidecar compose | `vpc-relay/docker-compose.browser.yml` |
| Install script | `vpc-relay/scripts/browser-install.sh` |
| Contrôle container | `vpc-relay/browser/docker.go` |
| Reverse proxy | `vpc-relay/browser/proxy.go` |
| Handlers API | `vpc-relay/browser/handlers.go` |
| Routes Go | `vpc-relay/main.go` (`/api/web/browser/*`, `/browser/*`) |
| UI composant | `frontend/src/lib/BrowserView.svelte` |
| Proxy Cloudflare | `frontend/src/routes/api/proxy/browser/[...path]/+server.ts` |
| Dashboard | `frontend/src/routes/[phone]/+page.svelte` (onglet Browser) |
| Déploiement | `deploy-vpc.sh` (install_browser_sidecar) |

---

## Liens manifestes

| # | Relation |
|---|---|
| **1** | Le VPC comme cerveau — maintenant il a des yeux |
| **14** | Scrcpy : même pattern de streaming distant, même hub WebSocket |
| **18** | Ghost Clone : le navigateur distant complète le miroir sémantique |
| **20** | Suparna : même pattern sidecar Docker stoppé / wake à la demande |
| **21** | Dual Tier : les agents L1/L2 utiliseront Vātāyana comme bac à sable |

---

## Mode agent : session partagée + session isolée (les deux)

Les agents (Suparna, Edge L2) disposeront de **deux modes** pour interagir avec Vātāyana :

### Mode A — Session partagée (visible)

L'agent pilote le **même Firefox** que l'humain voit dans l'iframe noVNC.

- L'humain **voit** ce que l'agent fait en temps réel (curseur, frappe, navigation).
- L'agent demande une action → l'humain valide → l'agent l'exécute dans la session visible.
- Utile pour : « Montre-moi », « Ouvre ce lien que j'ai reçu par SMS et je regarde ».

| Avantage | Risque |
|---|---|
| Transparence totale | L'humain ne peut pas naviguer en même temps |
| Confiance : je vois tout | Lent si l'agent explore beaucoup |

### Mode B — Session isolée (headless parallèle)

L'agent utilise un **deuxième Firefox headless séparé**, invisible pour l'humain, qui tourne en parallèle sans déranger la session principale.

- `firefox --headless` ou profil Firefox séparé avec marionette.
- L'agent travaille en arrière-plan, publie ses résultats dans le dashboard (cartes, captures).
- L'humain continue de naviguer normalement dans sa session noVNC.

| Avantage | Risque |
|---|---|
| Non-bloquant pour l'humain | Moins de visibilité sur ce que l'agent fait |
| Parallélisme : plusieurs agents simultanés | RAM supplémentaire (~150–200 Mo par Firefox headless) |
| Idéal pour tâches longues (recherche, extraction) | Nécessite des logs d'audit agent |

**Implémentation :** le conteneur `gafam-browser` aura deux profils Firefox distincts — `profile_main` (GUI, xvfb) et `profile_agent` (headless, marionette). Le Go API route les commandes agent vers le bon profil selon le mode demandé.

```
POST /api/web/browser/agent/task
{
  "agent": "suparna",
  "mode": "shared",       // "shared" | "isolated"
  "action": "navigate",
  "url": "https://..."
}
```

---

## Questions ouvertes

1. Firefox ESR ou Chromium headless avec WebDriver BiDi pour les agents ?
2. L'iframe noVNC suffit-il ou faut-il un rendu canvas WebCodecs pour la latence ?
3. Faut-il un mode « kiosque » (un seul onglet, pas de barre d'adresse) ou un Firefox complet ?
4. Les agents doivent-ils voir le DOM ou juste des captures d'écran ?
5. Faut-il permettre à l'utilisateur de téléverser son profil Firefox persistant ?

---

## Synthèse

> **Le VPC avait une voix (Suparna), un corps (scrcpy), une mémoire (SQLite). Il lui manquait des yeux. Vātāyana est la fenêtre qu'on ouvre depuis son tableau de bord — un vrai Firefox, dans le nuage, qui ne pèse rien quand il dort. Et quand les agents apprendront à regarder par cette fenêtre, le VPC ne se contentera plus d'écouter : il pourra voir, chercher, et montrer.**
