# Mokṣa — Méthode 4 · Tableau de Quêtes (PMD Sky)

> **Statut :** méta-concept + **implémentation v1** (Organic Tools / Quests). Poseur heuristique ; L1 Qwen plus tard.
> **Inspiration visuelle :** Pokémon Donjon Mystère — Explorateurs du Ciel (*Mystery Dungeon: Sky*) — le tableau des missions / Job Bulletin Board.
> **Lien :** méthode 1 (Mission + DAG), Karaka (organes / acteurs), Saṃyojaka (orchestre).

---

## L’idée en une phrase

> **Une demande n’ouvre pas une conversation outil-par-outil. Elle pose un tableau de quêtes anticipé. Les organes GAFAM (kāraka) se saisissent des cases. Chaque case reçoit un verdict (récompense = filtre). Un superviseur suit, valide, rajoute. Quand le panneau est assez plein, on synthétise la réponse dans un environnement (sandbox / rapport).**

---

## Pourquoi ce n’est pas « encore une ReAct loop »

| Approche classique (OpenCode, Manus, method1 nue) | Tableau de Quêtes |
|---|---|
| LLM choisit **un** outil → observe → choisit le suivant | LLM **anticipe** un panneau multi-quêtes dès le départ |
| L’état vit dans le chat / l’historique | L’état vit sur un **tableau visible** (contexte mémoriel) |
| Séquentiel (même si batch DAG ensuite) | Spatial : plusieurs quêtes **affichées**, **revendiquées**, **suivies** |
| L’humain lit des logs d’agent | L’humain lit un **board de missions** (PMD Sky) |
| Les sous-agents sont techniques | Les sous-agents sont des **organes nommés** (Suparna, Vātāyana, Yantraśālā…) |

La différence de situation : la demande initiale n’est pas « la tâche ». C’est le **générateur du tableau**. Le but réel = l’ensemble des quêtes adjacentes + leur synthèse.

---

## Flux

```
1. RÉCEPTION (avant même l’instruction utile)
   │
   │  Injection d’un MD/TXT léger = « carte du monde GAFAM »
   │  → organes, outils, permissions, but du nœud, règles Mokṣa
   │  (contexte rapide pour Qwen léger OU Qwen profond · 1 Go RAM)
   │
   ▼
2. DEMANDE
   │  User / SMS / message / intent
   │
   ▼
3. POSE DU TABLEAU (LLM)
   │  Anticipation : découpe la demande en quêtes
   │  Chaque quête = but partiel, outil probable, estimation, dépendances
   │  = le Job Bulletin Board
   │
   ▼
4. REVENDICATION (Kāraka / organes)
   │  Chaque agent Sanskrit regarde le tableau :
   │  « Cette case, c’est moi — outil X — résultat dans ~Ys »
   │
   ▼
5. SUIVI TEMPS RÉEL
   │  Panneau live : pending / running / done / failed
   │  Résultats rentrent dans le contexte global du board
   │
   ▼
6. RÉCOMPENSE (verdict de quête — pas XP)
   │  Judge léger (ou humain) pose sur chaque case terminée :
   │  done | failed | needs_more + score 0–1 + reason
   │  → ce verdict **filtre** la suite (pas un score cosmétique)
   │
   ▼
7. SUPERVISION
   │  Le premier superviseur (humain ou L2 léger) lit les verdicts :
   │  - done → case close
   │  - failed → retry ou abandon
   │  - needs_more → **ajoute une nouvelle case** sur le board
   │  - ou annule / fusionne
   │
   ▼
8. SYNTHÈSE
      Quand assez de verdicts done : les quêtes adjacentes à la demande
      initiale sont assemblées → environnement de réponse
      (sandbox files, rapport, SMS, carte front…)
      Option : Best-of-N sur 2 synthèses → garder celle au meilleur score.
```

---

## La couche « carte du monde » (pré-prompt)

Avant le prompt de la demande, le modèle reçoit une structure **légère** (MD ou TXT), pas un dump de code :

```markdown
# GAFAM Node — world card
## Organs (Karaka)
- Suparna L1 : lit logs, synthétise, suggère
- Edge L2 : raisonnement profond (phone)
- Vātāyana : fenêtre web (browser.*)
- Yantraśālā : établi (sandbox.exec, files, storage)
## Tools registry
- browser.status | screenshot | input
- sandbox.exec | file_list | file_read | file_write | storage
## Rules
- Human keeps the baton (ask / allow / deny)
- Quest board = memory, not chat history
- Claim → execute → report to board
## Purpose
Personal node that acts on digital life — not a coding IDE.
```

**Pourquoi avant la demande :** un modèle faible (ou 1 Go RAM) a besoin d’un **monde stable** déjà chargé. La demande n’explique pas GAFAM à chaque fois — elle arrive *dans* GAFAM.

---

## Structure du tableau (concept)

```
┌──────────────────────────────────────────────────────────────────────┐
│  QUEST BOARD · Mission #m42                                           │
│  Demande initiale : « Vérifie ce lien SMS et dis-moi si OK »          │
├──────────┬──────────┬─────────┬──────┬──────────┬─────────────────────┤
│ Quête    │ Organe   │ Outil   │ ETA  │ Statut   │ Récompense          │
├──────────┼──────────┼─────────┼──────┼──────────┼─────────────────────┤
│ Q1 Lire  │ Suparna  │ logs /  │ 5s   │ done     │ done · 0.9          │
│    SMS   │          │ state   │      │          │ « URL extraite »    │
│ Q2 Ouvrir│ Vātāyana │ browser │ 20s  │ done     │ needs_more · 0.4    │
│    URL   │          │ .input  │      │          │ « login wall »      │
│ Q3 Capt. │ Vātāyana │ screen- │ 3s   │ done     │ done · 0.8          │
│          │          │ shot    │      │          │                     │
│ Q4 Juger │ Edge L2  │ (raison)│ 30s  │ pending  │ —                   │
│    risque│/Suparna  │         │      │          │                     │
│ Q5 Auth? │ Vātāyana │ browser │ 15s  │ pending  │ (ajoutée car Q2     │
│          │          │ .input  │      │          │  needs_more)        │
└──────────┴──────────┴─────────┴──────┴──────────┴─────────────────────┘
```

Visuel PMD Sky : cases de mission, équipe qui choisit, progression, retour à la base pour le rapport — **plus une colonne verdict** qui dit si la case avance vraiment le but.

---

## Mapping GAFAM (déjà là vs à inventer)

| Élément PMD Sky | Équivalent GAFAM |
|---|---|
| Tableau des missions | Mission / Quest Board (UI + contexte mémoriel) |
| Équipe Pokémon | Kāraka / organes (Suparna, Vātāyana, Yantraśālā, Edge) |
| Arme / talent du monstre | Tool registry (`browser.*`, `sandbox.*`…) |
| Prendre une mission | Claim : organe se déclare sur une quête |
| Explorer le donjon | Exécution outil + observation |
| Retour au hub | Résultat écrit sur le board |
| Réussite / échec de mission | **Récompense** : verdict `done\|failed\|needs_more` (+ score) |
| Chef d’équipe | Superviseur (humain d’abord, L2 ensuite) — lit les verdicts |
| Rapport de fin | Synthèse → sandbox / carte / SMS (Best-of-N optionnel) |

Ce n’est **pas** idiot : c’est method1 (DAG + réévaluation) rendu **spatial, nommé, et pédagogique**. Le jeu donne la métaphore UI ; Mokṣa donne le moteur.

---

## Ce qui est fort

1. **Visuel = compréhension.** L’user n’est pas idiot : un board de quêtes se lit mieux qu’un stream de tool_calls.
2. **Anticipation.** On force le modèle à *poser le terrain* avant d’agir — moins de wander aléatoire.
3. **Organes revendiquent.** Au lieu d’un LLM unique qui joue tous les rôles, chaque kāraka dit « c’est mon outil ». Aligné Karaka + permissions.
4. **Supervision naturelle.** Valider / ajouter une case = le pipeline suggest→approve du manifeste 25, mais sur un panneau.
5. **Contexte pour petit modèle.** World-card MD avant la demande = harness adapté à 1 Go / Qwen léger.
6. **Synthèse finale dans l’environnement.** La réponse n’est pas que du texte chat — elle matérialise (fichiers sandbox, rapport, état).
7. **Récompense = filtre visible.** Chaque case porte un verdict ; `needs_more` étend le board au lieu d’empiler du chat.

---

## La récompense (= filtre de trajectoire)

Kimi / Anthropic (et la littérature RLHF / process reward) ont montré que **pendant l’entraînement**, la récompense est la clé : elle **modifie les poids**.

En **mode réponse**, écrire `+10` sur le board ne réentraîne rien. Sur le tableau de quêtes GAFAM, on garde l’esprit « reward », mais on le détourne correctement :

> **Récompense = verdict de quête qui change le panneau.**  
> Pas d’XP RPG. Pas de karma cosmétique. Un filtre.

### Schéma par case

```
reward:
  verdict: done | failed | needs_more
  score:   0.0–1.0              # optionnel — judge L1 léger
  reason:  "screenshot shows login wall"
```

### Comment ça boucle (exemple)

1. Vātāyana exécute Q2 (ouvrir URL) → résultat : page de login.
2. Judge : `needs_more · 0.4 · "login wall"`.
3. Superviseur lit le verdict → **ajoute Q5** « tenter auth / autre chemin ».
4. Plus tard, assez de `done` → synthèse dans le sandbox / rapport.
5. Option synthèse : 2 drafts → scorer → **Best-of-N** (reward à l’inférence = sélection).

### Tableau d’usages

| Usage | Sens | Effet réel |
|---|---|---|
| Points / karma (`Suparna +5`) | Faible | Décor. Placebo. |
| Verdict + reason → superviseur | **Fort** | Valide, retry, ou **nouvelle quête**. |
| Score process (sortie utile pour *cette* quête ?) | **Fort** | Alimente la réévaluation method1. |
| Best-of-N sur la synthèse finale | **Fort** | Sélection de trajectoire, pas RL. |
| Langage score dans le prompt (modèle RL-habitué) | Moyen | Aide format / auto-critique, pas la magie. |

### Ce qu’on refuse

- XP décoratif sans sélection derrière
- « Mission réussie » globale trop tôt (avant les verdicts des quêtes adjacentes)
- Confondre train-time RL et prompt-time `+10`

### Lien method1

Method1 réévalue après chaque batch. Ici, la récompense **est** cette réévaluation, rendue visible sur le board : chaque case porte son verdict ; le superviseur ne lit pas un mur de logs, il lit des **filtres**.

---

## Ce qui est fragile (à cadrer)

1. **Explosion du board.** Trop de quêtes anticipées = bruit. Budget max (ex. 5–8 cases, +N ajoutées par le superviseur).
2. **Fausse anticipation.** Le modèle invente des quêtes inutiles. Le superviseur doit pouvoir *élaguer* vite.
3. **Qui claim ?** Règle claire : matching organe↔outil (Vātāyana ne claim pas `sandbox.exec`), sinon chaos.
4. **Double cerveau.** Superviseur + poseur de tableau = 2 appels LLM. Sur 1 Go, séquencer : light pose → claims → heavy judge seulement si besoin.
5. **Pas magique.** Sans tools réels et sans registry Karaka, le board n’est qu’une jolie todo list.

---

## Position vs méthodes 1–3

| | Method 1 | Method 4 (ce doc) |
|---|---|---|
| Cœur | Mission + Step DAG + réévaluation | Même moteur, **métaphore + UX + claim + verdict** |
| Vue | API / struct Go | Tableau de quêtes type PMD |
| Acteurs | Steps anonymes `s1,s2` | Organes Sanskrit qui revendiquent |
| Entrée LLM | Plan prompt | World-card **puis** demande **puis** méthode board |
| Récompense | Implicite (réévaluation LLM) | **Explicite** : `done\|failed\|needs_more` sur chaque case |
| Sortie | Réponse assemblée | Synthèse dans un **environnement** (sandbox…) |

Method 2–3 disent : tout le monde fait Plan→Exec→Observe ; le harness compte.  
Method 4 dit : **notre harness GAFAM, c’est le tableau de quêtes + les organes nommés.**

---

## Verdict

**Pas idiot.** C’est même probablement la meilleure *peau* pour Mokṣa dans GAFAM :

- fidèle à l’orchestration 2026 (DAG, vérif, parallélisme de tâches),
- spécifique à nous (organes Sanskrit, nœud perso, LifeTools / Karaka),
- compréhensible par un humain (board de quêtes),
- viable avec un petit modèle si la world-card reste courte.

À ne pas faire : coder le jeu.  
À faire : traiter PMD Sky comme **métaphore produit** — le board est l’UI du contexte mémoriel de la Mission.

---

## Prochaine brique (quand on sort du méta)

1. Schéma JSON d’une `Quest` (id, title, organ_hint, tool, depends_on, status, claim, eta, reward{verdict,score,reason}).
2. World-card MD versionnée (≤ 1–2 Ko).
3. Une maquette UI board (sans LLM) branchée sur une Mission factice — colonnes statut + verdict visibles.
4. Puis : poseur de tableau (L1) → claims → exec Karaka → judge/reward → superviseur.

---

## Implémentation v1 (Organic Tools)

> **Statut code :** v1 livrée — poseur heuristique (pas encore Qwen L1).

| Couche | Fichiers |
|---|---|
| API Mission | `vpc-relay/moksa/` (`mission.go`, `store.go`, `pose.go`, `executor.go`, `handlers.go`, `worldcard.md`) |
| Routes | `vpc-relay/main.go` → `/api/web/mission*` |
| Proxy CF | `frontend/src/routes/api/proxy/mission/+server.ts`, `…/karaka/+server.ts` |
| UI | `frontend/src/lib/QuestBoard.svelte` — onglet **Quests** (premier) dans Organic Tools |

Flux UI : Pose board → Claim → Run (karaka) → Reward (`done` / `failed` / `needs_more`) → Add quest → Synthesize (`/files/missions/{id}/report.md`).

