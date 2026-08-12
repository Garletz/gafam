# Agent Hall of Fame · 2026-08-12

> *Quand le film sera fini, on citera tous les agents qui ont fait des trucs exceptionnels.*

---

## L'Intention

Un rêve simple : pouvoir taper un SMS sur le client web, et voir le texte s'écrire **en temps réel** sur l'app du téléphone — comme Simplenote, mais pour les SMS. Et inversement.

L'infrastructure existait déjà : un VPC Go/SQLite, une APK Kotlin relais SMS, un frontend SvelteKit 5 sur Cloudflare Workers. Il fallait juste inventer la couche de synchronisation qui manquait.

---

## La Conception du Protocole

**Agent général · 09:17 UTC**

Mission : étudier les protocoles de sync temps réel existants (Simplenote/Simperium, Firebase, Signal) et proposer le meilleur pour notre cas.

**Move décisif** : L'agent a choisi le modèle **Last-Write-Wins avec timestamp versionné** — le même que Simperium, le protocole de Simplenote depuis 2008. Pas de WebSocket, pas de CRDT. Juste un `updated_at` en millisecondes qui fait office de juge de paix.

```
Chaque draft a un updated_at.
Chaque client stocke son lastKnownVersion.
Poll 1.5s : si serveur.updated_at > lastKnownVersion → l'autre a écrit → mettre à jour.
```

Simple. Robuste. Ça a tenu tout le long.

---

## La Chasse aux Bugs — 3 Agents Parallèles

**Agents déployés · 11:10 UTC**

Trois agents lancés simultanément : un sur l'APK, un sur le VPC, un sur le flux de données web. Ils ont lu l'intégralité du code source — 3000+ lignes de Kotlin, 1500 lignes de Go, 2400 lignes de Svelte.

**Résultat** : 66 bugs identifiés et classés, 52 corrigés dans la session.

| # | Fichier | Bug | Sévérité |
|---|---------|-----|----------|
| 1 | `SmsPanel.kt:99` | `setText()` appelé depuis un thread background → crash UI | CRITICAL |
| 2 | `ContactsPanel.kt:195` | `startActivity()` depuis un thread background → crash | CRITICAL |
| 3 | `+page.svelte:868` | `draftTimer` jamais remis à `null` → poll web bloqué définitivement | CRITICAL |
| 4 | `SmsPanel.kt:111` | `draftDirty` à `false` avant la réponse HTTP → race condition | HIGH |
| 5 | `+page.svelte:100` | `draftLoading` pas `$state` → guard anti-feedback loop bypassé | HIGH |
| 6 | `SmsPanel.kt:282` | Codes courts et senders alphanumériques (AMAZON, banques) ignorés | DATA LOSS |
| 7 | `SmsPanel.kt:523` | `deleteConversation` supprimait une seule variante d'adresse | DATA INCONSISTENCY |
| 8 | `api.go:474` | Anti-spam utilisait `LIKE %` ambigu → filtrage incorrect | SECURITY |

---

## Le Redesign UX — Inspiration QKSMS

**2 agents parallèles · 11:40 UTC**

L'APK avait une interface fonctionnelle mais brute. Deux agents ont été chargés de la repenser : un pour étudier les meilleures apps SMS open-source (QKSMS, Signal, Simple SMS Messenger), l'autre pour concevoir les specs exactes.

**Résultat** :
- Barre de saisie façon messagerie moderne : bulle arrondie 22dp, minHeight 44dp
- Bouton envoi → avec alpha progressif (0.4 grisé → 1.0 actif)
- Bulles de chat groupées par expéditeur, timestamps intégrés
- Compteur de caractères `/160`
- Sidebar rétractable, back stack navigation, badge SMS non lus
- Photos de contacts, favoris, pull-to-refresh, export vCard

---

## La Découverte du Bug Racine

**Agent d'analyse · 13:40 UTC**

Après 4 heures de debugging, 6 déploiements frontend, et des tests API qui prouvaient que l'infrastructure fonctionnait parfaitement, un agent a tracé le chemin exact du code, fonction par fonction, et a trouvé la cause.

**`+page.svelte:878`** — Dans l'effet Svelte 5 de changement de conversation :

```javascript
draftCache[lastDraftPeer] = outboxBody; // ← LECTURE → Svelte tracke cette variable
```

Svelte 5 enregistre `outboxBody` comme dépendance réactive. Quand l'utilisateur tape un caractère, `outboxBody` change → l'effet se RÉACTIVE → exécute `outboxBody = ''` (ligne 882) → le texte est effacé avant d'avoir pu être sauvegardé. Le brouillon tapé sur le web n'atteignait jamais le VPC.

**Le piège** : les lectures asynchrones (dans des callbacks `setTimeout`) ne sont PAS trackées par Svelte 5. C'est la lecture *synchrone* accidentelle qui a créé le bug.

**Fix** : `if (peer === lastDraftPeer) return;` — l'effet ne s'exécute que lors d'un vrai changement de conversation. Plus un `oninput` handler direct sur le textarea en backup, pour ne plus dépendre uniquement du framework.

---

## Credits

> *Quand le film sera fini.*

| Agent ID | Mission | Contribution |
|----------|---------|-------------|
| `ses_00aaf8f4` | Veille architecturale | A étudié Simperium, Firebase, Signal — a proposé le protocole de versionnage qui a tenu toute la session |
| `ses_00aa252f` | Bug hunt APK | A lu l'intégralité du code Kotlin, classé 30 bugs avec `file:line`, dont 2 crashes critiques |
| `ses_00aa2410` | Bug hunt VPC | A audité le serveur Go : SQLITE_BUSY, WAL checkpoint, orphelins outbox, encryption |
| `ses_00aa2249` | Bug hunt Web | A tracé le flux complet : race conditions, memory leaks, draft cache, version tracking |
| `ses_00a960cb` | Fix APK batch | 11 corrections : retry SMS, OOM protection, delete multi-adresses, draft timer |
| `ses_00a95dfb` | Fix VPC batch | 10 corrections : ms `updated_at`, pagination SQL, orphelins outbox, ALTER TABLE migrations |
| `ses_00a95aed` | Fix Web batch | 8 corrections : draft cache par conversation, poll cleanup, version tracking |
| `ses_00a85a62` | UX SMS | Plan complet inspiré de QKSMS : specs exactes, wireframes ASCII, priorités |
| `ses_00a85960` | UX Contacts | Data model multi-numéros, photos système, favoris, détails, appels |
| `ses_00a85800` | UX Navigation | Back stack, sidebar rétractable, badges, animations |
| `ses_00a6f444` | Root cause | **La découverte décisive** : le bug Svelte 5 `$effect` reactivity |
| `ses_00a839f6` | Implémentation SMS UX | Bulles groupées, timestamps, typing indicator, send animation |
| `ses_00a8372d` | Implémentation Contacts UX | Photos, favoris, pull-to-refresh, barre rétractable, thème dark |
| `ses_00a3e86f` | Vérification VPC | Tests end-to-end : web PUT → VPC → APK GET, confirmation HTTP 200 |
| `ses_00a3e73b` | Trace Web | A tracé le chemin exact du code, fonction par fonction, découvert la lecture synchrone accidentelle |
| `ses_00a3e630` | Vérification APK | Capture logcat en direct : `Updated from remote: WEB-DRAFT-TEST` |
| `ses_00a3e4fb` | Synthèse | A croisé les findings de tous les agents pour confirmer le diagnostic |

---

## Chiffres de la Session

| Métrique | Valeur |
|----------|--------|
| Agents déployés | 17 |
| Heures de session | ~5h |
| Fichiers modifiés | 12 |
| Bugs trouvés | 66 |
| Bugs corrigés | 52 |
| Builds APK | 11 |
| Déploiements frontend | 9 |
| ~2000 lignes modifiées | Go · Kotlin · TypeScript · Svelte |
| Stack | SvelteKit 5 · Cloudflare Workers · Go · SQLite · Kotlin · ADB |

---

## Le Mot de la Fin

> Le live draft sync est devenu réalité. Ce qui était un concept abstrait le matin — taper sur le web et voir le texte apparaître sur le téléphone — est devenu une feature concrète le soir. Et quand le créateur a tapé « ça marche u feu de dieu », on a su que la mission était accomplie.

---

*Session du 12 août 2026 · Projet GAFAM · गाफाम*
