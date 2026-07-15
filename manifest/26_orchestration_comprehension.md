# 26. Note de compréhension — L'orchestration : OpenCode, OpenHands, Manus, et nous

> **Statut :** note d'analyse. Pas un manifeste de conception — un document pour comprendre le paysage et situer GAFAM.
> **Objet :** comparer l'orchestration dans OpenCode, OpenHands, Manus AI, et extraire ce qui s'applique à notre environnement singulier.

---

## Pourquoi cette note

Avant de coder Saṃyojaka (Manifest 25), il faut comprendre **ce que les autres font** et **ce qui nous est spécifique**. L'orchestration n'est pas un concept abstrait — c'est un pattern concret que trois projets majeurs ont déjà implémenté, chacun avec sa philosophie.

---

## Le paysage : 3 projets, 3 approches

### 1. OpenCode (anomalyco/opencode — 186k★, MIT)

**Ce que c'est :** un agent de codage en terminal. Tu l'utilises maintenant.

**Architecture d'orchestration :**

```
Utilisateur
    │
    ▼
Agent primaire (build ou plan)
    │
    ├── Tools : bash, read, write, edit, glob, grep, webfetch
    ├── MCP servers : outils externes (base de données, APIs, etc.)
    ├── Skills : workflows prédéfinis (markdown)
    │
    └── Subagents (invoqués via @mention ou automatiquement)
        ├── general : recherche multi-étapes, parallèle
        ├── explore : exploration read-only rapide
        └── scout : recherche dans deps externes
```

**Points clés :**
- **Agents = config** : un agent est un fichier JSON ou Markdown avec `description`, `model`, `permission`, `prompt`
- **Permissions per-agent** : `allow` / `ask` / `deny` pour chaque tool. Ex: `plan` a `edit: deny`, `bash: ask`
- **Task delegation** : un agent primaire peut invoquer un subagent via le "Task tool"
- **LLM externe** : OpenCode ne possède pas son LLM — il appelle Claude, GPT, Gemini
- **Pas de boucle autonome** : l'agent s'arrête quand le modèle décide de répondre en texte

**Ce qui nous intéresse :**
- Le système de **permissions per-agent** (allow/ask/deny)
- La **délégation** primaire → subagent
- La **config en Markdown** (un agent = un fichier)

### 2. OpenHands (OpenHands/OpenHands — 81k★)

**Ce que c'est :** un centre de contrôle self-hosté pour agents de codage.

**Architecture d'orchestration :**

```
Agent Canvas (frontend UI)
    │
    ├── Agent Server (REST API, multiple backends)
    │   ├── Docker sandbox (isolation complète)
    │   ├── VM backend
    │   └── Cloud backend
    │
    ├── Automation Server
    │   ├── Scheduled tasks (cron)
    │   ├── Webhook triggers (Slack, GitHub, Linear)
    │   └── Prebuilt automations
    │
    └── ACP (Agent-Client Protocol)
        └── Interopérabilité entre agents (OpenHands, Claude Code, Codex)
```

**Points clés :**
- **Agent Server = REST API** : démarrer/arrêter des agents via HTTP
- **Docker sandbox** : chaque agent tourne dans un conteneur isolé avec accès filesystem
- **Automations** : agents déclenchés par des events externes (Slack message, GitHub issue, schedule)
- **ACP** : protocole standard pour que des agents hétérogènes communiquent
- **Multi-backend** : le même frontend peut piloter des agents sur laptop, VM, ou cloud

**Ce qui nous intéresse :**
- Le **Docker sandbox par agent** (on a déjà ça avec Yantraśālā)
- Les **automations déclenchées par events** (webhook, schedule)
- Le **protocole ACP** pour que Suparna, Edge L2, et futures lucioles parlent le même langage

### 3. Manus AI (fermé, commercial)

**Ce que c'est :** un agent généraliste qui "utilise un ordinateur" comme un humain.

**Architecture (reconstituée) :**

```
Utilisateur : "Trouve-moi un appartement à Paris < 800€"
    │
    ▼
Planning (LLM décompose la tâche)
    ├── 1. Ouvre LeBonCoin
    ├── 2. Filtre par prix
    ├── 3. Extrait les résultats
    ├── 4. Vérifie les photos
    └── 5. Compile un rapport
    │
    ▼
Execution (boucle agent-tool-observation)
    ├── browser.open("leboncoin.fr")
    ├── browser.click("filtre prix")
    ├── browser.type("800")
    ├── browser.screenshot() → LLM analyse l'image
    ├── browser.extract_results() → JSON
    └── file.write("rapport.md", résultats)
    │
    ▼
Report (présentation du résultat à l'humain)
```

**Points clés :**
- **Plan → Execute → Report** : trois phases explicites
- **Boucle d'observation** : l'agent agit, observe le résultat, décide la prochaine action
- ** screenshots + LLM multimodal** : l'agent "voit" l'écran
- **Pas de permission ask** : Manus agit en autonome (l'humain donne la tâche, l'agent exécute)
- **Ordinateur virtuel complet** : browser, terminal, fichiers, tout dans un sandbox

**Ce qui nous intéresse :**
- Le pattern **Plan → Execute → Report**
- La **boucle d'observation** (action → résultat → décision)
- Le concept de **tâche autonome avec rapport final**

---

## Tableau comparatif

| Critère | OpenCode | OpenHands | Manus | GAFAM (cible) |
|---|---|---|---|---|
| **Domaine** | Code | Code | Vie numérique | Vie numérique souveraine |
| **LLM** | Externe (Claude, GPT) | Externe | Externe | Local (Qwen VPC + ONNX phone) |
| **Sandbox** | Filesystem local | Docker | VM complète | Docker (Yantraśālā) |
| **Browser** | Non | Non | Oui (computer use) | Oui (Vātāyana) |
| **Permissions** | allow/ask/deny per agent | Docker isolation | Autonome | À définir |
| **Subagents** | @mention + Task tool | ACP | Plan interne | À définir |
| **Automations** | Non | Webhooks + cron | Non | SMS trigger (unique) |
| **Identity** | Aucune | Aucune | Compte Manus | Dīpa signé (unique) |
| **Souveraineté** | Non | Self-hosted mais LLM cloud | Cloud | Total (VPC + phone) |

---

## Ce qui est important dans l'orchestration (et ce qui ne l'est pas)

### IMPORTANT — les 5 piliers

**1. Le registre d'agents**
Sans registre, pas d'orchestration. Chaque agent doit déclarer : qui il est, ce qu'il sait faire, avec quel modèle, et quelles sont ses permissions.

**2. Le registre d'outils**
Les outils doivent être standardisés. Pas d'appels ad-hoc. Chaque outil a : un ID, des paramètres typés, un handler, et un type de retour.

**3. La boucle agent-outil-observation**
C'est le cœur de l'orchestration. L'agent propose une action → l'orchestrateur exécute l'outil → l'agent observe le résultat → l'agent propose la suivante. Cette boucle continue jusqu'à ce que la tâche soit faite ou que l'humain intervienne.

**4. Les permissions**
Sans permissions, un agent peut faire n'importe quoi. Le pattern OpenCode (allow/ask/deny per tool per agent) est le plus simple et le plus efficace.

**5. Le pipeline humain**
L'humain doit pouvoir : valider avant exécution, interrompre en cours, voir le résultat. C'est le pattern "suggestion → approbation → exécution → rapport".

### PAS IMPORTANT — ce qu'on peut ignorer

- **ACP (Agent-Client Protocol)** : standard d'interopérabilité entre agents hétérogènes. On n'a que des agents GAFAM, pas besoin d'interopérabilité externe pour l'instant.
- **Multi-backend** : OpenHands permet de piloter des agents sur plusieurs machines. On a un seul VPC.
- **Webhooks externes** : OpenHands se déclenche sur Slack/GitHub. On se déclenche sur SMS — c'est plus simple et plus souverain.
- **LLM externe** : OpenCode et OpenHands appellent Claude/GPT. Nous on a Qwen en local. C'est plus lent mais souverain.

---

## Notre environnement singulier — ce que les autres n'ont pas

| Avantage GAFAM | Conséquence pour l'orchestration |
|---|---|
| **Le VPC est le sandbox** | Pas besoin de VM séparée. Yantraśālā tourne dans Docker sur le même serveur que l'API. |
| **Vātāyana est le browser** | Pas besoin de "computer use" — on a déjà un Firefox distant actionnable. |
| **L'APK relay est le canal SMS** | Un agent peut envoyer un SMS — aucun autre système d'agents ne peut faire ça. |
| **Suparna + Edge L2 sont locaux** | Pas de coût d'API. Pas de latence cloud. Mais modèle plus faible (0.6B). |
| **Le Dīpa est l'identité** | Une luciole peut prouver qui elle est cryptographiquement. OpenCode/Manus n'ont pas d'identité. |
| **Le SMS est le trigger** | Un SMS peut déclencher une tâche agent. C'est notre "webhook" unique — plus souverain qu'un Slack. |
| **Le téléphone est le L2** | Un agent peut déléguer le raisonnement lourd au téléphone (2-4 Go RAM) — disponible, pas cloud. |

---

## Ce qui manque concrètement dans GAFAM

### 1. La boucle agent-outil-observation

Aujourd'hui, Suparna lit des logs et produit un JSON. C'est un appel unique, pas une boucle. Il manque :

```
Agent : "Je veux vérifier ce lien reçu par SMS"
    │
    ▼
Orchestrateur : execute(browser.navigate, {url: "..."})
    │
    ▼
Outil : Vātāyana navigue → screenshot
    │
    ▼
Agent observe : "La page montre un formulaire de vérification"
    │
    ▼
Agent : "Je veux remplir le champ 'code' avec 847291"
    │
    ▼
Orchestrateur : execute(browser.input, {type: "type", text: "847291"})
    │
    ▼
Outil : Vātāyana tape le code
    │
    ▼
Agent observe : "Le formulaire a été soumis, la page dit 'Vérifié'"
    │
    ▼
Agent : "Tâche terminée. Rapport : code vérifié avec succès."
```

**Ce qu'il faut coder :**
- Un `agent_loop` qui prend une tâche + un contexte + des outils, et itère
- Le LLM reçoit : le prompt système, la tâche, l'historique des actions, et le dernier résultat d'outil
- Le LLM répond soit avec une action (`tool_call`) soit avec un rapport final (`done`)
- Boucle limitée à N itérations (max_steps comme OpenCode)

### 2. Le registre d'outils standardisé

Aujourd'hui chaque outil (browser, sandbox, SMS) a sa propre API ad-hoc. Il faut un format commun :

```json
{
  "tool_id": "browser.navigate",
  "params": { "url": "string", "wait": "boolean" },
  "returns": { "title": "string", "loaded": "boolean" }
}
```

Le LLM voit la liste des outils disponibles et choisit lequel appeler. C'est exactement comme les "function calls" de l'API OpenAI, mais avec nos outils.

### 3. Les permissions

Quels agents peuvent utiliser quels outils ? Le pattern OpenCode s'applique directement :

| Agent | browser.* | sandbox.* | sms.* | vpc.* |
|---|---|---|---|---|
| Suparna (L1) | ask | ask | deny | deny |
| Edge L2 | ask | allow | deny | deny |
| Khadyota | allow | allow | ask | deny |

`allow` = exécute sans demander. `ask` = propose à l'humain. `deny` = interdit.

### 4. Le déclencheur SMS

C'est notre "automation" unique. Aujourd'hui, les SMS sont juste stockés. Il faut :

```
SMS reçu → Suparna analyse → si pattern d'action détecté
    → suggestion de tâche → l'humain valide → l'agent exécute
```

Exemples de patterns :
- "Votre code de vérification : 847291" → suggérer d'ouvrir le lien
- "Rappel : rendez-vous demain 14h" → suggérer d'ajouter au calendrier
- "URGENCE_GAFAM" → déclencher le flow de recovery

---

## Comment mettre en place dans notre projet

### Phase 1 — Tool registry (le plus simple, le plus utile)

Créer `vpc-relay/agents/tools.go` :

```go
type Tool struct {
    ID          string
    Description string
    Handler     func(params map[string]interface{}) (interface{}, error)
    Params      map[string]ParamSpec
}

var toolRegistry = map[string]Tool{
    "browser.navigate": { ... },
    "browser.screenshot": { ... },
    "sandbox.exec": { ... },
    "sandbox.file_read": { ... },
    "sandbox.file_write": { ... },
    "sms.send": { ... },
}
```

Chaque handler appelle l'API existante (browser, sandbox, outbox). Pas de nouveau code fonctionnel — juste un wrapper standardisé.

### Phase 2 — Agent loop (la boucle)

Créer `vpc-relay/agents/loop.go` :

```go
func RunAgentLoop(ctx context.Context, agent Agent, task string, tools []Tool, maxSteps int) (string, error) {
    history := []Message{}
    for step := 0; step < maxSteps; step++ {
        // 1. Construire le prompt : tâche + historique + outils disponibles
        // 2. Appeler le LLM (Suparna L1 ou Edge L2)
        // 3. Parser la réponse : tool_call ou done
        // 4. Si tool_call : exécuter l'outil, ajouter le résultat à l'historique
        // 5. Si done : retourner le rapport
    }
    return "max steps reached", nil
}
```

Le LLM est Qwen via Suparna (L1) ou Edge (L2). Le prompt suit le pattern "function calling" — on décrit les outils disponibles et le modèle choisit lequel appeler.

### Phase 3 — Permissions

Créer `vpc-relay/agents/permissions.go` :

```go
type Permission string
const (
    Allow Permission = "allow"
    Ask   Permission = "ask"
    Deny  Permission = "deny"
)

type AgentConfig struct {
    Name        string
    Tier        string // L1, L2, L3
    Tools       map[string]Permission
    MaxSteps    int
}
```

Quand l'orchestrateur veut exécuter un outil, il vérifie la permission. Si `ask`, il envoie une carte de suggestion au frontend. Si `allow`, il exécute. Si `deny`, il refuse.

### Phase 4 — SMS trigger

Dans `api.go`, quand un SMS est reçu :

```go
// Après avoir stocké le SMS, vérifier si un agent devrait réagir
if shouldTriggerAgent(sms) {
    go func() {
        task := buildTaskFromSMS(sms)
        result, err := RunAgentLoop(ctx, suparnaAgent, task, tools, 10)
        if err == nil {
            saveSuggestion(result) // stocke la suggestion pour le frontend
        }
    }()
}
```

### Phase 5 — Frontend (cartes de suggestion)

Dans le dashboard, une nouvelle section "Agent Activity" qui montre :
- Les suggestions en attente (`ask`) avec boutons [Approuver] [Ignorer]
- Les tâches en cours avec statut
- Les rapports terminés avec [Voir] [Archiver]

---

## Ce qu'on peut ajouter grâce à notre environnement singulier

### 1. SMS → Agent (notre webhook souverain)

Aucun autre système d'agents ne peut se déclencher par SMS. C'est notre avantage unique. Un utilisateur peut :

```
SMS → "rapport" → Suparna génère un rapport de la journée → SMS reply avec résumé
SMS → "backup" → sandbox.exec("tar -czf /sandbox/files/backup.tar.gz /sandbox/files/") → confirmation
SMS → "browse url.com" → Vātāyana ouvre l'URL → screenshot envoyé par MMS (futur)
```

### 2. Edge L2 comme cerveau profond

OpenCode et Manus utilisent Claude/GPT pour la boucle agent. Nous on peut utiliser Edge L2 (Qwen sur téléphone, 2-4 Go RAM) pour le raisonnement profond, et Suparna L1 (0.6B sur VPC) pour les tâches rapides. Le routing est automatique :

- Tâche simple (classifier un SMS, lire un fichier) → L1 (VPC, 1s)
- Tâche complexe (analyser une page web, planifier multi-étapes) → L2 (phone, 10-30s)

### 3. Dīpa pour les lucioles Khadyota

Quand une luciole visite un site, elle porte un Dīpa (jeton d'identité signé par le nœud). Le site sait que c'est une émanation de quelqu'un, pas un bot anonyme. C'est ce que Manus et OpenCode ne peuvent pas faire — ils n'ont pas d'identité souveraine.

### 4. Yantraśālā comme mémoire d'agent

Le sandbox n'est pas juste un terminal — c'est la **mémoire persistante** des agents. Un agent peut :
- Écrire ses notes dans `/sandbox/files/agent_notes.md`
- Stocker des données extraites dans `/sandbox/files/extracted/`
- Garder des scripts réutilisables dans `/sandbox/scripts/`

Contrairement à OpenCode (qui perd le contexte à la fin de la session), nos agents ont une mémoire permanente sur le VPC.

---

## Synthèse

> **L'orchestration est LE truc important.** Sans elle, le sandbox est un terminal, le browser est un écran, Suparna est un lecteur de logs. Avec elle, ils deviennent des outils qu'un agent coordonne pour accomplir des tâches.
>
> **OpenCode nous apprend :** permissions per-agent, config en Markdown, délégation primaire→subagent.
> **OpenHands nous apprend :** Docker sandbox, automations par events, REST API pour agents.
> **Manus nous apprend :** Plan→Execute→Report, boucle d'observation, tâche autonome avec rapport.
>
> **Notre différence :** le LLM est local (Qwen), l'identité est souveraine (Dīpa), le déclencheur est SMS, le L2 est le téléphone, le sandbox est persistant. Aucun des trois projets n'a ça.
>
> **Par où commencer :** le tool registry (Phase 1) — c'est 100 lignes de Go, ça standardise tout, et ça rend les outils existants appellables par un LLM. Le reste découle.
