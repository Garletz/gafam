# Méthodes des Autres Agents — Même Pattern ?

## Oui, tous suivent le même squelette

```
while tâche pas finie :
  LLM réfléchit → choisit un outil
  Outil s'exécute
  Résultat observé → contexte
  Si besoin → retour au début
```

**Plan → Exécuter → Observer → Réfléchir → Re-planifier** (ReAct loop). C'est universel.

---

## 1. Grok Build (xAI) — Le Parallélisme

**Rust + TUI. Local-first. 8 sous-agents parallèles.**

- **Divide-and-Conquer** : chaque sous-agent travaille sur une partie différente du code
- **Arena Mode** : 8 agents résolvent le même problème en parallèle → on garde la meilleure solution
- **Plan Mode** : LLM génère un plan → l'utilisateur approuve/modifie → exécution parallèle
- **Model routing** : différentes tâches → différents modèles selon la complexité

**Secret sauce** : le parallélisme comme stratégie première. Compense un raisonnement moins profond par la largeur (8 tentatives simultanées).

---

## 2. OpenCode (opencode.ai) — La Sécurité

**TypeScript + serveur Hono. Boucle dans `session/prompt.ts`.**

- **Permission Ruleset** : la différence entre agents n'est pas le prompt mais les outils qu'ils peuvent voir. Agent Plan n'a pas les outils d'écriture.
- **9 stratégies de fallback pour `edit`** : du plus strict au plus permissif, le système réessaie avec une stratégie plus tolérante si la précédente échoue
- **Compaction agent** : un sous-agent dédié condense l'historique quand il devient trop grand
- **Doom loop detection** : détecte quand le LLM appelle le même outil avec les mêmes entrées 3+ fois de suite

**Secret sauce** : la sécurité par les outils disponibles (pas par les prompts). Les 9 fallbacks pour gérer le non-déterministe du LLM.

---

## 3. Manus (im) — Le Contexte

**Multi-agents dans une VM cloud. ~50 itérations par tâche.**

- **Context Engineering** : tout est optimisé pour le cache KV. Pas de timestamp. Append-only. Prefixes cohérents (`browser_`, `shell_`).
- **Tool-masking** : au lieu de supprimer/ajouter des outils (qui invalide le KV-cache), il masque les logits des noms d'outils pendant le décodage
- **Filesystem as context** : le système de fichiers = contexte illimité. Le modèle écrit/lit des fichiers à la demande. Stratégies de compression restorables (on garde l'URL, on jette le contenu).
- **todo.md injection** : un fichier réécrit constamment qui récite les objectifs à la fin du contexte. Évite le "lost-in-the-middle", garde le cap.
- **Keep failures** : les erreurs et stack traces sont gardées explicitement. Le modèle apprend de ses échecs.
- **Wide Research / Clone Fan-Out** : jusqu'à ~100 sous-agents complets lancés en parallèle.

**Secret sauce** : l'ingénierie du contexte comme philosophie première. "If model progress is the rising tide, we want Manus to be the boat." Le tool-masking préserve le cache. Le todo.md comme injection d'état debout.

---

## 4. OpenHands (ex OpenDevin) — L'Événement

**Python. Event stream. Docker sandbox. Architecture "Agent-Computer Interface".**

- **CodeAct** : au lieu d'appels d'outils, le LLM exécute du code Python/bash directement. 3 actions fondamentales : `CmdRunAction`, `IPythonRunCellAction`, `BrowseInteractiveAction`.
- **Event stream** : tout est un événement (actions, résultats, messages). Architecture **stateless** et **interruptible** — chaque étape est atomique, peut être mise en pause/reprise.
- **Condenser System** : quand la limite de tokens approche, compresse l'historique en un seul événement condensé.
- **Security Analyzer** : évalue le risque de chaque action avant exécution (bas/moyen/élevé).
- **Skills (microagents)** : des petits comportements spécialisés. Repo skills toujours actifs, knowledge skills activés par mots-clés.
- **Workspace abstrait** : le même agent peut tourner en local (LocalWorkspace), en Docker (DockerWorkspace), ou à distance (RemoteAPIWorkspace).

**Secret sauce** : l'event stream comme colonne vertébrale du système. CodeAct (code = action). Architecture complètement stateless et composable.

---

## En résumé : même pattern, différentes priorités

```
                    Plan ───→ Exécuter ───→ Observer
                      ↑                        │
                      └──── Réfléchir ←─────────┘
```

| Agent | Priorité #1 | Parallélisme | Gestion contexte | Réflexion |
|---|---|---|---|---|
| **Grok Build** | Largeur (8 agents) | ✅ Agressif | 256K-2M tokens | Hooks lifecycle |
| **OpenCode** | Sécurité (permissions) | ❌ Séquentiel | Compaction agent | Plugin Reflection-3 |
| **Manus** | Contexte (KV-cache) | ✅ Clone fan-out | Filesystem + todo.md | Keep failures |
| **OpenHands** | Événements (stateless) | ✅ Délégation | Condenser system | Security Analyzer |
| **Mokṣa (nous)** | Cycle réflexif structuré | ✅ DAG batché | Contexte mémoriel | LLM réévalue plan |

**Tous font pareil, mais chacun a trouvé un angle différent pour être meilleur :**
- Grok : plus de tentatives = meilleur résultat
- OpenCode : empêcher l'agent de déconner
- Manus : faire durer le contexte le plus possible
- OpenHands : tout est événement, tout est interruptible

**La leçon** : le secret n'est pas dans le modèle mais dans le **harness** — ce qui entoure le LLM. Deux agents sur le même modèle peuvent être complètement différents selon leur boucle, leurs outils, leur gestion de mémoire.

*Sources : github.com/xai-org/grok-build, opencode.ai/docs, manus.im/blog, github.com/All-Hands-AI/OpenHands*
