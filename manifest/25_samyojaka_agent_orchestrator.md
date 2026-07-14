# 25. Saṃyojaka — संयोजक · l'orchestre des lucioles

> **Statut :** manifeste de conception.
> **Prérequis :** [Suparna](20_suparna.md) (L1 LLM), [Edge L2](21_dual_tier_inference.md) (phone inference), [Vātāyana](22_vatayana_remote_browser.md) (browser), [Yantraśālā](24_yantrashala_sandbox.md) (sandbox).
> **Lien vision :** le deuxième pilier du [Nœud Personnel](19_personal_node.md) — la coordination des agents (`/intents` côté machine).
> **Précédent :** [Yantraśālā — l'établi](24_yantrashala_sandbox.md) fournit les outils. Ce manifeste fournit le chef d'orchestre.

---

## Pourquoi « Saṃyojaka »

**Saṃyojaka** (संयोजक, *saṃ* « ensemble » + *yuj* « joindre, connecter ») signifie en sanskrit le coordinateur, le connecteur, celui qui met en relation. C'est le chef d'orchestre — pas le musicien, pas l'instrument, mais celui qui fait jouer les musiciens ensemble.

Le VPC a des musiciens (Suparna, Edge L2, Vātāyana) et des instruments (le sandbox, le browser, l'outbox SMS). Mais il n'a pas de **chef d'orchestre** — personne pour dire *« toi, joue ça. Toi, attends. Toi, passe le résultat à untel. »*

> *Saṃyojaka est le coordinateur. Il ne joue pas. Il ne compose pas. Il fait jouer les autres ensemble.*

---

## Le problème en une phrase

> **Le VPC a des agents en silos. Suparna ne peut pas demander à Vātāyana d'ouvrir un lien. Edge L2 ne peut pas demander au sandbox d'exécuter un script. Saṃyojaka est le système nerveux qui connecte les agents aux outils, et les agents entre eux.**

---

## Pourquoi séparer l'établi et l'orchestre

| Yantraśālā (manifest 24) | Saṃyojaka (ce manifeste) |
|---|---|
| **L'établi** — les outils eux-mêmes | **L'orchestre** — la coordination |
| Terminal, fichiers, stockage | Registre d'agents, file de tâches, routage |
| L'humain utilise directement | Les agents utilisent via le coordinateur |
| Indépendant des agents | Dépend de l'établi pour fonctionner |
| Phase 1 — implémentable maintenant | Phase 2 — après l'établi |

> *On ne peut pas orchestrer sans instruments. Mais avoir des instruments sans chef, c'est du bruit. Les deux sont nécessaires, dans cet ordre.*

---

## Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│                      SAṂYOJAKA (l'orchestre)                       │
│                                                                    │
│  ┌──────────────────────┐    ┌─────────────────────────────────┐  │
│  │  REGISTRE D'AGENTS    │    │  REGISTRE D'OUTILS               │  │
│  │                       │    │                                  │  │
│  │  suparna (L1, VPC)    │    │  browser.navigate → Vātāyana      │  │
│  │  edge_l2 (L2, phone)  │    │  browser.screenshot → Vātāyana   │  │
│  │  khadyota_* (L3, VPC) │    │  sandbox.exec → Yantraśālā      │  │
│  │  ghost_clone (L1)     │    │  sandbox.file_read → Yantraśālā  │  │
│  │                       │    │  sms.send → outbox               │  │
│  │  heartbeat → alive    │    │  vpc.service_restart → Docker    │  │
│  └──────────┬───────────┘    └──────────────┬──────────────────┘  │
│             │                                │                     │
│             └────────────┬───────────────────┘                     │
│                          ▼                                         │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │                    FILE DE TÂCHES                              │  │
│  │                                                               │  │
│  │  POST /api/agents/task                                        │  │
│  │       │                                                       │  │
│  │       ▼                                                       │  │
│  │  pending → routing L1↔L2 → running → completed|failed          │  │
│  │                                                               │  │
│  │  Pipeline :                                                    │  │
│  │  suggest → approve (humain) → execute → report                 │  │
│  └──────────────────────────────────────────────────────────────┘  │
│                                                                    │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │                    FRONTEND (cartes)                           │  │
│  │                                                               │  │
│  │  Carte "Suggestion"  →  [Approuver] [Ignorer] [Modifier]      │  │
│  │  Carte "En cours"    →  ⏳ Suparna analyse...                  │  │
│  │  Carte "Résultat"    →  ✅ Fait. [Voir] [Archiver]             │  │
│  └──────────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────────┘
```

---

## Composants en détail

### Registre d'Agents

Chaque agent GAFAM s'enregistre avec ses capacités et son statut :

```json
{
  "agent_id": "suparna_vpc_01",
  "name": "Suparna",
  "tier": "L1",
  "provider": "suparna",
  "status": "idle",
  "capabilities": [
    "read_logs",
    "analyze_day",
    "suggest_web_action",
    "suggest_sandbox_action"
  ],
  "tools": ["logs.read", "logs.analyze", "sandbox.exec"],
  "max_concurrent_tasks": 1,
  "last_heartbeat": "2026-07-14T18:32:01Z"
}
```

```json
{
  "agent_id": "edge_l2_phone",
  "name": "Edge L2",
  "tier": "L2",
  "provider": "edge",
  "status": "sleeping",
  "capabilities": [
    "deep_analyze_day",
    "long_context_inference",
    "multi_turn_reasoning"
  ],
  "tools": ["logs.deep_analyze", "sandbox.exec"],
  "max_concurrent_tasks": 1,
  "wake_required": true,
  "last_heartbeat": "2026-07-14T17:45:00Z"
}
```

Chaque agent envoie un heartbeat périodique. Si absent > 60s, l'agent est marqué `offline`.

### Registre d'Outils

Chaque outil du VPC est exposé comme une capability standardisée avec ses paramètres et son type de retour :

```json
{
  "tool_id": "browser.navigate",
  "handler": "vatayana",
  "description": "Ouvre une URL dans le navigateur distant",
  "params": {
    "url": { "type": "string", "required": true },
    "wait_for_load": { "type": "boolean", "default": true }
  },
  "returns": { "title": "string", "loaded": "boolean" }
}
```

```json
{
  "tool_id": "sandbox.exec",
  "handler": "yantrashala",
  "description": "Exécute une commande shell dans le sandbox",
  "params": {
    "command": { "type": "string", "required": true },
    "timeout_seconds": { "type": "int", "default": 30 }
  },
  "returns": { "stdout": "string", "stderr": "string", "exit_code": "int" }
}
```

```json
{
  "tool_id": "sms.send",
  "handler": "sms_outbox",
  "description": "Envoie un SMS via l'APK relay",
  "params": {
    "recipient": { "type": "string", "required": true },
    "body": { "type": "string", "required": true }
  },
  "returns": { "status": "queued|sent|failed" }
}
```

```json
{
  "tool_id": "browser.screenshot",
  "handler": "vatayana",
  "description": "Capture d'écran du navigateur",
  "params": {},
  "returns": { "url": "string", "width": 1024, "height": 576 }
}
```

### Pipeline de Tâche

```
1. AGENT → Coordinateur : POST /api/agents/suggest
   { "agent": "suparna", "suggestion": "Ouvrir https://banque.fr/verify?code=ABC",
     "reason": "Code de vérification reçu par SMS à 18:32" }

2. Coordinateur → Frontend : Carte "Suggestion"
   ┌──────────────────────────────────────────┐
   │ 🪶 Suparna suggère                        │
   │ « Ouvrir le lien de vérification          │
   │   bancaire reçu par SMS »                 │
   │ Raison : Code reçu à 18:32                │
   │                                           │
   │ [ Approuver ]  [ Ignorer ]  [ Modifier ]  │
   └──────────────────────────────────────────┘

3. Humain → Carte : [Approuver]

4. Coordinateur → Vātāyana : exécute browser.navigate(url)
   Task { id: "...", status: "running", progress: 0 }

5. Vātāyana → Résultat :
   { "status": "completed", "title": "Vérification réussie — Banque", "loaded": true }

6. Coordinateur → Frontend : Carte "Résultat"
   ┌──────────────────────────────────────────┐
   │ 🌐 Vātāyana — Vérification                │
   │ ✅ Page chargée : « Vérification réussie » │
   │ banque.fr — 18:32:45                      │
   │                                           │
   │ [ Voir capture ]  [ Archiver ]            │
   └──────────────────────────────────────────┘
```

### Routage L1↔L2

Le coordinateur choisit automatiquement quel agent exécute une tâche :

```go
func routeTask(task Task) Agent {
    // Tâches rapides et légères → L1 (VPC, toujours dispo)
    if task.EstimatedTokens < 1000 && task.Priority == "fast" {
        return registry.GetAgent("suparna")
    }
    // Tâches lourdes ou raisonnement profond → L2 (téléphone)
    if task.EstimatedTokens > 5000 || task.RequiresDeepReasoning {
        return registry.GetAgent("edge_l2")
    }
    // Défaut → L1
    return registry.GetAgent("suparna")
}
```

Le routage est transparent pour l'humain : il ne sait pas si la tâche est exécutée sur le VPC ou sur le téléphone.

---

## Scénarios

### Scénario 1 — Suparna détecte, Vātāyana agit

```
1. SMS reçu : "Votre code de vérification bancaire : 847291"
2. Suparna (L1) lit le SMS → détecte un code de vérification
3. Suparna → Saṃyojaka : suggest "Ouvrir le lien de la banque"
4. Humain approuve
5. Saṃyojaka → Vātāyana : browser.navigate("https://banque.fr/verify")
6. Vātāyana → Saṃyojaka → Humain : "✅ Vérifié. Page chargée."
```

### Scénario 2 — Luciole Khadyota explore

```
1. Humain : "Trouve-moi un hôtel à Paris pour vendredi, < 150€"
2. Saṃyojaka → route L2 (tâche complexe, multi-étapes)
3. Edge L2 → Saṃyojaka → Vātāyana : browser.navigate("booking.com")
4. Vātāyana navigue → extrait → Sandbox : /sandbox/files/hotels.json
5. Edge L2 → Saṃyojaka → Suparna : analyse les résultats
6. Suparna → Humain : carte "3 hôtels trouvés, 89€-129€"
```

### Scénario 3 — Auto-maintenance

```
1. Suparna lit les logs VPC → détecte "disk usage 85%"
2. Suparna → Saṃyojaka : suggest "Nettoyer les logs > 30 jours"
3. Humain approuve
4. Saṃyojaka → Sandbox : sandbox.exec("find /sandbox/tmp -mtime +30 -delete")
5. Sandbox → Saṃyojaka → Frontend : "✅ 124 fichiers supprimés, 340 Mo libérés"
```

### Scénario 4 — Chaîne d'agents

```
1. SMS reçu : "Rappel rendez-vous médical demain 14h. Confirmer ?"
2. Suparna (L1) → détecte → suggest "Confirmer le rendez-vous"
3. Humain approuve
4. Saṃyojaka → Sandbox : sandbox.exec("python3 /sandbox/scripts/send_reply.py 'OUI'")
5. Script → Saṃyojaka → SMS outbox : sms.send("OUI", "+33...")
6. Saṃyojaka → Humain : "✅ Réponse envoyée"
```

---

## Le parallèle OpenCode

OpenCode (anomalyco/opencode, MIT license) est un agent de codage en terminal. Sa structure est le modèle dont Saṃyojaka s'inspire :

```
┌──────────────────────────────────────┐
│             OPENCODE                 │
│  Agents : build, plan, general       │
│  Tools  : bash, read, write, edit    │
│  Skills : custom workflows           │
│  MCP    : external tool servers       │
│  LLM    : Claude, GPT, local models  │
│                                      │
│  User prompt → Agent → Tools → LLM   │
│                      ↑        │      │
│                      └────────┘      │
│               (tool results loop)    │
└──────────────────────────────────────┘
```

| Concept OpenCode | Équivalent GAFAM via Saṃyojaka |
|---|---|
| Agent (build, plan) | Agent (Suparna L1, Edge L2, Khadyota L3) |
| Tools (bash, read, write) | Tools (browser.navigate, sandbox.exec, sms.send) |
| Skills | Workflows GAFAM (scripts sandbox, chaînes d'agents) |
| MCP servers | Pas d'équivalent — le VPC **est** le serveur |
| LLM backend | Qwen GGUF (VPC) + ONNX (téléphone) |
| Terminal UI | Dashboard SvelteKit (onglet Sandbox + cartes) |

**GAFAM n'a pas besoin d'être OpenCode.** OpenCode est un agent de codage. GAFAM est un agent de **vie numérique**. Mais la structure est identique : agents + outils + coordinateur. Saṃyojaka est l'implémentation GAFAM de ce pattern.

> **OpenCode te donne un terminal pour coder. Saṃyojaka donne à ton VPC un orchestre pour agir.**

---

## Intégration code

| Composant | Fichier |
|---|---|
| **Agent registry** | `vpc-relay/agents/registry.go` |
| **Tool registry** | `vpc-relay/agents/tools.go` |
| **Task orchestrator** | `vpc-relay/agents/orchestrator.go` |
| **Agent handlers** | `vpc-relay/agents/handlers.go` |
| **Routes** | `vpc-relay/main.go` (`/api/agents/*`) |
| **Cartes UI** | `frontend/src/lib/AgentCards.svelte` |
| **Dashboard tab** | `frontend/src/routes/[phone]/+page.svelte` (section "Agents") |
| **Proxy** | `frontend/src/routes/api/proxy/agents/+server.ts` |

---

## Phases

### Phase 1 — Registre statique

- Les agents sont définis en dur dans le code Go (pas de discovery)
- Suparna et Edge L2 sont les deux premiers agents enregistrés
- Le registre d'outils est aussi statique
- Pas de file de tâches — les agents sont appelés directement via leur API existante
- **Rôle :** poser la structure de registre, sans coordination active

### Phase 2 — File de tâches

- `POST /api/agents/task` accepte des tâches
- Le coordinateur route vers l'agent approprié
- Statut des tâches : pending → running → completed/failed
- Pas encore de pipeline suggestion→approbation
- **Rôle :** le coordinateur exécute des tâches à la demande

### Phase 3 — Pipeline humain

- Cartes de suggestion dans le frontend
- Flux : agent suggère → humain approuve → coordinateur exécute → rapport
- Routage L1↔L2 automatique
- Les agents peuvent s'enchaîner (le résultat de l'un devient l'entrée du suivant)
- **Rôle :** les agents proposent, l'humain valide, le coordinateur exécute

### Phase 4 — Khadyota Phase 0

- Dīpa (jeton d'identité signé) généré par le nœud
- Luciole = agent Saṃyojaka + Dīpa + scope + tâche
- Les lucioles utilisent le registre d'outils comme n'importe quel agent
- Fallback Vātāyana pour les sites sans Mārga
- **Rôle :** première implémentation concrète du manifeste 23 (Khadyota Protocol)

---

## Questions ouvertes

1. Le coordinateur doit-il être un package Go dans vpc-relay ou un service séparé ?
2. Faut-il un système de permissions par agent (Suparna peut suggérer mais pas exécuter, Edge L2 peut exécuter mais pas envoyer de SMS) ?
3. Quel niveau de parallélisme : les agents peuvent-ils utiliser plusieurs outils simultanément ?
4. Faut-il exposer les outils GAFAM comme un **serveur MCP standard** pour que des agents externes puissent les utiliser ?
5. Les agents doivent-ils pouvoir se découvrir mutuellement (discovery) ou le registre est-il centralisé ?
6. Faut-il un « mode démonstration » où l'humain voit l'agent travailler en temps réel (Vātāyana Mode A partagé) ?

---

## Refus explicites

- Agents qui exécutent sans validation humaine (Phase 1-3)
- Agents qui spawn d'autres agents sans limite
- Modification automatique de la configuration VPC par les agents
- Accès aux données privées (SMS, contacts) sans consentement explicite
- Agents externes non enregistrés qui accèdent au registre

---

## Liens manifestes

| # | Relation |
|---|---|
| **19** | Le Nœud Personnel — Saṃyojaka implémente `/intents` côté machine |
| **20** | Suparna — premier agent enregistré, L1 |
| **21** | Dual Tier — le routage L1↔L2 est le cœur de Saṃyojaka |
| **22** | Vātāyana — outil `browser.*` dans le registre |
| **23** | Khadyota — les lucioles sont des agents Saṃyojaka avec Dīpa |
| **24** | Yantraśālā — l'établi que Saṃyojaka connecte aux agents |
| **18** | Ghost Clone — futur agent qui enrichira `/state` |

---

## Synthèse

> **Un orchestre sans instruments fait du silence. Des instruments sans chef font du bruit.**
>
> **Yantraśālā a donné les instruments au VPC (terminal, fichiers, stockage). Saṃyojaka donne le chef d'orchestre — le registre qui sait qui peut jouer quoi, la file de tâches qui dit quand jouer, le pipeline qui fait approuver par l'humain avant d'exécuter.**
>
> **Suparna propose. L'humain approuve. Le coordinateur exécute. Vātāyana navigue. Le sandbox traite. L'outbox envoie.**
>
> **Ce n'est plus un serveur qui répond. C'est un nœud qui agit — avec des agents qui coopèrent, des outils qui s'enchaînent, et un humain qui garde la baguette.**
