# 27. Dakṣiṇā — दक्षिणा · l'offrande qui fait grandir le logiciel

> **Statut :** manifeste de vision — le modèle économique du projet.
> **Prérequis :** [Saṃyojaka](25_samyojaka_agent_orchestrator.md) (l'orchestrateur), [le Vault] (la mémoire de recherche), le pipeline d'approbation humaine.
> **Lien vision :** le métabolisme du [Nœud Personnel](19_personal_node.md) — non pas l'usage du logiciel, mais sa croissance.
> **Précédent :** [Saṃyojaka](25_samyojaka_agent_orchestrator.md) orchestre les kāraka à l'intérieur du nœud. Dakṣiṇā orchestre la croissance du nœud lui-même.

---

## Pourquoi « Dakṣiṇā »

**Dakṣiṇā** (दक्षिणा) est, dans la tradition indienne, l'offrande faite au maître — le don qui circule dans un sens unique, de celui qui reçoit l'enseignement vers celui qui le donne. Ce n'est pas un prix, pas un paiement, pas une transaction : c'est une offrande qui honore une relation et permet au maître de continuer à enseigner.

Ici, le maître, c'est le logiciel lui-même. Et l'offrande devient sa nourriture : des crédits pour son propre artisan.

> *Dakṣiṇā n'achète pas une fonctionnalité. Dakṣiṇā nourrit le logiciel pour qu'il grandisse de lui-même.*

---

## Le constat en une phrase

> **GAFAM est quasi-autonome à l'usage — un SMS déclenche une recherche complète. Mais sa croissance dépend encore entièrement du temps d'un seul développeur. Un logiciel souverain qui ne peut pas se développer lui-même n'est souverain qu'à moitié.**

Le projet a un système nerveux (Saṃyojaka), une mémoire (le Vault), des mains (Yantraśālā), des yeux (Vātāyana), une voix (le relais SMS). Il lui manque un **métabolisme** : une manière durable de convertir de la valeur en croissance, sans entreprise, sans levée de fonds, sans dépendance au temps d'une personne.

---

## Le concept

Une page publique — `daksina.gafam.cloud` — séparée de toute infrastructure personnelle :

```
┌─────────────┐     offrande      ┌──────────────────┐
│  Visiteur    │ ───────────────→ │  Page Dakṣiṇā     │
│  (humain)    │                  │  (publique)       │
└─────────────┘                  └────────┬─────────┘
                                          │ crédits
                                          ▼
                              ┌───────────────────────┐
                              │  Kāraka codeur         │
                              │  (OpenCode + Kimi K3)  │
                              │                        │
                              │  lit: le repo ENTIER   │
                              │       les issues       │
                              │       l'avis des devs  │
                              │       les retours      │
                              │       le vault         │
                              └────────┬───────────────┘
                                       │ PR (jamais de push direct)
                                       ▼
                              ┌───────────────────────┐
                              │  Revue humaine         │
                              │  (le nœud personnel    │
                              │   garde l'autorité)    │
                              └────────┬───────────────┘
                                       │ merge
                                       ▼
                              Logiciel meilleur
                              → plus d'utilisateurs
                              → plus d'offrandes
```

1. **Un visiteur fait une offrande** — montant libre, une fois ou récurrent.
2. **L'offrande devient des crédits** pour le kāraka codeur (OpenCode headless + Kimi K3 ou successeur).
3. **Le kāraka travaille dans la limite des crédits** : il lit le repo complet (vpc-relay, APK, gafam-manager, frontend, manifests), les issues GitHub, l'avis des devs principaux, les retours utilisateurs — puis il écrit des améliorations et ouvre des pull requests.
4. **L'humain merge ou refuse.** Toujours. Le kāraka propose, l'humain dispose.

---

## Ce que le kāraka codeur lit (ses sources de vérité)

| Source | Rôle |
|---|---|
| Le repo entier | le terrain : Go, Kotlin, Rust/Tauri, Svelte, Python, manifests |
| GitHub issues | la demande brute, triée par le kāraka en missions typées (bugfix, feature, docs, review) |
| L'avis des devs principaux | la direction — une page où les mainteneurs écrivent leurs priorités |
| Les retours utilisateurs | formulaire public minimal (pas de compte, pas de tracking) |
| **Le Vault** | la mémoire des revues et décisions passées — le kāraka ne repose pas une question déjà tranchée, ne repropose pas un changement déjà refusé |

---

## Les garde-fous — la souveraineté s'applique à elle-même

1. **Jamais de push direct sur `main`.** PR uniquement, revue humaine obligatoire. C'est le pipeline `ask` du manifest 25, appliqué au code.
2. **Budget plafonné par période.** Les crédits s'épuisent ; le kāraka s'arrête. Pas de dérive infinie.
3. **Journal public des dépenses.** Chaque offrande, chaque crédit consommé, chaque PR — visible sur la page. La transparence est la condition de la confiance dans l'offrande.
4. **Les mêmes barres de qualité que les humains.** `go test`, `svelte-check`, build vert — une PR qui ne passe pas ne se propose pas.
5. **Charte de non-régression souveraine.** Aucune modification qui réduise la souveraineté des utilisateurs : pas de télémétrie, pas de dark pattern, pas de dépendance fermée nouvelle. Cette charte est dans le prompt système canonique du kāraka — *elle est gospel*.
6. **Dogfooding intégral.** Le kāraka codeur tourne avec les outils GAFAM : sandbox pour les builds, Vault pour la mémoire, missions research pour comprendre une issue avant d'y toucher. Le projet se développe avec ses propres organes — c'est la preuve vivante qu'ils fonctionnent.

---

## Architecture esquisse

- **La page** : statique, worker Cloudflare séparé, **aucun accès aux VPC des utilisateurs**. L'offrande ne touche jamais l'infrastructure personnelle de quiconque.
- **Le paiement** : Stripe / Ko-fi / GitHub Sponsors → webhook → table `daksina_credits` (solde, seuil de déclenchement, journal).
- **Le runner** : OpenCode headless sur une machine du projet (pas le VPC d'un utilisateur), déclenché quand `crédits > seuil`, une mission à la fois — exactement le mutex de Saṃyojaka.
- **Les sorties** : PRs GitHub, changelog public, et chaque mission archivée dans le Vault (`/files/research/missions/daksina-*/`) — la mémoire de la croissance, consultable par tous.

---

## Ce que Dakṣiṇā n'est pas

- **Pas une DAO, pas un token, pas un investissement.** Une offrande ne donne droit à rien — pas de vote, pas de retour, pas de promesse. Comme la dakṣiṇā traditionnelle : on donne parce que la chose mérite d'exister.
- **Pas une délégation de gouvernance.** L'humain garde le dernier mot sur chaque ligne. Le jour où personne ne relit les PRs, le kāraka s'arrête — c'est écrit dans ses règles.
- **Pas une course aux fonctionnalités.** Stabilité, souveraineté, sobriété d'abord. Un kāraka qui ajoute du bruit est un kāraka mal réglé.

---

## La mesure du succès

- **Taux de merge** : % de PRs du kāraka acceptées sans modification majeure. Cible : > 60 % en montée continue (le Vault fait baisser les malentendus).
- **Coût par amélioration** : crédits dépensés / PR mergée. Doit **diminuer** avec le temps — la mémoire compose.
- **Le goulot se déplace** : quand l'écriture n'est plus le problème, c'est l'approbation humaine qui devient la limite. *Ce jour-là, le logiciel se développe lui-même — sous l'autorité de l'humain, comme tout le reste du projet.*

---

## Lien avec les autres manifestes

- **[25. Saṃyojaka](25_samyojaka_agent_orchestrator.md)** — le pipeline suggestion → approbation → exécution → rapport devient le cadre des PRs.
- **[26. Orchestration](26_orchestration_comprehension.md)** — Dakṣiṇā applique l'orchestration au repo lui-même : le cas d'usage le plus exigeant et le plus utile.
- **[19. Nœud Personnel](19_personal_node.md)** — le nœud grandit par l'offrande, pas par le capital. Souveraineté économique = souveraineté tout court.
- **Le Vault** — la mémoire des décisions : un kāraka qui se souvient ne refait pas les erreurs passées.

---

*यद्दक्षिणा स्वयं वर्धते। — Là où l'offrande grandit d'elle-même, le logiciel aussi.*
