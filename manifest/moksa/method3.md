# Réflexions & Recherche Juillet 2026

## Réflexion 1 — Parallélisme de Tâches (pas de modèles)

**Constats**
- Anthropic pousse le modèle à produire une réflexion longue et profonde en un seul jet. Ça donne des résultats très pertinents au premier tirc est surmeent la voix cepdenatc'est pas adapté à la recherche en temps réel à voir...
- Pour de la recherche temps réel, il faut **obligatoirement des outils externes** : browser, sandbox, search. Le modèle seul ne peut pas inventer de l'information fraîche.
- Paralléliser 8 modèles sur la *même tâche* (Arena Mode de Grok) = redondant. Ça compense un modèle faible, pas l'approche optimale.
- Ce qui a du sens : **paralléliser des tâches différentes et indépendantes**.

**Principe**
```
Au lieu de :   8 × même modèle → même instruction → meilleur résultat
Faire :        1 modèle → DAG de tâches indépendantes → parallèle
               Chaque tâche a son propre contexte, son propre outil
               Les résultats fusionnent → évaluation → prochain batch
```

C'est exactement Mokṣa : le DAG capture naturellement les dépendances. Les tâches sans lien entre elles partent en parallèle. Chaque tâche utilise l'outil adapté (sandbox, browser, search, etc.), pas un clone du LLM.

**Avantage** : on scale horizontalement le nombre d'outils, pas le nombre de modèles. Moins cher, plus pertinent, résultats hétérogènes qui s'enrichissent mutuellement.

---

## Réflexion 2 — Parallélisme de Missions

**Constats**
- Une mission bien exécutée apporte le résultat attendu.
- Mais la recherche sans raccourcis (exploration libre) fait souvent découvrir des choses périphériques très intéressantes — qu'on n'aurait pas trouvées en allant droit au but.
- L'idée : **lancer la même mission avec des méthodes différentes** en parallèle, puis regarder les résultats des 2+ meilleures.

**Principe**
```
Mission : "trouve des vulnérabilités dans le module X"

Méthode A : approche boîte noire (browser, scan externe)
Méthode B : approche boîte blanche (sandbox, code review)
Méthode C : approche historique (git log, issues, commits)

Les 3 tournent en parallèle.
À la fin, on compare les résultats.
Découvertes périphériques = ce qui dépasse le cadre initial.
```

**Pourquoi c'est intéressant**
- Chaque méthode a ses angles morts. Ensemble, elles se couvrent.
- Les découvertes périphériques (ce qu'on ne cherchait pas mais qu'on trouve quand même) viennent souvent des frictions entre méthodes.
- On peut aussi lancer des missions identiques avec des **paramètres de conduite différents** (exploration vs exploitation, depth vs breadth).
- Le surcoût est contrôlé : chaque mission partage les mêmes outils et le même VPC.

**Risque** : explosion combinatoire. Solution : un **budget max de missions parallèles** (ex: 3). Et une mission "exploratrice" par défaut qui part toujours en parallèle de la mission principale.

---

## Recherche — Labs Juillet 2026

### Anthropic — L'ingénierie de boucle

**Claude Code "Goal"** : boucle agentique qui décompose, exécute et vérifie des sous-tâches automatiquement jusqu'à un critère de succès défini par l'utilisateur.

4 types de boucles officiels (blog Claude Code, juin 2026) :
1. **Turn-based** — classique question/réponse, une action à la fois
2. **Goal-based** — décomposition automatique, exécution autonome jusqu'au critère
3. **Time-based** — exécution programmée (/schedule)
4. **Proactive** — l'agent anticipe et propose des actions sans instruction

**"Claude écrit 80% de son propre code"** (juin 2026) :
- Productivité ×8 par ingénieur (Q2 2026 vs 2021-2024)
- Tâches autonomes de 12h (contre 4 min en mars 2024)
- Détection de bugs : aurait attrapé 1/3 des bugs de prod passés
- Le rôle humain passe de "codeur" à "reviewer"

**"Dive into Claude Code"** (arXiv, avril 2026) — analyse architecturale complète :
- Le système de permissions à 7 modes + classifieur ML
- Pipeline de compaction à 5 niveaux
- Sous-agents avec contextes isolés
- "Planning tool is basically a no-op whose value lies in keeping the agent on track"

### Google DeepMind — Sécurité et Scale

**AI Control Roadmap** (juin 2026) :
- Framework de défense en profondeur : D1-D4 (détection), R1-R3 (prévention)
- Prémisse : "alignment training cannot be assumed to guarantee safe behavior"
- ~1M tâches agentiques analysées en interne
- L'agent est traité comme un **insider threat potentiel** (comme un employé malveillant qui a déjà accès au bureau)

**"Towards a science of scaling agent systems"** (janvier 2026) :
- Étude de 180 configurations d'agents
- **"More agents is better" est réfuté** — le scaling plateau et peut même dégrader les performances
- 3 propriétés des tâches agentiques : interactions multi-step, collecte itérative d'info, adaptation stratégique
- Pas de domination sur tout le spectre : plus de raisonnement = moins d'efficacité

### Meta — Muse Spark + Squad Reasoning

**Muse Spark 1.1** (juillet 2026) — modèle agentique 1M tokens :
- #1 sur MCP Atlas, JobBench, Humanity's Last Exam
- Computer use (desktop, browser, mobile)
- **Parallel subagent delegation** natif
- Meta Model API (premier API payant Meta)
- $1.25/$4.25 par 1M tokens

**Squad Reasoning** — parallélisme par débat multi-agent :
- Plusieurs agents débattent en parallèle sur la même question
- Trade : intelligence brute → efficacité radicale
- Benchmark Harvey Legal Agent : 20% (Muse Spark) vs 11% (Fable)

**Meta-Agent** (arXiv, mai 2026) — génération automatique de systèmes multi-agents :
- Phase 1 : planification (DAG d'agents avec contrats d'entrée/sortie + critères de vérification)
- Phase 2 : exécution avec coordinateur + vérification à chaque étape
- Attribution d'erreur à 3 niveaux : locale, upstream, structurelle
- Stratégies de récupération : retry local → ré-exécution partielle → re-décomposition

### Tendances générales 2026

| Lab | Approche | Parallélisme | Innovation |
|---|---|---|---|
| **Anthropic** | Loop engineering + goal-based | Sous-agents séquentiels | Permission auto-mode, compaction 5 niveaux |
| **Google** | Multi-surface orchestration | Parallèle via Anti-gravity | AI Control Roadmap, D1-D4/R1-R3 |
| **Meta** | Squad Reasoning + Meta-Agent | Squad débat + subagent delegation | Génération auto de systèmes, vérification construction+execution |
| **OpenAI** | Agents SDK (handoffs) | Fan-out explicite | Guardrails, tracing, managed handoffs |

**Convergence :** Tout le monde arrive à la même conclusion en 2026 :
1. Le **harness** (ce qui entoure le LLM) est plus important que le modèle
2. Le parallélisme doit être **structuré** (DAG, dépendances), pas sauvage
3. La **vérification à chaque étape** est obligatoire — les erreurs propagées sont le vrai danger
4. La **gestion de contexte** (compaction, KV-cache, filesystem) est le goulot d'étranglement
5. "More agents is better" est un mythe — ce qui compte c'est la diversité des méthodes, pas le nombre de clones

## Convergence avec Mokṣa

| Notre reflexion | Confirmé par |
|---|---|
| Parallélisme de tâches (DAG) | Meta-Agent (DAG agent specs), Google (orchestration structurée) |
| Réévaluation après chaque batch | Anthropic "Goal" loop, Meta-Agent verification |
| Contexte mémoriel ≠ conversation | Manus (filesystem as context), OpenCode (compaction agent) |
| Missions parallèles avec méthodes différentes | Grok Arena Mode, Meta Squad Reasoning |
| Harness > Model | Tous les labs |

*Sources : anthropic.com/research, claude.com/blog, deepmind.google, ai.meta.com, arxiv.org (2604.14228, 2605.25233, 2605.14212, 2601.13671)*
