# 18. Le Ghost Clone — Miroir sémantique du téléphone

> **⚠️ BROUILLON — Vision exploratoire. Ne pas prendre au sérieux pour l'implémentation actuelle.**
> Pistes de réflexion (clone sémantique, logcat, LLM, APK sensoriel). Rien de normatif tant qu'une direction n'est pas validée.

---

## L'idée en une phrase

> **Le téléphone reste le corps. Le VPC héberge son fantôme — un clone vivant, navigable, utilisable depuis l'interface web, pas uniquement une base de données qui se synchronise en silence.**

---

## Le problème qu'on refuse

Aujourd'hui, GAFAM fait déjà beaucoup — et on ne le minimise pas :

| Ce qu'on a | Ce que ça fait | Ce que le Web Client montre déjà |
| :--- | :--- | :--- |
| **APK Relay + Web Client** | Sync SMS, contacts, outbox → SQLite → interface web | Conversations, contacts, envoi SMS, paramètres, gardiens — **une vraie UI messagerie** sur `gafam.cloud` |
| **Scrcpy Bridge** (manifest 14) | Stream H.264 pixel par pixel | Onglet Remote Control — écran Android brut, session lourde |

L'APK Relay n'est **pas** une DB muette. Le frontend **reproduit** la messagerie sur le web : ce n'est pas SQLite exposé tel quel, c'est une interface utilisable. Ça fonctionne, et c'est le cœur actuel du produit.

**Ce qui manque encore** n'est pas « une interface » — c'est **le reste du téléphone** :

- Les apps tierces (Signal, WhatsApp, banque, 2FA…)
- L'arbre UI complet (écrans, boutons, états)
- Les notifications hors SMS
- Agir dans n'importe quelle app sans scrcpy
- Une présence permanente du **device entier**, pas seulement du canal SMS

Pour ouvrir Signal et taper un code (manifest 5 cas 2), aujourd'hui il faut soit être au téléphone, soit lancer scrcpy. La messagerie SMS native, elle, est déjà sur le web.

**Le Ghost Clone comble cet écart** : étendre ce qu'on fait déjà pour la messagerie à **l'ensemble du téléphone** — une représentation permanente, légère et actionnable, sans streamer des pixels.

---

## Le principe : double sémantique, pas double pixel

Un écran Android à 60 fps, c'est de l'image. Un **arbre UI** — la hiérarchie des vues, les textes, les boutons, l'état des apps — c'est de la **sémantique**.

```
Scrcpy (manifest 14)     :  ████████████████████  2–5 Mbps  (pixels)
Ghost Clone              :  ░░░░░░░░░░░░░░░░░░░░  ~10 Kbps  (structure)
```

Le clone ne reproduit pas l'écran. Il reproduit **ce que l'écran signifie** : quelles apps sont ouvertes, quels messages sont visibles, quels boutons existent, quelle notification vient d'arriver. L'interface web **reconstruit** une vue utilisable à partir de cet arbre — pas une vidéo, une **présence**.

---

## Architecture globale

```
┌─────────────────────────────────────────────────────────────────┐
│  TÉLÉPHONE (le corps — runtime réel)                             │
│  APK Relay + compagnon                                           │
│  SIM · Play Services · capteurs · apps · écran physique          │
└───────────────────────────┬─────────────────────────────────────┘
                            │  ADB / tunnel chiffré (léger)
                            │  UI tree · fichiers · notifs · SMS
                            │  logcat · dumpsys · état apps
                            ▼
┌─────────────────────────────────────────────────────────────────┐
│  VPC — LE GHOST CLONE (le fantôme — présence utilisable)         │
│                                                                  │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────────┐  │
│  │ État sémantique│  │ Clone Store │  │ API + Web UI légère  │  │
│  │ (arbre UI live)│  │ (fichiers,  │  │ (naviguer, agir,     │  │
│  │               │  │  historique) │  │  lire, répondre)     │  │
│  └──────────────┘  └──────────────┘  └──────────────────────┘  │
│                                                                  │
│  Option tactique : docker-android headless (à la demande)       │
└───────────────────────────┬─────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────────┐
│  WEB CLIENT (gafam.cloud)                                        │
│  Voit et utilise le clone — pas la DB brute, pas le flux H.264   │
└─────────────────────────────────────────────────────────────────┘
```

**Le téléphone exécute. Le VPC reflète. L'interface habite le fantôme.**

---

## Ce qu'est le Ghost Clone (et ce qu'il n'est pas)

### C'est

- Un **daemon permanent** sur le VPC (~50 Mo RAM) qui maintient l'état du téléphone en temps quasi-réel
- Une **représentation navigable** : l'utilisateur ouvre une vue « Mon téléphone » sur le web et **agit** (lire SMS, ouvrir une app, répondre à une notif)
- Un **point d'accès** pour l'humain et, plus tard, pour un agent automatisé
- Un **miroir sémantique** : structure, pas pixels

### Ce n'est pas

- Un remplacement de l'interface messagerie actuelle — **elle reste**, elle est déjà bonne
- Une table SQLite exposée brute au navigateur
- Un émulateur Android 24/7 dans Docker (trop lourd pour 1 Go RAM)
- Un flux scrcpy permanent (trop gourmand, Manager local requis)
- Une copie complète d'AOSP réécrite from scratch

---

## Implémentation : Go d'abord, Docker si ça suffit

**Intention principale :** un **daemon Go complet** (`ghost-clone` ou module de `vpc-relay`) — même langage que le relay, même déploiement Docker, même souveraineté.

**Alternative acceptée :** si un conteneur **headless** (ex. `docker-android` / budtmo) peut faire le travail d'exécution APK à la demande, on l'utilise — **sans** le laisser tourner en permanence. Le daemon Go reste le cerveau ; le Docker headless est un **outil tactique** qu'on démarre, pas un organe vital.

| Composant | Rôle | Permanent ? |
| :--- | :--- | :--- |
| **Daemon Go Ghost Clone** | Sync sémantique, état, API, interface | ✅ Oui — 24/7 |
| **APK compagnon** (évolution du relay) | Pont ADB côté téléphone, UI tree, capteurs | ✅ Oui — sur le tel |
| **docker-android headless** | Exécuter un APK isolé pour test/debug | ❌ À la demande |
| **Scrcpy** (manifest 14) | Contrôle pixel par pixel d'urgence | ❌ Session ponctuelle |

On ne préjuge pas du gagnant technique pour chaque brique. On préjuge du **résultat** : un clone utilisable sur l'interface.

---

## L'Ouroboros distribué (vision)

Le téléphone Android possède ce que le VPC ne peut pas avoir : Play Services, carte SIM, capteurs, apps signées, runtime Google.

Le VPC possède ce que le téléphone ne peut pas avoir : disponibilité 24/7, IP fixe, interface web, stockage illimité, intelligence centralisée.

**L'Ouroboros** : au lieu de dupliquer Android sur le serveur, on **forward** les services du vrai téléphone vers le VPC. Le tel fournit le runtime. Le VPC fournit la présence. L'APK compagnon comble les trous (données privées, accessibility, capteurs déportés).

```
Téléphone                          VPC
──────────                         ───
ActivityManager  ──forward──►  État des apps dans le clone
Notifications    ──forward──►  Centre de notifs web
SMS / Contacts   ──forward──►  Boîte de réception (déjà partiel)
UI Hierarchy     ──forward──►  Arbre navigable dans le Web Client
Play Store       ──forward──►  (Phase ultérieure — install à distance)
```

Pas besoin de réécrire AOSP. Le serpent se mord la queue : le corps reste sur la table de nuit, le fantôme vit dans le cloud.

---

## Ce que l'utilisateur voit (la différence cruciale)

### Aujourd'hui (messagerie SMS — déjà là)

```
gafam.cloud → Conversations SMS
              Contacts synchronisés
              Envoi / outbox
              Paramètres, gardiens
              (+ Remote Control scrcpy en session)
```

C'est une **vraie interface**. Pas une DB. Le Ghost Clone ne vient pas remplacer ça.

### Avec Ghost Clone (extension)

```
gafam.cloud → [Tout ce qui existe déjà]
            + [Mon Téléphone — le reste du device]
                ├─ Écran courant (reconstruit depuis l'arbre UI)
                ├─ Notifications apps tierces
                ├─ État des apps installées
                ├─ Actions : ouvrir Signal, valider 2FA, répondre hors SMS
                └─ Présence permanente sans session scrcpy
```

On **étend** l'interface web au-delà du canal SMS — pas depuis zéro.

---

## Phases

### Phase 1 — Ghost Clone Go + APK compagnon *(maintenant)*

- Daemon Go sur le VPC : sync arbre UI, notifs, SMS, fichiers clés
- APK compagnon (évolution du relay actuel) : expose l'arbre UI via ADB/accessibility
- Web Client : vue « Mon Téléphone » reconstruite depuis l'état sémantique
- Remplace progressivement le besoin de scrcpy pour l'usage quotidien
- **Hors scope Phase 1** : RAG, agent IA, forward Play Store

### Phase 2 — Firmware sur le Hardware Relay *(device/concept.md)*

- Le daemon Ghost Clone devient le **firmware logiciel** du Galet ESP32
- Plus de dépendance à l'APK Android — le hardware parle directement au modem
- Le VPC reçoit le même flux sémantique, depuis une puce au lieu d'un smartphone

### Phase 3 — Le hardware tue le smartphone *(manifest 1 abouti)*

- Le Galet remplace totalement l'Android
- Le Ghost Clone sur le VPC devient la seule interface du relay
- Vision manifest 1 : boîtier minimaliste, SIM, bouton, oubli

---

## Liens avec les autres manifestes

| Manifeste | Relation |
| :--- | :--- |
| **1** (Philosophie) | Le clone réalise le pilier B : le VPC comme **cerveau**, pas simple routeur |
| **5** (Recovery cas 2) | L'arbre UI remplace les `input tap` fragiles pour automatiser Signal/WhatsApp |
| **14** (Scrcpy) | Reste l'outil **tactique** (pixels d'urgence). Le Ghost Clone est le **stratégique** |
| **17** (Messagerie) | Orthogonal — publier/lire chez les autres ≠ cloner son propre tel |
| **device/concept.md** | Phase 2–3 du Ghost Clone = firmware du Galet |

---

## Contraintes VPS (rappel)

Droplet type : **1 vCPU · 1 Go RAM · 25 Go disque**

| Charge | RAM estimée | Verdict |
| :--- | :--- | :--- |
| vpc-relay actuel (Go + SQLite) | ~30 Mo | ✅ |
| Ghost Clone daemon Go | ~50 Mo | ✅ |
| Les deux ensemble | ~80 Mo | ✅ |
| docker-android 24/7 | ~2 Go+ | ❌ |
| Scrcpy permanent | Bande passante | ❌ |

Le clone sémantique est **faisable sur l'infrastructure actuelle**. L'émulateur complet ne l'est pas — et ce n'est pas le but.

---

## Synthèse

> **On n'efface pas la messagerie web. On élève un fantôme autour d'elle.**

Le téléphone dort sur la table de nuit. La messagerie SMS vit déjà sur `gafam.cloud`. Le fantôme ajoute **le reste du device** — léger, permanent, navigable — sans remplacer ce qui marche.

Daemon Go en priorité. Docker headless en renfort. Scrcpy en dernier recours. Le hardware relay en horizon.

Le Ghost Clone, c'est le moment où GAFAM cesse d'être un **relais SMS** et devient vraiment un **double numérique souverain**.

---

## Annexe — Voie pragmatique : Logcat permanent + LLM frontière

> *Et si, avant l'arbre UI complet, on se contentait de **forcer les logs Android** vers le VPC et de laisser un petit modèle les lire à notre place ?*

### L'idée

Au lieu de reconstruire tout le téléphone en sémantique dès le jour 1 :

1. **L'APK compagnon** (ou le relay actuel enrichi) + **ADB si nécessaire** maintient un flux **logcat** permanent vers le daemon Go sur le VPC.
2. Dès que le téléphone **reçoit quelque chose** (SMS, notif, broadcast système, app au premier plan, erreur réseau…), les lignes pertinentes sont **poussées de force** au VPC — pas en polling lent, en stream continu ou par rafales événementielles.
3. Sur le VPC, un **modèle LLM frontière ultra-léger**, spécialisé pour lire les logs Android (format `tag/pid`, `ActivityManager`, `NotificationService`, `SmsReceiver`, etc.), produit une **synthèse** compréhensible par un humain.
4. Le **Web Client** affiche cette synthèse en **temps quasi-réel** : texte narratif (« Signal : message de Alice ») + **carte visuelle** générée ou capture ponctuelle déclenchée par l'événement log.

```
Téléphone                         VPC (daemon Go)
──────────                        ───────────────
SMS reçu        ──► logcat ──►    buffer logs
Notif Signal    ──► logcat ──►    filtre événements
App ouverte     ──► logcat ──►         │
                                      ▼
                               LLM frontière (~1–3B)
                               « Alice t'a écrit sur Signal »
                                      │
                                      ▼
                               Web Client
                               synthèse texte + carte image
```

### Pourquoi ça a du sens

| Avantage | Détail |
| :--- | :--- |
| **Léger** | Logcat = texte. Pas d'arbre UI complet, pas de H.264. Quelques Ko par événement. |
| **Déjà presque là** | APK + ADB + daemon Go = stack GAFAM existante. Pas de nouveau runtime. |
| **Permanent** | Le flux peut tourner 24/7 sans Manager local (ADB WiFi ou tunnel APK). |
| **Intelligence sans clone** | Le LLM **interprète** au lieu de **reproduire** — le VPC devient un traducteur du téléphone. |
| **MVP rapide** | Phase 0 du Ghost Clone avant l'arbre UI complet. |

Les logs Android contiennent souvent plus qu'on ne croit : nom d'app, intent, parfois extrait de notif, changements d'activité, erreurs réseau, tags opérateur. Un modèle entraîné ou fine-tuné sur des dumps logcat + ground truth peut en tirer une **synthèse fidèle** sans parser chaque OEM.

### Ce que ça règle (et ce que ça ne règle pas)

**Oui, ça adresse une partie réelle du problème :**

- **Savoir ce qui se passe** sur le téléphone sans le tenir en main — notifs, apps, SMS hors canal relay, anomalies.
- **Vue unifiée** sur le web : messagerie SMS (déjà là) + **fil d'événements interprété** pour le reste.
- **Recovery / veille** (manifest 5) : détecter qu'un mot-clé est arrivé sur Signal via les logs, sans `input tap` aveugle.
- **Coût infra** : texte + petit LLM quantifié (GGUF, ONNX) peut tenir sur un VPS 1 Go **si le modèle reste vraiment petit** (SmolLM, Phi-3-mini, Qwen2.5-0.5B…) et tourne à la demande par batch de logs, pas en inférence permanente à 60 fps.

**Non, ça ne remplace pas le Ghost Clone complet :**

| Besoin | Logcat + LLM | Arbre UI / Ghost Clone |
| :--- | :--- | :--- |
| Lire qu'une notif Signal est arrivée | ✅ | ✅ |
| Savoir qui a écrit (si log suffisant) | ⚠️ Souvent | ✅ |
| **Répondre** dans Signal depuis le web | ❌ | ✅ (actions) |
| Naviguer dans une app | ❌ | ✅ |
| Fiabilité 100 % (OEM, ROM custom) | ⚠️ Logs variables | ✅ Structure UI |
| Image fidèle de l'écran | ⚠️ Synthèse / screenshot déclenché | ✅ Reconstruction |

En résumé : **ça règle le problème de la conscience à distance** (« qu'est-ce que mon téléphone fait ? »), pas celui du **contrôle total** (« je pilote Signal depuis le navigateur »). Pour beaucoup d'usages GAFAM — veille, recovery, synthèse — c'est déjà énorme.

### Pipeline technique (esquisse)

```
1. APK : ForegroundService « Log Stream »
   - logcat --format=threadtime (filtré tags GAFAM + notif + sms)
   - batch toutes les 2s ou sur événement (BroadcastReceiver notif)

2. Daemon Go (ghost-log ou module vpc-relay)
   - POST /api/ghost/logs (auth JWT_SECRET)
   - Ring buffer SQLite ou fichier rotatif (7 jours)
   - Déclencheur inférence si N nouvelles lignes ou pattern critique

3. LLM frontière (sur VPC ou worker edge)
   - Entrée : fenêtre de logs (dernières 50–200 lignes)
   - Sortie JSON : { summary, app, actor, urgency, suggested_action? }
   - Option image : screenshot ADB one-shot sur événement notif (pas stream)

4. Web Client
   - Panneau « Fil du téléphone » à côté de la messagerie SMS
   - Cartes : texte LLM + miniature (screenshot ou icône app)
   - WebSocket ou poll léger depuis le VPC
```

### Entraînement du modèle spécialisé

Le modèle n'a pas besoin d'être géant. L'approche réaliste :

- **Corpus** : dumps logcat annotés (événement → résumé humain). Générables en dev en simulant SMS, notifs, ouverture d'apps sur un tel de test.
- **Fine-tune** d'un modèle 0.5B–3B sur ce corpus uniquement — pas un GPT généraliste.
- **Tâche unique** : logcat window → synthèse structurée. Pas de chat libre.
- **Hébergement** : `llama.cpp` / `ollama` sur le VPS si RAM suffisante, sinon inférence batch toutes les 30s sur rafales de logs (pas du temps réel strict — acceptable pour la veille).

### Place dans la roadmap Ghost Clone

| Phase | Quoi |
| :--- | :--- |
| **0 — Logcat + LLM** | Flux permanent, synthèse web, screenshot ponctuel. **Essai prioritaire.** |
| **1 — Arbre UI** | Extension si les logs ne suffisent pas pour agir |
| **2–3** | Hardware relay (inchangé) |

La voie logcat n'est pas un repli — c'est peut-être **le bon premier pas** : peu de bande passante, réutilise l'APK, donne une expérience « mon téléphone me parle » sur le web avant de construire le clone sémantique complet.

### Verdict

**Oui, ça a du sens.** Ça ne remplace pas la vision Ghost Clone, mais ça **résout une bonne partie du problème** avec un ordre de grandeur moins de complexité : conscience distante, synthèse lisible, intégration naturelle à côté de la messagerie web existante. Le contrôle fin (taper dans Signal, valider un écran) restera scrcpy ou arbre UI — mais beaucoup d'utilisateurs n'ont besoin que de **savoir** et parfois de **réagir via SMS/outbox**, pas de piloter chaque pixel.

> **Le fantôme commence peut-être par une voix : les logs bruts du corps, traduits par un petit esprit sur le VPC.**
