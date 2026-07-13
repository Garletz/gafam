# 20. Suparna — सुपर्ण · l'esprit aux belles ailes

> **Statut :** manifeste de conception pour la prochaine brique VPC.  
> **Prérequis opérationnels :** onglet Logs, `LogShipper` APK, `logs.go` VPC, bridge ADB Manager (manifest 14).  
> **Lien vision :** Phase 0 du [Ghost Clone](18_ghost_clone.md).

---

## Pourquoi « Suparna »

**Suparna** (सुपर्ण, *su* « beau » + *parṇa* « aile / plume ») est un épithète sanskrit de l'oiseau céleste — celui qui vole entre le monde visible et l'invisible, qui **voit de haut** sans être confondu avec un oracle ni un assistant.

Le nom est **volontairement opaque** :

- On ne sait pas encore si ce module **interprétera seulement** les logs ou **proposera un jour** des actions (ADB, recovery, chat).
- Ce n'est pas « IA », « assistant », « oracle » — rien qui promette une fonction figée.
- C'est une **présence** sur le VPC : un petit esprit plumeux qui lit ce que le téléphone murmure en logcat.

> *Pour l'instant Suparna écoute. Ce qu'il fera ensuite — traduire, avertir, suggérer — se décidera quand le modèle aura prouvé qu'il comprend le contexte.*

---

## L'idée en une phrase

> **Le VPC possède déjà les logs du téléphone. Suparna est l'oiseau local — Qwen ultra-léger via ONNX — qu'on invoque à la demande pour donner forme à ce bruit, sans jamais quitter le serveur.**

---

## Problème adressé

| Aujourd'hui | Limite |
| :--- | :--- |
| Onglet **Logs** : flux brut (`tag`, `level`, `message`) | Illisible pour un humain sur une journée entière |
| Messagerie SMS sur le web | Ne couvre pas Signal, banque, 2FA, anomalies système |
| Scrcpy / ADB Shell | Session lourde, Manager souvent requis, pas de synthèse |
| Ghost Clone (brouillon 18) | Trop large pour un premier pas |

**Besoin immédiat :** Logs → jour sélectionné → **invoquer Suparna** → synthèse structurée de la journée.

**Besoin futur (si le modèle convainc) :** le même esprit pourrait **suggérer** — jamais imposer — actions ADB, alertes recovery, fil d'événements. Rôle encore **non défini**.

---

## Principe GAFAM : intelligence **dans** le VPC

Conformément à la [philosophie](1_core_philosophy.md) (pilier B — le VPC comme cerveau) :

- **Aucun appel LLM cloud** sur les logs (SMS, recovery, tags opérateur).
- Modèle **open weights** ([Qwen3-0.6B](https://huggingface.co/Qwen/Qwen3-0.6B)) quantifié, servi **sur le droplet**.
- Inférence via **[ONNX Runtime](https://onnxruntime.ai/)** — stack portable, pas de PyTorch lourd en prod.
- Le Worker Cloudflare **ne voit jamais** le modèle ni les logs bruts.

---

## Architecture cible

```
┌──────────────── TÉLÉPHONE ─────────────────┐
│  APK LogShipper + (option) logcat via ADB   │
│  POST /api/auth/logs                        │
└────────────────────┬───────────────────────┘
                     │
                     ▼
┌──────────────── VPC (gafam-api) ─────────────────────────────┐
│  logs.go          Ring buffer JSONL · quota 1 Go · par jour  │
│       │                                                       │
│       ▼                                                       │
│  suparna          Sidecar ou subprocess ONNX · Qwen3-0.6B      │
│  (सुपर्ण)         Fenêtre de logs → prompt → JSON              │
│       │                                                       │
│       ▼                                                       │
│  POST             /api/web/logs/suparna?day=YYYY-MM-DD        │
│  sessionMiddleware                                            │
└────────────────────┬──────────────────────────────────────────┘
                     │
                     ▼
┌──────────────── WEB CLIENT ────────────────────────────────────┐
│  Logs · « Invoquer Suparna » (ou icône plume)                  │
│  Panneau : timeline, alertes, citations — sans jargon technique  │
└────────────────────────────────────────────────────────────────┘
```

| Option deploy | Description | RAM pic |
| :--- | :--- | :--- |
| **A — Sidecar** `gafam-qwen` (`llama.cpp` + GGUF) | Conteneur dédié, wake/stop | ~500 Mo actif, **0** stoppé |
| **B — Subprocess ONNX** | `onnxruntime-genai` (abandonné sur 1 Go — OOM torch) | trop lourd |
| **C — Ollama** | Dev seulement | ~800 Mo+ |

**Recommandation Phase 1 :** **A** — sidecar GGUF, **une invocation à la fois**, stop après usage.

---

## Modèle : Qwen3-0.6B

| Critère | Valeur |
| :--- | :--- |
| Paramètres | ~0,6B |
| Quantization | Q4 / INT4 ONNX (CPU VPS) |
| Mode | `enable_thinking=false` |
| Fenêtre entrée | 200–800 lignes filtrées / jour |

---

## Contrat de sortie (JSON strict)

```json
{
  "day": "2026-07-12",
  "summary": "Résumé en 3–5 phrases.",
  "timeline": [
    { "time": "14:32", "app": "sms", "event": "…", "severity": "info" }
  ],
  "alerts": [],
  "stats": { "sms_in": 12, "sms_out": 3, "errors": 1, "sources": ["apk"] },
  "confidence": "low|medium|high",
  "log_citations": ["14:32:01 sms …"]
}
```

Pas de chat libre. Schéma validé côté Go.

---

## Phases — le rôle de Suparna **évolue**

### Phase 1 — Suparna écoute *(maintenant)*

- Bouton **Invoquer Suparna** sur l'onglet Logs.
- `POST /api/web/logs/suparna`
- Cache SQLite `suparna_readings(day, json, created_at)`.
- **Rôle :** traduire une journée de logs en langage humain. Rien d'autre.

**Succès :** résumé utile (SMS, pair, outbox, erreurs) sur vraies données.

---

### Phase 2 — Suparna veille *(si Phase 1 OK)*

- Déclenchement sur patterns (`E`, `recovery`, `challenge`).
- Fil discret à côté de Chats — cartes courtes.
- **Rôle :** avertir, pas agir.

| Scénario | Utilité |
| :--- | :--- |
| Veille quotidienne | « Qu'est-ce qui s'est passé ? » |
| Recovery (manifest 5) | Keyword / challenge repéré |
| Panne VPC/APK | Diagnostic sans lire 400 lignes |
| Pré-scrcpy | Expliquer ADB déconnecté |

---

### Phase 3 — Suparna murmure des intentions *(peut-être)*

**Uniquement si le modèle comprend le contexte.** Rôle encore **hypothétique**.

| Capacité possible | Garde-fou |
| :--- | :--- |
| Suggestions ADB | Humain valide |
| Chat limité (5 tours) sur logs du jour | Pas d'Internet |
| Recovery assist | Jamais auto |

> Suparna ne devient **pas** un agent autonome sans décision produit explicite.

---

## Refus explicites (toutes phases)

- LLM sur SQLite SMS en clair
- Envoi SMS automatique
- Inférence 24/7
- Fine-tune cloud sur logs prod
- Remplacer scrcpy par le modèle

---

## Intégration code

| Composant | Fichier |
| :--- | :--- |
| Ingest | `LogShipper.kt` |
| Stockage | `logs.go` |
| UI | `Logs.svelte` — bouton plume / « Suparna » |
| Proxy | `api/proxy/logs/` |
| Moteur | **Nouveau** `vpc-relay/suparna/` |

**Prompt système (esquisse) :**

```
Tu es Suparna, module de lecture des journaux GAFAM. Entrée : lignes de log structurées.
Sortie : JSON uniquement. Langue : français. N'invente rien hors des logs.
```

---

## Budget VPS 1 Go

| Pic total ~520 Mo | Règles |
| :--- | :--- |
| gafam-api + Watchtower + Qwen Q4 | Mutex inférence · déchargement après 5 min idle |

---

## Sécurité

Session web confirmée · pas d'endpoint public · sanitize prompt injection · citations obligatoires · sidecar sans port Internet.

---

## Déploiement VPS (rêve 1 Go + swap)

**Stack retenue :** sidecar `llama.cpp` + GGUF Q4_K_M — **pas** de build ONNX/torch sur le droplet.

| Fichier | Rôle |
| :--- | :--- |
| `deploy-vpc.sh` (racine) | **Auto-install** Manager (`curl \| bash`) : **swap 4 Go inline**, `gafam-api` + sock, Qwen via scripts GitHub raw |
| `vpc-relay/docker-compose.qwen.yml` | Sidecar `gafam-qwen`, `mem_limit: 520m`, `-c 2048 --parallel 1` |
| `vpc-relay/scripts/setup-vpc-swap.sh` | Même logique swap (manuel / debug) — le deploy n’en dépend plus |
| `vpc-relay/scripts/qwen-install.sh` | GGUF → `/root/gafam_data/qwen/` + crée le conteneur |
| `vpc-relay/scripts/qwen-ctl.sh` | `start` / `stop` / `status` |

**Install auto (Manager) :** `curl …/deploy-vpc.sh | bash` → swap + API + Qwen (stoppé), sans fichiers locaux.

---

## Liens manifestes

| # | Relation |
| :--- | :--- |
| **1** | Première « intelligence » locale du cerveau VPC |
| **5** | Recovery — lecture des signes dans les logs |
| **14** | ADB — Phase 3 peut-être |
| **18** | Annexe logcat+LLM → **ce manifeste** |
| **19** | Premier esprit du nœud personnel |

---

## Nommage produit

| Contexte | Nom |
| :--- | :--- |
| Code / Docker | `vpc-relay/` · `gafam-qwen` · `vpc-relay/scripts/qwen-*.sh` |
| API | `/api/web/logs/suparna` |
| UI (bouton) | **Invoquer Suparna** ou icône 🪶 (plume) |
| Settings | **Suparna** — modèle & statut |
| README public | ne pas expliquer — laisser le sanskrit filtrer les curieux |

---

## Questions ouvertes

1. Sidecar vs subprocess ?
2. Résumés FR only ?
3. Logcat ADB inclus ?
4. Fine-tune futur ?

---

## Synthèse

> **Les logs sont le murmure du corps. Suparna est l'oiseau sur le VPC qui, pour l'instant, traduit — et peut-être un jour suggère — sans jamais quitter ton ciel.**

On ne promet pas un oracle ni un pilote automatique. On installe une **présence légère**, un nom qu'on ne comprend pas tout de suite, et on verra ce qu'elle devient quand Qwen3-0.6B GGUF aura lu assez de journées.

*Infra : modèle GGUF sur disque VPC, wake RAM à la demande, auto-stop. Endpoint `POST /api/web/logs/suparna` + bouton Logs.*
