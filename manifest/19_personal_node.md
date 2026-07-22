# 19. Le Nœud Personnel — La présence numérique permanente

> **⚠️ BROUILLON — Carnet de vision produit. Ne pas prendre au sérieux pour l'implémentation actuelle.**
> Idées fortes (nœud, surfaces, peripherals, peering, bourgeon) à affiner ou jeter. Même statut qu'un brouillon de brouillon — utile pour réfléchir, pas pour coder demain.

---

## L'état abouti en une scène

Le téléphone est dans un tiroir, branché, connecté en permanence au VPC. Le VPC ne dort jamais. L'utilisateur n'a plus son téléphone en poche — il ouvre un client web depuis n'importe où : chez un ami, en voyage, sur une montre, demain sur une interface Neuralink ou des lunettes connectées.

Il ne se dit pas *« j'accède à mon clone »* ou *« je consulte mon VPS »*.

Il se dit :

> **« J'accède à mon GAFAM (nœud). »**

Son numéro de téléphone n'est plus lié à un objet en poche. C'est l'adresse d'une **présence persistante** — la sienne — toujours éveillée, souveraine, joignable par les humains et les agents qu'il autorise.

---

## Ce qu'est le Nœud GAFAM

Un **Nœud Personnel** est la matérialisation cloud de l'identité mobile d'un humain :

| Propriété | Définition |
| :--- | :--- |
| **Adresse** | Son numéro de téléphone → `+33606.gafam.cloud` |
| **Hébergement** | Son propre VPC (auto-hébergé, souverain) |
| **État** | Toujours éveillé — mémoire, messagerie, présence, file d'intents |
| **Périmètre** | Tout ce qui relevait du « téléphone » : SMS, apps, notifs, recovery, ghost |
| **Accès** | Surfaces autorisées (web, montre, agent IA, client tiers) |

Le Nœud n'est pas un serveur technique. Ce n'est pas un clone Android. Ce n'est pas une app de messagerie.

**C'est toi, version persistante.**

---

## Architecture : trois couches, un centre

```
┌─────────────────────────────────────────────────────────────────┐
│  SURFACES (minces — partout dans le monde)                       │
│                                                                  │
│  Web Client · montre · lunettes · agent IA · client chez un ami  │
│  → lire l'état · déposer des intents · s'authentifier (rituel)   │
└───────────────────────────────┬─────────────────────────────────┘
                                │
                    ┌───────────▼───────────┐
                    │      TON NŒUD         │
                    │  +33606.gafam.cloud     │
                    │                         │
                    │  /state    — ce que je sais
                    │  /intents  — ce que je veux faire
                    │  /feed     — ce que je publie (manifest 17)
                    │  /auth     — qui peut entrer (manifest 12)
                    │  /links    — qui je connais (manifest 17)
                    │                         │
                    │  Toujours éveillé.      │
                    │  Souverain.             │
                    │  Un seul endroit.       │
                    └───────────┬─────────────┘
                                │
┌───────────────────────────────▼─────────────────────────────────┐
│  PERIPHERALS (gros — chez toi, branchés)                         │
│                                                                  │
│  Téléphone au tiroir (APK relay) · futur Galet ESP32             │
│  → exécute les intents · SIM · apps · capteurs · réseau cellulaire│
└─────────────────────────────────────────────────────────────────┘
```

**Surfaces** = yeux et mains à distance.  
**Nœud** = cerveau et mémoire permanents.  
**Peripherals** = bras sur le réseau physique (GSM, apps Android).

L'utilisateur ne porte plus le peripheral. Il habite ses surfaces. Le nœud fait le lien.

---

## Les trois questions du produit

Tout GAFAM se réduit à trois questions. Chaque manifeste existant répond à l'une d'elles :

| Question | Module | Manifestes |
| :--- | :--- | :--- |
| **Qu'est-ce que je sais ?** | État unifié (`/state`) | 18 Ghost · SMS · contacts · notifs |
| **Qu'est-ce que je veux faire ?** | File d'intents (`/intents`) | SMS outbox · actions apps · recovery |
| **Qui peut toucher mon nœud ?** | Auth & Liens | 12 Rituel · 17 Liens · 5 Gardiens |

Plus de features empilées. Un **contrat** : lire, agir, autoriser.

---

## État unifié (`/state`)

Un seul endpoint. Un seul JSON. Ce que le nœud sait **maintenant** :

```json
{
  "node": "+33606",
  "presence": "awake",
  "peripheral": { "connected": true, "last_seen": "...", "battery": 87 },
  "messaging": {
    "unread_sms": 3,
    "pending_outbox": 1
  },
  "ghost": {
    "last_events": [
      { "app": "Signal", "summary": "Message de Alice", "at": "..." }
    ]
  },
  "intents_pending": 2,
  "next_ritual_window": "friday 09:00 UTC"
}
```

Humain ou agent : un GET, une photographie de ta vie mobile.

---

## File d'intents (`/intents`)

Tu n'envoies pas un SMS. Tu **déposes une intention** :

```json
{
  "id": "intent_a7f3",
  "action": "send_sms",
  "to": "+33607",
  "body": "J'arrive dans 10 minutes",
  "created_at": "...",
  "status": "pending"
}
```

Le nœud stocke. Le peripheral exécute quand il peut (téléphone au tiroir, réseau dispo). Le statut passe à `done` ou `failed`.

**Depuis n'importe quelle surface** — web chez un ami, montre, agent IA :

```json
{ "action": "reply_signal", "to": "Alice", "body": "OK" }
```

Le tiroir exécute plus tard. Tu n'as pas besoin du téléphone en poche. Tu n'as pas besoin d'être synchrone.

| Action | Exécuté par |
| :--- | :--- |
| `send_sms` | APK / Galet (SIM) |
| `reply_signal` | APK (accessibility) ou scrcpy session |
| `publish_envelope` | Nœud directement (manifest 17) |
| `wake_peripheral` | Signal au tel au tiroir |

La file d'intents est le **muscle différé** du nœud.

---

## Surfaces : tout client est une face du nœud

| Surface | Capacités | Exemple |
| :--- | :--- | :--- |
| **Web complet** | État + intents + messagerie + ghost + settings | `06.gafam.cloud` |
| **Montre / lunettes** | État résumé + intents courts | « Répondre OK » d'un tap |
| **Agent IA autorisé** | `/state` + `/intents` (scope limité) | « Résume mes notifs » |
| **Client tiers** | Auth rituel + lecture feed (manifest 17) | Ami qui scanne ton feed |
| **Neuralink / futur** | Intents vocaux + synthèse état | À anticiper, pas à coder |

Chaque surface s'enregistre auprès du nœud avec un **scope** :

```json
{
  "surface_id": "watch_01",
  "type": "wearable",
  "scopes": ["state.read", "intents.create.sms"],
  "authorized_at": "..."
}
```

Le nœud ne demande pas *« quel appareil es-tu ? »* Il demande *« quelle face de moi es-tu ? »*

---

## Contrat Agent (humains et machines)

Un agent IA — le tien, ou un service tiers autorisé — interagit avec ton nœud comme une surface :

```
GET  /state              → photographie actuelle
GET  /feed               → ce que tu as publié (manifest 17)
POST /intents            → ce que tu veux faire
GET  /intents?status=pending → ce qui attend le peripheral
```

**Un endpoint. Une identité. Des permissions.**

Pas de scraping Gmail + WhatsApp + SMS. Pas de compte Meta. Ton nœud est **l'API de ta vie mobile** — souveraine, auto-hébergée, adressable par ton numéro.

C'est ce qu'Apple et Google ne donneront jamais : un agent qui travaille **pour toi sur ton infra**, pas sur la leur.

---

## Comment les manifestes existants deviennent des modules

| Manifeste | Rôle dans le Nœud |
| :--- | :--- |
| **1** Philosophie | Le Nœud **est** la souveraineté incarnée |
| **5** Recovery | Réentrée dans **ton nœud** sans peripheral |
| **12** Rendez-vous mécanique | Auth des **surfaces** |
| **14** Scrcpy | Surface tactique pixels (session) |
| **17** Publier / Liens / Cercles | Communication **entre nœuds** |
| **18** Ghost Clone | Enrichissement de **`/state`** |
| **device/concept** | Peripheral **Galet** — remplace le tel au tiroir |

Le Nœud n'ajoute pas une feature. Il **nomme le centre** autour duquel tout gravite.

---

## L'évolution du produit (4 ans de vision)

### Aujourd'hui — Nœud naissant

- Peripheral : APK relay (SMS)
- Nœud : vpc-relay Go + SQLite
- Surface : web messagerie
- **Gap** : pas encore `/state` unifié ni file d'intents

### Phase 2 — Nœud éveillé

- Peripheral : APK sensoriel (notifs, événements) + tel au tiroir
- Nœud : `/state` + `/intents` + ghost (manifest 18)
- Surfaces : web complet + recovery (manifest 5, 12)
- Communication inter-nœuds (manifest 17)

### Phase 3 — Nœud autonome

- Peripheral : Galet hardware (device/concept)
- Nœud : agent-ready, contrats IA
- Surfaces : montre, lunettes, APIs tierces
- Plus de smartphone au tiroir — le Galet suffit

### Horizon — Nœud comme norme

- Le numéro de téléphone = adresse d'un nœud persistant
- Les humains portent des **surfaces minces**
- Les agents parlent aux **nœuds**, pas aux apps
- GAFAM = protocole d'infrastructure personnelle souveraine

---

## Ce que ça change pour le développement

**Arrêter** d'ajouter des endpoints SMS/contacts/scrcpy en silos.

**Commencer** à tout faire passer par :

```
/state   — lire
/intents — agir
/auth    — entrer
/links   — connaître
/feed    — publier (manifest 17)
```

Le relay SMS actuel devient `intents.execute(send_sms)` côté peripheral.  
Le ghost devient `state.ghost.last_events`.  
Le scrcpy devient une surface `type: tactical_pixels`.  
La recovery devient `auth.surface_temporary`.

**Un nœud. Trois verbes. Des surfaces.**

---

## Synthèse

> **GAFAM abouti, ce n'est pas un relay SMS, ni un clone téléphone, ni un VPC Go.**
>
> **C'est ton numéro devenu présence permanente — un nœud souverain toujours éveillé.**
>
> **Le téléphone au tiroir n'est qu'un bras. Le web n'est qu'une face.**
>
> **Toi, tu n'as plus besoin de porter le corps.**

Le Nœud Personnel est le nom du produit fini. Tout le reste n'en est que l'anatomie.

---

## Suite — Peering : comment deux nœuds se trouvent *(brouillon de brouillon)*

> **⚠️ Section la plus spéculative du manifeste 19.** Bourgeon, guichet de seeks, recherche inversée — pistes non validées. À ne pas confondre avec les Liens du manifest 17 tant que rien n'est décidé.

> *Le Bourgeon de contact est un joli nom — pas encore la solution. Ce qui suit pose le principe et la direction, à affiner.*

### Postulat : tout le monde a déjà un nœud

On ne vend pas « une app pour contacter Gary ». Chaque personne sur GAFAM **a déjà** un nœud — `+33XX.gafam.cloud`, toujours là. Le problème n'est pas *« comment créer un compte »*. C'est *« comment deux nœuds se reconnaissent sans s'échanger un numéro de téléphone »*.

Comme pour la messagerie (manifest 17) : **pas de point de contact classique**. Pas de « envoyer une demande à l'autre ». Pas de push. Pas d'adresse email forgeable.

### L'inversion : publier chez soi, l'autre vient voir

| Modèle classique | Modèle nœud GAFAM |
| :--- | :--- |
| J'envoie une demande d'ami **vers** Bob | Je **publie chez moi** une intention de lien |
| Bob reçoit une notif | Bob **va regarder** qui cherche à le joindre |
| Numéro / email exposé | Identité stable privée, surface rotative |
| Spam permanent | IP qui tourne → anciennes adresses mortes |

**Si je veux contacter quelqu'un**, je ne le ping pas. Je publie sur **mon** feed une **demande de peering** :

```json
{
  "type": "link_seek",
  "published_at": "2026-07-10T19:00:00Z",
  "target_hint": "hash(pubkey_bob)",
  "context": "Rencontré à la conférence OpenDataHive — Gary",
  "expires_at": "2026-07-17T19:00:00Z"
}
```

Ou, si je ne connais pas encore son nœud mais j'ai assez d'infos :

```json
{
  "type": "link_seek_vague",
  "context": "Bob, bar Le Marais, 9 juillet",
  "challenge": "mot de passe convenu : forge-3"
}
```

Je publie **chez moi**. C'est mon journal d'intentions. Pas un message envoyé.

### La réception inversée : qui veut me joindre ?

Bob ne reçoit rien. À **sa** fenêtre de communication (rituel manifest 12, manifest 17), il **scanne** :

1. **Ses Liens** — enveloppes `for: +33607` (déjà manifest 17)
2. **Le guichet de seeks inverses** — tous les nœuds qui ont publié une `link_seek` **adressée à lui**

```
Bob ouvre son client → [Qui me cherche ?]
  │
  ├─ Scan annuaire / guichet : seeks où target_hint = hash(ma_pubkey)
  ├─ Liste : « Gary (+33606) — conférence OpenDataHive — il y a 2 jours »
  ├─ Liste : « Inconnu — bar Le Marais — challenge forge-3 »
  │
  └─ Bob accepte ou refuse chaque seek → Lien créé ou ignoré
```

C'est une **recherche inversée** — pas « j'ajoute Gary », mais *« qui a voulu entrer en contact avec moi, quand, comment, avec quel contexte »*. Comme des invitations, mais **l'autre est venu déposer son intention**, pas te tirer la manche.

### Pas de point de contact — un guichet aveugle

Pour que Gary puisse publier une seek **pour** Bob sans connaître l'URL du feed de Bob :

```
Gary dépose sur le guichet (D1 / annuaire) :
  clé publique = hash(pubkey_bob)     ← Bob seul sait que c'est pour lui
  blob = seek chiffré pour Bob
  TTL = 7 jours

Bob, à son rituel :
  GET /seeks/for/me
  → déchiffre, lit, accepte ou non
  → seek supprimée après lecture (comme coffre manifest 12)
```

Le guichet ne sait pas qui est Gary ni ce que dit la seek. Il sait seulement : *« quelqu'un cherche l'identité X »*. Bob est le seul à pouvoir lire.

**Quand l'IP de Gary tourne** : le seek sur le guichet pointe vers `node_id` Gary, pas son IPv4. Le spam sur l'ancienne IP meurt. Les Liens acceptés suivent l'annuaire.

### Identité en trois couches (rappel)

| Couche | Exemple | Partagé ? | Survit au changement d'IP ? |
| :--- | :--- | :--- | :--- |
| **SIM privée** | `+33606` | ❌ Jamais en peering | — |
| **Nœud stable** | `pubkey` / `06.gafam.cloud` | ⚠️ Aux Liens acceptés | ✅ |
| **Reachability** | IPv4, FreeDNS | Rotation = anti-spam | ❌ (volontairement) |

On ne « partage pas son IPv4 » en soirée. On partage — si besoin — un **bourgeon** éphémère, un **contexte** (*« conférence, forge-3 »*), ou on publie une seek et on attend que l'autre vienne regarder.

### Modes de contactabilité

| Mode | Comportement |
| :--- | :--- |
| **Fermé** | Aucune seek acceptée. Liens existants seulement. |
| **Rituel seulement** | Je regarde le guichet à ma fenêtre — pas avant. |
| **Bourgeon** (éphémère) | Surface IRL 5–15 min — QR, NFC, code verbal — pour échanger assez d'info pour une seek ciblée |
| **Ouvert aux seeks contextuelles** | J'accepte les `link_seek_vague` si le challenge colle |

Tu **claudes** ou **déclaudes** — le nœud n'est pas une boîte aux lettres ouverte à tous.

### Scénario IRL complet

```
1. Soirée. Gary rencontre Bob. Pas de numéro échangé.
2. Gary : « T'as un nœud ? On fait forge-3 ? »
3. Gary publie chez lui + dépose seek sur guichet (target = Bob si bourgeon échangé)
4. Bob, vendredi 9h (son rituel), ouvre [Qui me cherche ?]
5. Bob voit Gary, contexte soirée, accepte → Lien
6. Désormais : Gary publie sur son feed, Bob scanne ses Liens (manifest 17)
```

Si Bob ne veut pas : il ignore. Gary n'a jamais **envoyé** quoi que ce soit **vers** Bob. Pas de rejet humiliant visible — juste absence d'acceptation.

### Ce qui reste à inventer (honnêtement)

| Question ouverte | Piste |
| :--- | :--- |
| Guichet central vs scan fédéré ? | D1 aveugle par `hash(pubkey)` — comme coffres manifest 12 |
| Seek vague sans bourgeon ? | Brute-force de contexte impossible si challenge fort |
| Échelle mondiale ? | Pas de scan global — **rituel + guichet par identité** |
| Assez d'info pour contacter ? | `link_seek_vague` + contexte humain — l'autre doit reconnaître |
| Bourgeon | Utile IRL pour échanger `target_hint` — pas le mécanisme, l'amorce |

Le Bourgeon reste un **geste poétique** pour la rencontre physique. Le mécanisme, c'est **publish seek + inverse scan + accept** — la même inversion que toute la philosophie GAFAM.

### Lien avec le reste

| Manifeste | Rôle dans le peering |
| :--- | :--- |
| **17** | Publier chez soi, scanner les Liens — **même geste** pour seeks et messages |
| **12** | Fenêtre rituelle = moment où je regarde « qui me cherche » |
| **19** | `/links` = seeks acceptées ; `/feed` = seeks publiées |

> **On ne demande pas le numéro. On publie qu'on cherche. L'autre vient voir s'il veut.**
