# Mokṣa — Méthode de Résolution Réfléchie

## Principe

Une instruction arrive (user ou VPC) → Mokṣa la résout via un cycle **Plan → Parallélise → Observe → Réévalue → Ajuste → Libère**.

## Architecture

```
Instruction
    │
    ▼
┌──────────────────────────────────────────────────────────┐
│  1. GÉNÉRATION DU PLAN (LLM)                              │
│     L'instruction → LLM produit un plan multi-étapes      │
│     Chaque étape = Step { tool, input, depends_on }       │
│     Le LLM estime le temps par étape                      │
└────────────────────────────────┬─────────────────────────┘
                                 │
                                 ▼
┌──────────────────────────────────────────────────────────┐
│  2. EXÉCUTION BATCHÉE                                     │
│     Résout le DAG de steps :                               │
│     - steps sans dependance → parallèle (goroutines)      │
│     - chaque step a un timeout (estimated_ms × 2)         │
│     - watcher goroutine collecte les résultats            │
└────────────────────────────────┬─────────────────────────┘
                                 │
                                 ▼
┌──────────────────────────────────────────────────────────┐
│  3. OBSERVATION + RÉÉVALUATION (LLM)                     │
│     Résultat de chaque step → LLM évalue :                │
│     - étape réussie ? besoin de corriger ?                │
│     - le plan est toujours pertinent ?                    │
│     - faut-il ajouter/enlever/réordonner des steps ?      │
│     Si ajustement → retour en 2. Sinon → 4.              │
└────────────────────────────────┬─────────────────────────┘
                                 │
                                 ▼
┌──────────────────────────────────────────────────────────┐
│  4. FINALISATION                                          │
│     Résultat final assemblé → réponse à l'utilisateur    │
│     Mission archivée (contexte mémoriel, pas le chat)     │
└──────────────────────────────────────────────────────────┘
```

## Structure de Données

### Mission (état courant, pas dans la conversation)

Stockée dans le **contexte mouvant** du VPC, accessible via API REST. Le LLM reçoit un résumé markdown, pas l'objet brut.

```go
type Step struct {
    ID          string        // "s1", "s2"...
    Tool        string        // "sandbox.exec", "browser.screenshot"...
    Input       any           // paramètres de l'outil
    DependsOn   []string      // IDs des steps prérequis
    Status      string        // pending | running | done | failed | cancelled
    Result      any           // sortie de l'outil
    Error       string        // si failed
    EstimatedMs int           // temps estimé par le LLM
    StartedAt   time.Time
    CompletedAt time.Time
}

type Mission struct {
    ID          string
    Instruction string        // la demande originale
    Steps       []Step        // tout le DAG
    Summary     string        // résumé markdown du plan (pour le LLM)
    Status      string        // planning | executing | evaluating | done | failed
    TurnCount   int           // nombre de boucles réévaluation
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

### API

```
POST   /api/v1/mission              → créer une mission + lancer l'orchestrateur
GET    /api/v1/mission/{id}         → état actuel (polling frontend)
GET    /api/v1/mission/{id}/stream  → SSE pour suivi live
DELETE /api/v1/mission/{id}         → annuler
```

### Persistance (optionnelle)

- `/workspace/.missions/{id}.json` — reprise après crash VPC
- En mémoire sinon (mutex + map)

## Parallélisation

Les steps `depends_on: []` (ou dont les dépendances sont résolues) partent en **parallèle**.

Exemple :
```json
s1: sandbox.exec("whoami")          → depends_on: []
s2: browser.screenshot("https://x") → depends_on: []
s3: sandbox.exec("ls -la")          → depends_on: ["s1"]
s4: sandbox.write("result.md", ...) → depends_on: ["s1", "s2"]
```

`s1` et `s2` simultanés. `s3` attend `s1`. `s4` attend `s1` + `s2`.

Chaque step a un **timeout = estimated_ms × 2** (minimum 5s). Si timeout → `failed` avec `error: "timeout"`.

## Boucle de Réévaluation

Après chaque **batch** de steps terminé, le LLM reçoit :

```
Contexte :
- Plan original (summary)
- Résultats des steps complétés
- Erreurs éventuelles

Tâche du LLM :
1. Les résultats sont-ils cohérents ?
2. Faut-il ajuster les steps restants ?
3. Faut-il ajouter des steps de correction ?
4. Le plan est-il toujours valide ou faut-il tout re-planifier ?
5. Si tout est fait → finaliser
```

Si le LLM demande des ajustements → nouveau batch. Sinon → finalisation.

## Contexte Mémoriel vs Conversation

| Aspect | Conversation | Mission (contexte mouvant) |
|---|---|---|
| Stockage | Table messages | Struct Go mutexée + fichier |
| Cycle de vie | Permanent | Naît et meurt avec la tâche |
| Contenu | Historique complet | État actuel + résumé |
| Accessible par | LLM + user | LLM (via injection) + API |
| Taille | Croissante | Fixe, contrôlée |

**Pourquoi pas dans la conversation :**
- La conversation grossit à chaque tour → coûteux en tokens
- Le plan change → dur de modifier l'historique
- Parallélisation impossible (la conversation est séquentielle)
- Le contexte mémoriel permet au LLM de voir l'état actuel proprement sans bruit historique

## Implémentation

### Fichiers

```
vpc-relay/karaka/
├── mission.go          → types Mission, Step, constructeurs
├── orchestrator.go     → boucle principale Plan→Exec→Evaluate
├── executor.go         → lancement parallèle des steps + watcher
├── plansmith.go        → LLM prompt pour générer le plan initial
├── evaluator.go        → LLM prompt pour réévaluer après batch
└── handlers.go         → routes API /api/v1/mission/*
```

### Dépendances

- `karaka/registry.go` — résolution tool → handler
- `karaka/tools.go` — exécution des outils
- Go routines + `sync.WaitGroup` pour parallélisation
- `time.After` pour timeouts

## Flux Complet (exemple)

1. User : « déploie le dossier X sur le VPS et teste-le »
2. LLM → Plan : [s1: sandbox.exec("tar -czf ..."), s2: sandbox.exec("scp ..."), s3: sandbox.exec("ssh ... test")]
   - s1 et s2 parallélisables ? Oui si indépendants
   - s3 dépend de s1 + s2
3. Batch 1 : s1 + s2 en parallèle, watcher attend les deux
4. Résultats → LLM réévalue : s1 OK, s2 OK, s3 peut partir
5. Batch 2 : s3 seul
6. Résultat → LLM : tout bon
7. Finalisation → réponse à l'utilisateur
