# 17. Le Canal Poneglyph — Authentification par Conjugaison Temporelle

## Le Besoin : Un 5ᵉ Plan de Communication

Les manifestes précédents couvrent quatre plans réseau distincts :

| Plan | Canal | Limite principale |
| :--- | :--- | :--- |
| **GSM** | SMS/MMS via relais Android | Coupure opérateur, spoofing SS7 |
| **Internet IP** | Android↔VPC (5151), Web↔VPC (5150) | Connexion synchrone, IP exposée |
| **Portail** | Cloudflare D1 (coffres éphémères) | Boîte morte à usage personnel (manifest 12) |
| **Fédération** | VPC↔VPC mTLS (port 5152) | Évident mais lourd : certificats, IP, simultanéité |

Le manifeste 5 (Social Recovery) décrit un scénario critique : l'utilisateur est chez un ami, sans son téléphone relais, et le réseau GSM est coupé. Le cas N°1 (VPC-to-VPC direct) et le cas N°2 (ADB + Signal) restent des solutions **synchrones** ou **dépendantes d'applications tierces**.

Ce manifeste introduit un **5ᵉ plan** : un canal de communication **asynchrone**, **symétrique**, où l'authentification ne repose ni sur un certificat, ni sur un header forgeable, ni sur une connexion directe entre machines — mais sur un **rituel physique partagé** qui conjure un espace privé de communication.

---

## Le Concept Fondamental : Authentification par Conjugaison

### Ce que ce n'est PAS

- Ce n'est **pas** du VPC↔VPC classique (mTLS, SSH, port 5152).
- Ce n'est **pas** un serveur SMTP ou un relay qui « valide » l'identité de l'expéditeur.
- Ce n'est **pas** de l'envoi du « sens » d'un message via reconstruction sémantique (HDC) — bien que HDC puisse servir de couche de furtivité optionnelle.
- Ce n'est **pas** un échange de clés ou de dictionnaire sur le réseau.

### Ce que c'est

> **Deux personnes physiquement présentes exécutent un geste synchronisé. Ce geste fait naître un espace cryptographique privé. Tout message lisible dans cet espace est authentique par définition.**

On appelle cet espace un **Poneglyph** (référence au projet [HDC Markdown Encoder](https://github.com/Garletz/HDC-Markdown-Encoder-Reconstructor) d'OpenDataHive — le principe d'un dictionnaire partagé permettant de reconstruire un message à partir de vecteurs, sans jamais transmettre le dictionnaire lui-même).

La validation n'est pas : *« Est-ce le VPC de mon ami ? »*
La validation est : **« Est-ce que je peux décoder ça avec notre Poneglyph ? »**

Un message illégitime n'est pas rejeté — il est **indéchiffrable**. Il n'existe pas de champ `From:` à falsifier.

---

## Architecture : Les Trois Couches du Canal

```
┌─────────────────────────────────────────────────────────────────┐
│  COUCHE 1 — LE RITUEL (Forge)                                   │
│  Preuve de co-présence physique → seed partagé (jamais transmis) │
├─────────────────────────────────────────────────────────────────┤
│  COUCHE 2 — LE PONEGLYPH (Espace privé)                         │
│  Clé symétrique dérivée du seed → chiffrement AES-GCM            │
│  Option : encodage HDC pour furtivité du transport              │
├─────────────────────────────────────────────────────────────────┤
│  COUCHE 3 — LA BOÎTE MORTE (Transport)                          │
│  Cloudflare D1 / sous-domaine éphémère / tout pegboard public   │
│  Zéro confiance. Stocke des blobs. Ne valide rien.              │
└─────────────────────────────────────────────────────────────────┘
```

**Le relay sur un domaine n'a aucun rôle de sécurité.** Il est interchangeable (D1, DNS TXT, fichier partagé, clé USB). Il sert uniquement à l'**asynchronicité** : les deux VPC n'ont pas besoin d'être en ligne simultanément.

---

## Le Rituel de Forge (Création du Poneglyph)

Le forge réutilise et étend le **Rendez-vous Synchrone Mécanique** (manifest 12), mais avec **deux participants actifs** au lieu d'un seul.

### Prérequis

- Alice et Bob possèdent chacun un VPC GAFAM Relay.
- Ils sont **physiquement au même endroit** (co-présence = preuve biologique anti-hacker).
- Les deux ont accès à leur Web Client ou APK.

### Déroulement

#### Étape 1 : Initiation du Forge

1. Alice ouvre son Web Client et clique sur **« Forger un Canal Poneglyph »**.
2. Elle saisit le numéro de téléphone de Bob (`+336...`).
3. Son VPC génère un challenge temporel :
   - **Un délai relatif** (ex: *« Dans 90 secondes »*) — voir manifest 15 pour éviter les problèmes de fuseaux horaires.
   - **Un nombre d'impulsions** entre 1 et 8 (ex: `6 clics`).
4. Le challenge est affiché sur l'écran d'Alice **et** poussé vers le VPC de Bob via la boîte morte d'appairage (même mécanisme que le coffre manifest 12, mais avec un `type: "forge_invite"`).

#### Étape 2 : Acceptation et Synchronisation

5. Bob voit l'invitation sur son Web Client : *« Alice (+336...) souhaite forger un Canal Poneglyph. Rendez-vous dans 90 secondes — 6 impulsions. »*
6. Bob accepte. Les deux VPC entrent en mode **forge pending**.
7. Un compte à rebours synchronisé s'affiche sur les deux écrans.

#### Étape 3 : Le Geste Conjugué (T₀)

8. À l'expiration du compte à rebours, un bouton d'action apparaît sur **les deux** interfaces (fenêtre de 30 secondes).
9. Alice et Bob effectuent chacun les **6 impulsions** sur leur propre écran.
10. Chaque VPC envoie localement sa confirmation au challenge (pas d'échange réseau à ce stade).

#### Étape 4 : La Conjuration (T₁)

11. Les deux VPC dérivent **indépendamment** et **identiquement** le seed du Poneglyph :

```
seed = PBKDF2(
  passphrase = sort(phone_A, phone_B) + "|" + delay_seconds + "-" + impulses,
  salt       = forge_id,          // UUID public déposé sur la boîte morte
  iterations = 500_000
)

channel_key = HKDF(seed, info="gafam-poneglyph-v1", len=32)
channel_id  = SHA256(phone_A + phone_B + forge_id)[:16]  // identifiant public du canal
```

12. Le `channel_key` n'est **jamais transmis**. Il existe uniquement en RAM et dans la table SQLite locale `poneglyph_channels` des deux VPC.
13. Chaque VPC enregistre le canal :

```sql
CREATE TABLE poneglyph_channels (
  id           TEXT PRIMARY KEY,   -- channel_id
  peer_phone   TEXT NOT NULL,
  peer_name    TEXT,
  forged_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
  expires_at   DATETIME,           -- TTL configurable (défaut : 30 jours)
  last_seq_in  INTEGER DEFAULT 0,
  last_seq_out INTEGER DEFAULT 0,
  status       TEXT DEFAULT 'active'  -- active | revoked | expired
);
```

14. Le canal est vivant. Le Poneglyph existe. Aucun secret n'a transité sur le réseau.

---

## Communication Asynchrone sur le Canal

### Format d'un Message

Chaque message déposé sur la boîte morte suit cette structure :

```json
{
  "channel_id": "a7f3c2e1b9d84f60",
  "seq": 42,
  "type": "message | wake | urgent | ping | close | recovery_request | recovery_grant",
  "blob": "<AES-256-GCM(channel_key, payload) en base64>",
  "nonce": "<12 bytes aléatoires>",
  "timestamp": "2026-07-07T20:41:00Z"
}
```

Le champ `blob` est tout ce que la boîte morte voit. Sans le `channel_key`, c'est du bruit.

### Payload Interne (après déchiffrement)

```json
{
  "from_phone": "+336Alice",
  "body": "URGENCE_GAFAM — demande de code recovery",
  "reply_to_seq": null
}
```

### Cycle de Vie d'un Message

1. **VPC-A encode** le payload avec `channel_key` → dépose sur D1 (`POST /api/poneglyph/{channel_id}`).
2. **VPC-B poll** périodiquement (`GET /api/poneglyph/{channel_id}?since_seq=N`) ou est réveillé par un trigger `wake`.
3. **VPC-B décode** → si le déchiffrement AES-GCM réussit (tag valide) → message authentique.
4. **VPC-B ACK** → `DELETE` du blob sur D1 (lecture destructive, comme manifest 12).
5. Si le déchiffrement échoue → le blob est ignoré (bruit, leurre, ou attaque).

---

## Les Codex : Vocabulaire de Signalisation

Pour minimiser le trafic et permettre le réveil asynchrone, le canal réserve des **types de messages courts** :

| Type | Rôle | Taille typique |
| :--- | :--- | :--- |
| `ping` | Heartbeat — le canal est vivant | ~100 bytes |
| `wake` | Réveil immédiat — déclenche un poll complet | ~100 bytes |
| `urgent` | Wake + notification push Android du peer | ~150 bytes |
| `recovery_request` | Demande de code d'urgence (manifest 5) | ~300 bytes |
| `recovery_grant` | Réponse avec challenge mécanique (HH:MM-N) | ~300 bytes |
| `message` | Message libre (chat, instructions) | variable |
| `close` | Fermeture propre du canal | ~100 bytes |

Ces types ne sont pas des headers forgeables — ils sont **à l'intérieur** du blob chiffré. L'extérieur ne voit que `channel_id` + `seq` + bruit.

---

## Cas d'Usage Principal : Recovery sans GSM

Scénario : Gary est chez Alice. Son téléphone relais est chez lui. Le GSM est coupé. Ils ont forgé un Canal Poneglyph il y a deux semaines.

```
Gary (chez Alice)              Boîte Morte D1              VPC Gary (chez lui)
       │                              │                            │
       │  Gary demande un code        │                            │
       ├─ VPC Alice encode ─────────►│                            │
       │  type: recovery_request     │                            │
       │                              │◄──── poll (wake reçu) ────┤
       │                              │                            │
       │                              │──── blob recovery_req ────►│
       │                              │                            ├─ decode OK
       │                              │                            ├─ valide channel
       │                              │                            ├─ génère challenge
       │                              │◄─── blob recovery_grant ───┤
       │                              │                            │
       ├─ VPC Alice lit réponse ◄────┤                            │
       │                              │                            │
       │  "Code : 18:36 — 4 clics"   │                            │
       │◄── Alice montre à Gary ──────┤                            │
       │                              │                            │
       ├─ Gary entre le code sur gafam.cloud ─────────────────────►│
       │  (manifest 12 — rendez-vous mécanique)                   │
```

**Aucune connexion directe entre les VPC.** Aucun SMS. Aucun header forgeable. La preuve que la requête vient d'Alice est implicite : seul un participant du forge peut produire un blob déchiffrable.

---

## Couche Optionnelle : Furtivité HDC

Pour les environnements à surveillance réseau avancée (DPI, analyse de trafic), le blob AES peut être **ré-encodé en vecteurs hyperdimensionnels** avant dépôt sur la boîte morte.

### Principe

1. Le VPC dispose d'un `ItemMemory` dérivé du même `seed` (extension HDC du Poneglyph).
2. Le payload chiffré AES est tokenisé et encodé en vecteurs 10 000D (int8).
3. La boîte morte stocke des `.npy` ou des tableaux de floats — **indiscernables du bruit** pour un observateur.
4. Le VPC destinataire décode HDC → récupère le blob AES → déchiffre AES.

### Quand utiliser HDC

| Usage | HDC recommandé ? |
| :--- | :--- |
| Wake / Ping / Close | Non — AES seul suffit (~100 bytes) |
| Recovery request/grant | Optionnel — gain de furtivité marginal |
| Messages longs / chat | Non — limitations HDC (mots répétés, OOV, latence) |
| Environnement hostile (DPI) | Oui — le transport ne ressemble plus à du JSON chiffré |

HDC est une **couche de camouflage**, pas le mécanisme d'authentification. L'authentification reste le forge.

---

## Schéma D1 (Boîte Morte Cloudflare)

```sql
CREATE TABLE poneglyph_mailbox (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  channel_id  TEXT NOT NULL,
  seq         INTEGER NOT NULL,
  blob        TEXT NOT NULL,        -- payload chiffré (ou vecteurs HDC)
  created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
  expires_at  DATETIME,             -- TTL par message (défaut : 24h)
  UNIQUE(channel_id, seq)
);

CREATE INDEX idx_poneglyph_channel ON poneglyph_mailbox(channel_id, seq);
```

### Règles du Guichetier (Worker Cloudflare)

- `GET /api/poneglyph/{channel_id}?since_seq=N` : retourne les messages avec `seq > N`, supprime ceux lus.
- `POST /api/poneglyph/{channel_id}` : accepte un blob, incrémente `seq` atomiquement.
- **Rate limiting** : 10 requêtes/minute par `channel_id` + par IP.
- **Pas d'authentification côté Worker** — le Worker ne connaît ni les participants, ni le contenu.
- **Expiration automatique** : les blobs non lus sont supprimés après 24h (cron Worker).
- **Honeypots** : le Worker peut servir de faux blobs aléatoires sur des `channel_id` inexistants pour empoisonner les attaquants qui scannent.

---

## Analyse de Sécurité

### Forces

| Propriété | Mécanisme |
| :--- | :--- |
| **Anti-forge** | Pas de champ identité externe. Déchiffrer = prouver la participation au forge. |
| **Anti-MITM** | Le `channel_key` n'a jamais transité sur le réseau. |
| **Co-présence physique** | Le forge exige deux humains au même endroit, au même moment. |
| **Asynchronicité** | Store-and-forward natif. Compatible DTN (manifest 7). |
| **Symétrie** | Aucun VPC n'est « serveur » de l'autre. |
| **Éphéméralité** | Messages supprimés après lecture. Canal expirable (TTL). |
| **Boîte morte aveugle** | Cloudflare/D1 ne voit que du bruit. Zéro confiance sur le transport. |

### Risques et Atténuations

| Risque | Atténuation |
| :--- | :--- |
| **Entropie faible du challenge** (même critique que manifest 12) | Timer relatif (manifest 15) + fenêtre de 30s + PBKDF2 500k |
| **Canal compromis** (seed deviné) | Révocation (`close`) + re-forge obligatoire |
| **Boîte morte compromise** | Le contenu est chiffré. Sans `channel_key`, inutile. |
| **Replay attack** | Numérotation `seq` stricte + rejet des `seq` déjà traités |
| **Canal orphelin** (un VPC révoque, l'autre non) | Message `close` signé + expiration TTL automatique |
| **Observation du forge** (caméra, épaule) | Le challenge est visuel mais l'impulsion est locale (pas transmise) |

### Comparaison avec les Autres Canaux

| Canal | Forgeable ? | Async ? | Co-présence requise ? |
| :--- | :--- | :--- | :--- |
| SMS recovery (manifest 5) | Oui (SS7 spoof) | Oui | Non |
| VPC↔VPC mTLS (manifest 11) | Non | Non | Non |
| Coffre mécanique (manifest 12) | Non (entropie faible) | Semi | Non (solo) |
| SMTP | Oui (headers) | Oui | Non |
| **Canal Poneglyph** | **Non** | **Oui** | **Oui (au forge)** |

---

## Lien avec les Autres Manifestes

| Manifeste | Relation |
| :--- | :--- |
| **5** (Social Recovery) | Le Canal Poneglyph remplace le cas N°1 (VPC-to-VPC) par une version asynchrone et sans connexion directe. |
| **7** (DTN) | Le canal est nativement store-and-forward. Compatible avec des transports lents (radio HF, satellite). |
| **11** (Dual-Binding) | Le port 5152 (fédération mTLS) reste pertinent pour la messagerie P2P à haut débit. Le Poneglyph couvre les cas recovery et signaux courts. |
| **12** (Rendez-vous Mécanique) | Le forge Poneglyph **étend** le challenge mécanique à deux participants. Le recovery final utilise toujours le rendez-vous solo. |
| **15** (Fuseaux Horaires) | Le forge utilise un **timer relatif** (« dans 90 secondes ») et non une heure absolue. |

---

## Implémentation (Roadmap vpc-relay)

### Phase 1 — MVP Recovery

- [ ] Table `poneglyph_channels` dans SQLite
- [ ] Endpoint VPC : `POST /api/poneglyph/forge` (initie le rituel)
- [ ] Endpoint VPC : `POST /api/poneglyph/forge/confirm` (valide les impulsions)
- [ ] Worker Cloudflare : table `poneglyph_mailbox` + routes GET/POST
- [ ] Poll loop dans `main.go` : goroutine par canal actif
- [ ] Types `recovery_request` et `recovery_grant`

### Phase 2 — Signaux et Wake

- [ ] Types `wake`, `urgent`, `ping`, `close`
- [ ] Notification Android via outbox existante sur `urgent`
- [ ] Révocation et re-forge

### Phase 3 — Furtivité HDC (optionnel)

- [ ] Binding Go ↔ lib HDC (ou port simplifié du encodeur Python)
- [ ] `ItemMemory` dérivé du `seed` Poneglyph
- [ ] Mode `hdc_transport: true` par canal

---

## Synthèse

Le Canal Poneglyph n'est pas un protocole de messagerie. C'est un **contrat temporel scellé par un rituel physique**, matérialisé par une clé symétrique que deux VPC conjurent ensemble sans jamais l'échanger.

> **Le relay n'authentifie personne. Le temps partagé forge un espace privé. Pouvoir y lire, c'est la preuve d'identité.**

C'est le 5ᵉ plan réseau de GAFAM : un **plan sémantique de confiance** au-dessus d'un transport aveugle, où le message n'existe que pour ceux qui ont partagé le geste du forge.

c'estt nul -_-

---

# Partie 2 — Cartographie : Deux VPC qui veulent communiquer

> *Reprise du but initial. Pas de concept neuf. Juste : qu'est-ce qui existe déjà, du plus ancien au plus récent, et qu'est-ce qu'on réutilise ?*

---

## Le Problème Posé

Deux serveurs VPC GAFAM — chacun chez un utilisateur différent, chacun souverain sur sa machine — doivent pouvoir :

1. **S'échanger des messages** (recovery, signaux, chat court).
2. **Le faire sans GSM** si le réseau cellulaire est coupé.
3. **Le faire sans être online en même temps** (l'un chez lui, l'autre chez un ami).
4. **Prouver que le message vient du bon pair** — sans header forgeable, sans confiance aveugle dans un relay tiers.
5. **Ne pas exposer inutilement** leurs IP respectives à un observateur.

Ce n'est pas un problème nouveau. L'informatique y travaille depuis 50 ans. La question n'est pas « inventer quoi ? » mais **« quel outil du menu existant colle à nos contraintes ? »**

---

## Contraintes Spécifiques GAFAM

Avant de parcourir les protocoles, rappel du terrain :

| Contrainte | Origine projet |
| :--- | :--- |
| Chaque VPC est **souverain** (Go + SQLite, Docker, IP propre) | `vpc-relay/` |
| Pas de hub central de confiance obligatoire | Manifest 1, 6 |
| Le portail Cloudflare **ne stocke pas les messages** | Manifest 6, 8 |
| Recovery possible **sans téléphone relais** sous la main | Manifest 5 |
| GSM peut être **coupé** | Manifest 5 cas N°1 |
| Connexion directe VPC↔VPC possible mais **pas élégante seule** | Manifest 11 port 5152 |
| Store-and-forward déjà natif (SQLite, outbox) | `api.go`, manifest 7 |
| Auth web par **rendez-vous mécanique** déjà implémenté | Manifest 12 |

---

## Chronologie des Protocoles Serveur ↔ Serveur

Du plus ancien au plus récent. Pour chacun : principe, forces, faiblesses **dans le contexte de deux VPC GAFAM**.

---

### ère 0 — Avant le réseau (années 1960–1970)

#### Support physique / Sneakernet
- **Principe :** On copie les données sur un support, on le transporte physiquement.
- **Forces :** Aucune dépendance réseau. Air-gap total.
- **Faiblesses :** Pas scalable. Incompatible avec recovery à distance.
- **Verdict GAFAM :** ❌ Hors scope.

#### (1976)
- **Principe :** Store-and-forward par téléphone modem ou réseau. Chaque nœud relaie les fichiers vers la destination finale. *« Le réseau est l'ordinateur de ton voisin. »*
- **Forces :** **Asynchrone natif.** Pas besoin que les deux extrémités soient online. Fondation historique du DTN.UUCP — Unix-to-Unix Copy 
- **Faiblesses :** Configuration complexe (fichiers `L.sys`, chemins). Quasi disparu.
- **Verdict GAFAM :** ⚠️ **Esprit pertinent** (manifest 7 DTN) mais protocole mort. Le principe store-and-forward est déjà dans SQLite.

---

### ère 1 — Les premiers réseaux (années 1970–1985)

#### FTP — File Transfer Protocol (1971)
- **Principe :** Connexion TCP directe. Un serveur, un client. Transfert de fichiers.
- **Forces :** Simple. Ubiquitaire.
- **Faiblesses :** Synchrone. IP exposée. Pas d'auth forte par défaut. Passive/active mode = cauchemar NAT.
- **Verdict GAFAM :** ❌ Remplacé par HTTP/SSH pour tout ce qu'on en ferait.

#### Telnet / rlogin (1969 / 1983)
- **Principe :** Session distante en clair. Modèle « je me connecte à ta machine ».
- **Forces :** Interaction directe.
- **Faiblesses :** Clartext. Modèle client→serveur asymétrique.
- **Verdict GAFAM :** ❌ Obsolète. SSH l'a tué.

#### SMTP — Simple Mail Transfer Protocol (1982)
- **Principe :** Relais de messages asynchrone via serveurs MX intermédiaires. Store-and-forward en cascade.
- **Forces :** **Asynchrone.** Les deux extrémités n'ont pas besoin d'être online. Infrastructure mondiale.
- **Faiblesses :** Headers `From:` / `Received:` **trivialement falsifiables**. SPF/DKIM/DMARC = rustines. Le relay voit les métadonnées. Latence. Spam.
- **Verdict GAFAM :** ❌ **Rejeté explicitement** — c'est le contre-exemple du projet.

#### X.400 (1984)
- **Principe :** Email « sérieux » avec adressage structuré (ITU-T). Authentification plus formelle que SMTP.
- **Forces :** Métadonnées riches. Intégrité partielle.
- **Faiblesses :** Complexité monstrueuse. Disparu du monde civil (survit en militaire/santé).
- **Verdict GAFAM :** ❌ Mort pour une raison.

---

### ère 2 — La cryptographie arrive (années 1985–2000)

#### Kerberos (1988)
- **Principe :** Ticket d'accès délivré par un **KDC central** (Key Distribution Center). Les services font confiance au KDC, pas les uns aux autres directement.
- **Forces :** Single Sign-On. Pas de mot de passe qui transite.
- **Faiblesses :** **Hub central obligatoire.** Si le KDC tombe, tout tombe. Contraire à la philosophie GAFAM.
- **Verdict GAFAM :** ❌ Trop centralisé.

#### PGP / GPG — Pretty Good Privacy (1991)
- **Principe :** Chiffrement asymétrique (clé publique/privée). Messages chiffrés déposés sur n'importe quel canal (email, fichier, BBS). **Web of Trust** : les humains signent les clés des autres en personne.
- **Forces :** **Async.** Le transport est aveugle. Pas de `From:` forgeable si signature vérifiée. WoT = co-présence physique pour établir la confiance.
- **Faiblesses :** UX catastrophique. Gestion des clés pénible. WoT jamais décollé grand public.
- **Verdict GAFAM :** ✅ **Très pertinent philosophiquement.** Le forge Poneglyph = WoT simplifié. Mais **ne pas réinventer** : le pattern « chiffrer avec clé du pair, déposer sur canal public » est exactement ça.

#### SSH — Secure Shell (1995)
- **Principe :** Session chiffrée TCP directe. Authentification par clé publique ou mot de passe. Port forwarding possible.
- **Forces :** Standard. Clé publique = identité machine. Tunneling ( `-L`, `-R` ).
- **Faiblesses :** **Synchrone** — les deux doivent être online. IP exposée des deux côtés. Un VPC doit être « serveur » (port 22 ouvert). Scanning de port visible.
- **Verdict GAFAM :** ⚠️ **Fonctionne techniquement** mais c'est le VPC↔VPC « évident » que tu n'aimes pas. Pas async. Pas élégant pour du recovery.

#### IPSec / VPN site-à-site (années 1990)
- **Principe :** Tunnel chiffré au niveau IP. Deux réseaux entiers connectés comme s'ils étaient sur le même LAN.
- **Forces :** Transparent pour les applications. Standard entreprise.
- **Faiblesses :** Configuration lourde. Les deux extrémités online. IP fixe souvent requise. Outline (manifest 11 Phase 4) = variante mobile.
- **Verdict GAFAM :** ⚠️ Prévu en Phase 4 (Outline) pour Android↔VPC, pas inter-VPC.

#### SSL/TLS (1994 → 1999)
- **Principe :** Couche de chiffrement sur TCP. Certificat serveur = identité. mTLS = les deux côtés présentent un certificat.
- **Forces :** Standard universel. mTLS = authentification mutuelle forte.
- **Faiblesses :** Synchrone. PKI ou auto-signé (pinning). C'est le port 5152 du manifest 11.
- **Verdict GAFAM :** ✅ **C'est la solution évidente du projet** (fédération mTLS). Pas besoin de réinventer — il faut juste l'implémenter.

---

### ère 3 — Messagerie et files d'attente (années 2000–2010)

#### XMPP Server-to-Server / Jabber (1999)
- **Principe :** Fédération : `alice@serveurA` envoie à `bob@serveurB` via TLS s2s entre les deux serveurs XMPP. Chaque serveur est souverain.
- **Forces :** **Fédération décentralisée.** Chaque opérateur héberge son serveur. Async (messages stockés si offline). Proche du modèle Matrix.
- **Faiblesses :** XML verbeux. Complexité s2s (certificats, dialback). Déclin face à WhatsApp/Signal.
- **Verdict GAFAM :** ✅ **Le modèle fédéré le plus proche de GAFAM.** Deux VPC = deux serveurs XMPP. Manifest 2 s'en inspire explicitement. Mais XMPP lui-même est trop lourd à embarquer.

#### MQTT — Message Queuing Telemetry Transport (1999)
- **Principe :** Pub/Sub via un **broker central**. Les clients publient sur des topics, les abonnés reçoivent. Léger.
- **Forces :** Async. Léger. Idéal IoT.
- **Faiblesses :** **Broker tiers obligatoire** qui voit les topics (métadonnées). Pas de fédération native. Le broker est un point de confiance.
- **Verdict GAFAM :** ⚠️ Pattern utile (pub/sub) mais le broker central contredit la souveraineté. Cloudflare D1 en pub/sub chiffré = variante acceptable.

#### AMQP / RabbitMQ (2003)
- **Principe :** File de messages enterprise. Producer → Queue → Consumer. Ack, retry, routing.
- **Forces :** Fiabilité. Découplage temporel (async).
- **Faiblesses :** Infrastructure lourde. Broker central. Overkill pour deux VPC.
- **Verdict GAFAM :** ❌ Trop lourd. Le principe « queue » est déjà dans SQLite outbox.

#### BitTorrent / P2P (2001)
- **Principe :** Échange de chunks entre pairs sans serveur central. DHT pour la découverte.
- **Forces :** Pas de hub. Résilient.
- **Faiblesses :** Optimisé pour gros fichiers publics, pas messages privés courts. DHT = métadonnées exposées.
- **Verdict GAFAM :** ❌ Mauvais fit.

#### Tor Hidden Services / Onion Routing (2002)
- **Principe :** Adresse `.onion` = serveur accessible sans révéler son IP. Relais intermédiaires en couches. Dead drops possibles.
- **Forces :** **IP cachée.** Async possible. Transport aveugle.
- **Faiblesses :** Latence. Dépendance au réseau Tor. Complexité d'opération.
- **Verdict GAFAM :** ⚠️ Intéressant pour cacher l'IP du VPC, mais lourd. Le coffre chiffré D1 (manifest 12) résout le même problème plus simplement.

#### Secure Scuttlebutt — SSB (2014)
- **Principe :** Chaque nœud possède sa propre **chaîne de messages signés** (append-only log). Sync par gossip quand deux nœuds se croisent (WiFi, USB, Internet). Pas de serveur.
- **Forces :** **Offline-first.** Pas de serveur central. Messages signés cryptographiquement. Sync async par rencontre.
- **Faiblesses :** La sync complète peut être lourde. Découverte de pairs difficile.
- **Verdict GAFAM :** ✅ **Très proche du besoin.** Chaque VPC = un log signé. Sync via boîte morte ou rencontre directe. Briar est construit sur SSB.

---

### ère 4 — Protocoles modernes (années 2010–2020)

#### WebRTC Data Channels (2011)
- **Principe :** P2P direct navigateur↔navigateur. Signaling via serveur tiers, puis connexion UDP directe chiffrée (DTLS).
- **Forces :** P2P réel une fois établi. Chiffrement natif.
- **Faiblesses :** NAT traversal (STUN/TURN). Signaling server requis. Les deux doivent être online au moment du handshake.
- **Verdict GAFAM :** ⚠️ Pour du temps réel (Scrcpy), pas pour de l'async recovery.

#### Matrix Protocol — Server-to-Server Federation (2014)
- **Principe :** Chaque homeserver est souverain. S2S via HTTPS + JSON. Chaque message est signé. Fédération : `@alice:A.com` → ` @bob:B.com` passe par TLS entre A et B.
- **Forces :** **Exactement le modèle GAFAM manifest 2.** Fédéré, souverain, messages signés, async (store si offline).
- **Faiblesses :** Protocole lourd (rooms, events, state resolution). Implémenter un homeserver = mois de travail.
- **Verdict GAFAM :** ✅ **Le gold standard du modèle visé.** Mais réimplémenter Matrix dans `vpc-relay` serait absurde. S'inspirer du **modèle** (fédération HTTPS signée), pas du protocole.

#### Signal Protocol / Double Ratchet (2013)
- **Principe :** Chiffrement E2E avec forward secrecy. Clés qui tournent à chaque message. X3DH pour le premier contact.
- **Forces :** Sécurité E2E maximale. Standard de facto messagerie privée.
- **Faiblesses :** Conçu pour client↔client via serveur relais (Signal/WhatsApp). Pas server-to-server. Le serveur Signal voit les métadonnées.
- **Verdict GAFAM :** ⚠️ Le Double Ratchet est overkill pour recovery. Mais X3DH (échange de clés initial) inspire le forge.

#### gRPC (2015)
- **Principe :** RPC typé sur HTTP/2 + protobuf. Client appelle une fonction sur le serveur.
- **Forces :** Performant. Typé. Streaming.
- **Faiblesses :** Synchrone. RPC = asymétrique par nature.
- **Verdict GAFAM :** ❌ Pas adapté à l'async recovery.

#### WireGuard (2018)
- **Principe :** VPN moderne, kernel-level. Clé publique = identité. Handshake minimal.
- **Forces :** Simple. Rapide. Clé publique = adresse du pair.
- **Faiblesses :** Les deux online pour le handshake. Tunnel, pas messagerie.
- **Verdict GAFAM :** ⚠️ Bon pour lier deux VPC en réseau, pas pour déposer un message recovery async.

#### Magic Wormhole (2015)
- **Principe :** Code verbal court (`7-horseman-diversity`). PAKE via serveur relais. Les deux parties dérivent la même clé sans la transmettre. Session chiffrée éphémère.
- **Forces :** **Rituel humain + PAKE + relay aveugle.** Exactement le pattern du forge Poneglyph.
- **Faiblesses :** Session **éphémère** (one-shot). Pas de canal persistant async.
- **Verdict GAFAM :** ✅ **Le plus proche du Partie 1.** À réutiliser comme base du forge, pas à réinventer.

#### Briar (2016)
- **Principe :** Messagerie P2P. Ajout de contact par **Bluetooth/NFC** (co-présence physique). Sync async via Tor ou Bluetooth quand les nœuds se croisent.
- **Forces :** **Co-présence physique pour établir la confiance.** Offline-first. Pas de serveur obligatoire.
- **Faiblesses :** App mobile lourde. Tor obligatoire pour sync à distance.
- **Verdict GAFAM :** ✅ **Le modèle produit le plus proche.** Forge physique + sync async = Briar entre deux VPC.

#### Noise Protocol Framework (2018)
- **Principe :** Framework de handshake TCP léger (XX, IK, etc.). Utilisé par WireGuard, WhatsApp.
- **Forces :** Composable. Léger. Patterns nommés pour chaque scénario.
- **Faiblesses :** Bas niveau — il faut construire par-dessus.
- **Verdict GAFAM :** ✅ Si implémentation mTLS port 5152 : utiliser Noise XX ou IK, pas réinventer TLS from scratch.

#### ActivityPub (2018)
- **Principe :** Fédération HTTP (Inbox/Outbox). Chaque serveur a une URL publique. Messages signés HTTP Signatures.
- **Forces :** Simple. HTTP natif. Fédéré.
- **Faiblesses :** Conçu pour le social web. Métadonnées publiques (URLs d'acteurs).
- **Verdict GAFAM :** ⚠️ Le pattern Inbox/Outbox async est réutilisable. L'exposition publique des URLs ne l'est pas.

---

### ère 5 — Aujourd'hui (2020+)

#### NOSTR (2020)
- **Principe :** Identité = clé pubkey. Messages signés déposés sur des **relays** interchangeables. Le relay ne peut pas falsifier (signature) ni lire (chiffrement optionnel).
- **Forces :** Relays substituables. Pas de compte. Signature = preuve d'identité sans header forgeable.
- **Faiblesses :** Métadonnées visibles par le relay (qui parle à qui). Pas de store-and-forward fiable (relay peut drop).
- **Verdict GAFAM :** ✅ **Pattern « relay aveugle + signature » très pertinent.** Le blob chiffré sur D1 + `channel_key` = variante NOSTR privée.

#### OPAQUE / PAKE moderne (2020+)
- **Principe :** Password Authenticated Key Exchange sans que le serveur ne voie jamais le mot de passe. OPRF.
- **Forces :** Établissement de clé à partir d'un secret faible, sans transmission.
- **Faiblesses :** Conçu client↔serveur, pas pair↔pair directement.
- **Verdict GAFAM :** ✅ Le forge (PBKDF2 + challenge) est un PAKE artisanal. Pour production : considérer SPAKE2 ou OPAQUE.

#### QUIC / HTTP/3 (2020)
- **Principe :** TCP+TLS remplacé par UDP chiffré natif. Connexion plus rapide.
- **Forces :** Performance. Chiffrement par défaut.
- **Faiblesses :** Toujours synchrone. Pas de messagerie.
- **Verdict GAFAM :** ⚠️ Transport pour le port 5152 si implémenté, pas un protocole de communication en soi.

#### Bundle Protocol 6 / DTN — NASA (2000s, standardisé 2007+)
- **Principe :** Messages = « bundles » stockés et relayés de nœud en nœud. Pas de chemin continu requis. Conçu pour l'espace.
- **Forces :** **Le store-and-forward ultime.** Async extrême (fenêtres de secondes par mois).
- **Faiblesses :** Complexité. Convergence lente. Peu d'implémentations civiles.
- **Verdict GAFAM :** ⚠️ Manifest 7 en est inspiré. SQLite outbox = BP6 artisanal. Pas besoin du protocole complet.

---

## Tableau Synthétique : Quel Protocole pour Quel Besoin GAFAM ?

| Besoin GAFAM | Protocole historique équivalent | Réutiliser tel quel ? |
| :--- | :--- | :--- |
| Recovery async sans GSM | PGP dead drop / UUCP / SMTP (sans les défauts) | Pattern, pas le protocole |
| Établir la confiance initiale | PGP WoT / Bluetooth SAS / Magic Wormhole | ✅ Wormhole + WoT |
| Canal persistant entre deux VPC | XMPP s2s / Matrix / Briar sync | ✅ Modèle Briar/Matrix |
| Messages signés, relay aveugle | NOSTR / PGP | ✅ Pattern NOSTR |
| Connexion directe VPC↔VPC | SSH / mTLS / WireGuard | ✅ mTLS port 5152 (manifest 11) |
| Cacher l'IP du VPC | Tor onion / coffre D1 chiffré | ✅ Coffre D1 (déjà fait) |
| Store-and-forward offline | UUCP / BP6 / AMQP | ✅ SQLite outbox (déjà fait) |
| Échange de clé sans transmission | PAKE / Magic Wormhole / OPAQUE | ✅ PAKE au forge |
| Headers non forgeables | Signatures PGP / NOSTR | ✅ AES-GCM tag (déjà fait) |

---

## Ce que GAFAM a Déjà (et qui suffit)

En relisant tout le projet, **la majorité des briques existent déjà** :

```
┌────────────────────────────────────────────────────────────┐
│  DÉJÀ IMPLÉMENTÉ                                           │
├────────────────────────────────────────────────────────────┤
│  Store-and-forward      → SQLite outbox (api.go)             │
│  Boîte morte aveugle    → Cloudflare D1 coffres (manifest 12)│
│  Chiffrement applicatif → AES-256-GCM (api.go)              │
│  Rendez-vous mécanique  → Challenge HH:MM + clics (manifest 12)│
│  Auth sans mot de passe → PBKDF2 500k (manifest 12)        │
│  Android relay async    → Polling outbox 1s (api.go)       │
│  Gardiens recovery      → trusted_guardians (api.go)       │
│  Honeypots / rate limit → D1 + startHoneypotGenerator      │
├────────────────────────────────────────────────────────────┤
│  DOCUMENTÉ MAIS PAS CODÉ                                    │
├────────────────────────────────────────────────────────────┤
│  Fédération mTLS        → Port 5152 (manifest 11)          │
│  VPC-to-VPC recovery    → Manifest 5 cas N°1               │
│  DTN / store-and-forward long → Manifest 7                   │
│  Shamir secret sharing  → Manifest 5                       │
└────────────────────────────────────────────────────────────┘
```

---

## Verdict Honnête : Qu'est-ce qui Manque Vraiment ?

Après ce tour complet, **il ne manque pas un 5ᵉ protocole exotique**. Il manque **l'assemblage** de trois patterns qui marchent déjà très bien :

### 1. Le Forge = Magic Wormhole + Bluetooth SAS
Deux humains ensemble, geste synchronisé, clé dérivée sans transmission.
→ **Ne pas réinventer.** Implémenter SPAKE2 ou reprendre le PAKE de Wormhole, avec le challenge mécanique (manifest 12) comme input.

### 2. Le Canal = NOSTR privé + PGP dead drop
Blob chiffré sur D1, relay aveugle, lecture destructive, numérotation `seq`.
→ **Ne pas réinventer.** C'est exactement le coffre manifest 12, mais **persistant** et **bidirectionnel** entre deux paires.

### 3. La Fédération = Matrix s2s simplifié (port 5152)
HTTPS + signature entre VPC pour les messages à haut débit.
→ **Ne pas réinventer.** mTLS + Noise XX. C'est le manifest 11. Il faut juste le coder.

### Ce qu'on peut jeter du Partie 1

| Élément Partie 1 | Verdict |
| :--- | :--- |
| Nom « Poneglyph » | Garder comme nom produit, pas comme mécanisme crypto |
| HDC comme couche transport | ❌ Jeter pour le MVP — overkill, limitations connues |
| « 5ᵉ plan réseau sémantique » | ❌ Reformuler — c'est un **canal de confiance pré-établi**, pas un nouveau réseau |
| Rituel forge détaillé | ✅ Garder — c'est le seul morceau UX genuinely GAFAM |
| Boîte morte D1 | ✅ Garder — déjà là, éprouvé |

---

## Recommandation Finale (Partie 2)

**Arrêter de chercher un protocole nouveau.** Le menu est complet depuis 30 ans.

Le plan d'action minimal et élégant pour deux VPC qui veulent communiquer :

```
ÉTAPE 1 — FORGE (one-time, co-présence)
  Pattern : Magic Wormhole PAKE + manifest 12 challenge
  Output  : channel_key partagé, stocké localement dans les deux VPC

ÉTAPE 2 — CANAL ASYNC (recovery, signaux)
  Pattern : NOSTR relay privé + PGP dead drop
  Transport : Cloudflare D1 (déjà en place)
  Format : AES-GCM blob + seq (déjà en place)

ÉTAPE 3 — FÉDÉRATION DIRECTE (messagerie, haut débit)
  Pattern : Matrix s2s / mTLS
  Transport : Port 5152 (manifest 11, à implémenter)
  Format : HTTPS + signature VPC

ÉTAPE 4 — FALLBACK (GSM coupé, pas de forge préalable)
  Pattern : Briar (sync à la prochaine rencontre physique)
  Ou : Manifest 5 cas N°2 (ADB + Signal)
```

**Le seul morceau genuinely nouveau dans tout GAFAM**, c'est le **rendez-vous mécanique** (manifest 12) appliqué comme rituel de forge entre deux personnes. Tout le reste est de l'assemblage intelligent de briques de 1991 (PGP), 1999 (XMPP), 2015 (Wormhole) et 2016 (Briar).

Le Partie 1 de ce manifeste reste valide comme **spécification produit** du forge et du canal async. Mais la fondation technique, elle, existe déjà. Il faut la brancher, pas la réinventer.

---

# Partie 3 — La Boîte Auto-Publieuse & les Cercles de Liens

> *On abandonne le paradigme « envoyer un message ». On adopte le paradigme « publier chez soi, l'autre vient lire ». Pas de recovery ici — uniquement la messagerie dans le temps et l'espace.*

---

## Le Renversement Fondamental

Les Parties 1 et 2 cherchaient encore **comment faire voyager l'information** d'un VPC vers un autre. C'était du plumbing réseau déguisé.

La Partie 3 part d'un constat plus radical, inspiré par les contraintes du voyage spatial (délais de lumière, fenêtres de communication, absence de lien continu) mais valable sur Terre :

> **On n'envoie rien. On écrit sur son propre espace. Le destinataire vient chercher.**

Ce n'est pas de la messagerie. C'est de la **publication adressée** — comme laisser une lettre sur sa propre porte d'entrée, avec le nom du destinataire sur l'enveloppe. Le destinataire, s'il connaît l'adresse, passe la prendre quand il veut.

---

## Le Principe : Publier chez soi, lire chez les autres

### Ce que fait l'expéditeur (Gary, `+33606`)

1. Gary ouvre son Web Client sur `06.gafam.cloud`.
2. Il rédige un message **à l'intention de Bob** (`+33607`).
3. Il clique sur **« Publier »** — pas sur « Envoyer ».
4. Son VPC enregistre l'enveloppe **sur son propre espace public** (`06.gafam.cloud/feed`).
5. Gary n'a aucune idée si Bob est online. Il n'a poussé aucun paquet vers Bob. Il a simplement **affiché**.

### Ce que fait le destinataire (Bob, `+33607`)

1. Bob ouvre son Web Client sur `07.gafam.cloud`.
2. Son client lance un **scan de ses Liens** — un flux RSS de tous ses contacts acceptés.
3. Pour chaque Lien (ex: `06.gafam.cloud/feed`), il cherche les enveloppes dont l'intitulé (`for`) correspond à son numéro.
4. S'il trouve une nouvelle enveloppe de Gary → il la lit, la déchiffre, l'affiche.
5. Pour répondre, Bob **publie chez lui** (`07.gafam.cloud/feed`) une enveloppe adressée à Gary.
6. Gary, lors de son prochain scan, trouvera la réponse.

```
Gary publie chez Gary          Bob scanne les Liens
┌─────────────────┐            ┌─────────────────┐
│ 06.gafam.cloud  │            │ 07.gafam.cloud  │
│                 │            │                 │
│ [Pour Bob]      │◄── scan ───│ RSS des Liens   │
│  "Salut..."     │            │ → lit Gary      │
│                 │            │                 │
│ [Pour Alice]    │            │ [Pour Gary]     │
│  "..."          │── scan ───►│  "Réponse..."   │
└─────────────────┘            └─────────────────┘
```

**Aucun VPC ne parle à un autre VPC.** Chaque VPC ne fait que servir **son propre flux**. La communication est une **lecture croisée**, pas un envoi.

---

## L'Enveloppe : Message sans envoi

Chaque publication est une **enveloppe** — un message adressé, publié sur l'espace de l'auteur, jamais transmis :

```json
{
  "id": "env_a7f3c2...",
  "from": "+33606",
  "for": "+33607",
  "published_at": "2026-07-07T22:30:00Z",
  "circle_id": "circle_abc",
  "payload": "<AES-GCM chiffré pour la pubkey de Bob>",
  "signature": "<signature VPC Gary>"
}
```

| Champ | Rôle |
| :--- | :--- |
| `from` | Qui a écrit (vérifiable par signature) |
| `for` | À qui c'est adressé (filtre RSS côté lecteur) |
| `payload` | Contenu chiffré — seul le destinataire lit |
| `circle_id` | Cercle de Liens dans lequel cette enveloppe est visible |
| `signature` | Preuve que Gary a bien écrit ça |

L'enveloppe est **publique dans sa forme** (on voit qu'il y a un message de Gary pour Bob) mais **privée dans son contenu** (seul Bob déchiffre). Pas de header SMTP forgeable — la signature VPC est la preuve d'origine.

---

## Les Liens : Établir qui peut lire qui

Deux personnes doivent **se connaître mutuellement** pour entrer dans le flux de l'autre. C'est le système d'**Invitations**.

### Création d'un Lien

1. Gary envoie une **invitation** à Bob (via SMS classique, QR en personne, ou publication d'invitation sur `06.gafam.cloud/invites`).
2. Bob **accepte** l'invitation depuis son Web Client.
3. Les deux VPC enregistrent le Lien dans leur table locale :

```sql
CREATE TABLE links (
  id           TEXT PRIMARY KEY,
  peer_phone   TEXT NOT NULL,
  peer_feed_url TEXT NOT NULL,   -- ex: https://06.gafam.cloud/feed
  peer_pubkey  TEXT NOT NULL,
  circle_id    TEXT,              -- cercle partagé (optionnel)
  status       TEXT DEFAULT 'active',  -- pending | active | revoked
  created_at   DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

4. Le client de Bob ajoute `06.gafam.cloud/feed` à sa **liste de scan RSS**.

Sans Lien accepté, Bob ne scanne pas le flux de Gary. Gary ne peut pas publier pour quelqu'un qui ne l'a pas accepté (l'enveloppe existe mais le destinataire ne la cherche pas).

---

## Le Scan RSS : Comment le client trouve les messages

Le Web Client de Bob exécute périodiquement (ou à la demande) un **scan de Liens** :

```
Pour chaque Lien actif dans links :
  1. GET peer_feed_url (ex: 06.gafam.cloud/feed?since=last_seen)
  2. Pour chaque enveloppe retournée :
     a. Si envelope.for == mon_numéro → candidat
     b. Vérifier signature VPC du pair
     c. Déchiffrer payload avec ma privkey
     d. Si OK → afficher dans ma boîte de réception
     e. Mettre à jour last_seen pour ce Lien
```

C'est un **flux RSS classique**, filtré par `for`. Pas de WebSocket. Pas de push. Bob choisit **quand** il scanne — pendant sa fenêtre de communication, le matin, quand il ouvre l'app.

L'UX par défaut du client :

> *« J'ai publié un message pour Bob sur mon espace. Maintenant je vais scanner mes Liens pour voir si quelqu'un m'a écrit. »*

Publier et lire sont **deux gestes symétriques**. On ne « envoie » jamais.

---

## Les Cercles de Liens : La boîte partagée du groupe

### Le besoin

Deux contacts en tête-à-tête suffisent pour du 1-to-1. Mais si Gary est en déplacement (voyage spatial, pays sans réseau, décalage horaire extrême), un **cercle de Liens mutuels** permet une propagation plus rapide :

> Tous les membres d'un même Cercle peuvent voir les **dernières publications** de chaque membre — pas seulement les enveloppes qui leur sont adressées personnellement.

### Principe

Un **Cercle** est un groupe de Liens mutuels (3 à N personnes) qui acceptent de partager leur flux entre eux :

```
Cercle "Équipage Mars" : Gary + Bob + Alice + Carol

Gary publie sur 06.gafam.cloud/feed
  → Bob scanne (Lien direct)
  → Alice scanne (Lien direct)
  → Carol scanne (Lien direct)
  → Si Bob est offline mais Alice lit le message de Gary,
    Alice le voit dans le flux du Cercle
  → Quand Bob scanne plus tard, il peut aussi interroger
    le cache du Cercle (circle.gafam.cloud/crew-mars)
```

### La boîte interne partagée du Cercle

Chaque Cercle dispose d'un **espace miroir** sur le portail :

```
URL : {hash_cercle}.circle.gafam.cloud
     ou : 06-07-08.circle.gafam.cloud (phones triés)
```

Ce n'est pas un espace d'écriture — c'est un **agrégat de lecture** :
- Chaque membre publie toujours **chez lui** (`06.gafam.cloud/feed`).
- Le VPC de chaque membre **pousse une copie chiffrée** de ses dernières publications vers l'agrégat du Cercle (optionnel, configurable).
- Les autres membres peuvent scanner **le Cercle** en un seul GET au lieu de scanner chaque Lien individuellement.

```
Gary publie chez lui
       │
       ├─► 06.gafam.cloud/feed          (source souveraine)
       │
       └─► crew-mars.circle.gafam.cloud (miroir du cercle)
                ▲
                │ scan unique
                │
              Bob (offline 4h, puis revient)
              → un seul GET sur le cercle
              → voit tout ce que Gary, Alice, Carol ont publié
```

### Pourquoi c'est plus rapide en déplacement

Sur Terre avec décalage, ou dans l'espace avec des fenêtres de communication :

| Sans Cercle | Avec Cercle |
| :--- | :--- |
| Bob doit scanner 15 Liens un par un | Bob scanne 1 agrégat Cercle |
| Si Gary publie pendant que Bob dort, Bob attend son prochain scan | Un membre éveillé du Cercle peut relayer (cache local) |
| Propagation = vitesse du scan du destinataire | Propagation = vitesse du membre le plus réactif du Cercle |

Le Cercle ne **remplace** pas la publication souveraine chez soi. Il **accélère la découverte** — comme un tableau d'affichage commun dans une base spatiale où chacun épingle ses notes sur son casier, et le tableau central reflète les derniers casiers mis à jour.

---

## Fenêtres de Communication : Parler dans le temps

Inspiré par les contraintes spatiales (délai Mars-Terre : 4 à 24 minutes aller simple), mais applicable partout :

### Le concept

Deux contacts (ou un Cercle) définissent des **fenêtres de communication** — des plages horaires où ils s'engagent à publier et/ou scanner :

```json
{
  "circle_id": "crew-mars",
  "windows": [
    { "day": "tuesday", "publish": "14:00-14:30", "scan": "18:00-18:30" },
    { "day": "friday",  "publish": "09:00-09:15", "scan": "09:15-09:45" }
  ]
}
```

Gary publie à 14h00 (sa fenêtre). Bob scanne à 18h00 (sa fenêtre). **4 heures de décalage — aucun problème.** C'est le mode normal, pas l'exception.

Pour le voyage spatial : les fenêtres sont calées sur les **fenêtres d'antenne** (quand la sonde a du réseau). GAFAM ne combat pas la physique — il l'embrasse.

---

## Architecture Technique

### Ce que fait chaque composant

| Composant | Rôle | Ne fait PAS |
| :--- | :--- | :--- |
| **VPC Gary** | Sert `06.gafam.cloud/feed`, signe et chiffre les enveloppes | Parler au VPC de Bob |
| **VPC Bob** | Sert `07.gafam.cloud/feed`, scanne les Liens | Recevoir de push |
| **Cloudflare** | Héberge les URLs `/feed` et les agrégats Cercle | Lire le contenu (chiffré) |
| **Web Client** | Publier + scanner RSS | Envoyer des paquets |
| **Cercle agrégat** | Miroir de lecture groupée | Stocker la source de vérité |

### Endpoints VPC (nouveaux)

```
GET  /feed?since=timestamp          → liste des enveloppes publiées (publiques)
POST /feed/publish                  → publier une nouvelle enveloppe
GET  /links                         → mes Liens actifs
POST /links/invite                  → envoyer une invitation
POST /links/accept                  → accepter une invitation
DELETE /links/{id}                  → révoquer un Lien
GET  /circles                         → mes Cercles
POST /circles/create                → créer un Cercle
POST /circles/{id}/join             → rejoindre un Cercle (sur invitation)
```

### Endpoints portail (Cloudflare Worker)

```
GET  /{phone}/feed                  → proxy vers VPC ou cache edge
GET  /circle/{id}/aggregate         → agrégat des dernières publications du Cercle
POST /circle/{id}/mirror            → push miroir (depuis VPC membre)
```

---

## Schéma de Données

### Table `envelopes` (SQLite, chaque VPC)

```sql
CREATE TABLE envelopes (
  id            TEXT PRIMARY KEY,
  from_phone    TEXT NOT NULL,
  for_phone     TEXT NOT NULL,
  circle_id     TEXT,
  payload       BLOB NOT NULL,       -- chiffré pour le destinataire
  signature     BLOB NOT NULL,
  published_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
  mirrored      BOOLEAN DEFAULT FALSE -- poussé vers le Cercle ?
);
```

### Table `inbox_cache` (ce que j'ai lu chez les autres)

```sql
CREATE TABLE inbox_cache (
  id            TEXT PRIMARY KEY,
  envelope_id   TEXT NOT NULL,
  from_phone    TEXT NOT NULL,
  decrypted_body TEXT,
  read_at       DATETIME DEFAULT CURRENT_TIMESTAMP,
  source_url    TEXT NOT NULL         -- d'où j'ai trouvé cette enveloppe
);
```

---

## Comparaison avec les Parties 1 et 2

| | Partie 1 (Poneglyph) | Partie 2 (Protocoles) | **Partie 3 (Boîte Auto-Publieuse)** |
| :--- | :--- | :--- | :--- |
| Paradigme | Forge + canal chiffré | Réutiliser l'existant | **Publier chez soi, lire chez l'autre** |
| Transport | Boîte morte D1 | Mesh / mTLS / SMTP | **Flux RSS de Liens** |
| VPC parle à VPC ? | Non (D1 intermédiaire) | Oui (mTLS, SSH) | **Non — jamais** |
| Recovery | Oui (cas principal) | Oui | **Non (hors scope)** |
| Décalage temporel | Toléré | Problème | **Feature native** |
| Voyage spatial | Mentionné | Non | **Cas de design principal** |
| Originalité | Rituel forge | Aucune (assemblage) | **Inversion push→pull** |

---

## Analyse : Qu'est-ce qui est réellement nouveau ?

| Élément | Existe ailleurs ? | Spécifique GAFAM ? |
| :--- | :--- | :--- |
| Publier chez soi, l'autre lit | Solid Pods, RSS, Gemini, blogs | Namespace `+33XX.gafam.cloud` |
| Flux RSS de contacts | Google Reader, Feedly, ActivityPub | Filtre `for: mon_numéro` + chiffrement |
| Cercles de confiance | Google Circles, WhatsApp groups | Agrégat miroir + propagation en déplacement |
| Fenêtres de communication | NASA DSN scheduling | Timer relatif (manifest 15) + UX web |
| Enveloppe sans envoi | Pas d'équivalent direct | Le mot « Publier » au lieu de « Envoyer » |
| VPC ne parle jamais à VPC | Rare (tout le monde fait du s2s) | **Souveraineté absolue** — chaque île est isolée |

Ce qui n'existe nulle part en produit grand public : la combinaison **publication souveraine + scan RSS de Liens + Cercles miroir + fenêtres temporelles + chiffrement par destinataire**, le tout ancré sur un numéro de téléphone.

---

## UX : Le cycle quotidien

```
1. J'ouvre mon Web Client (06.gafam.cloud)
2. Je vois : "2 nouvelles enveloppes pour toi" (depuis mon dernier scan)
3. Je les lis
4. Je rédige une réponse pour Bob → [Publier sur mon espace]
5. Je clique [Scanner mes Liens] → cherche si d'autres m'ont écrit
6. C'est tout. Je ferme. Bob lira quand il scannera.
```

Pas de notification push obligatoire. Pas de « en ligne / hors ligne ». Pas de double check bleu. Juste : **tu as publié, il lira.**

Optionnel : un membre du Cercle peut activer un **wake minimal** (un bit `has_new` sur l'agrégat) — dégradé uniquement, jamais le mode par défaut. Le rituel reste la norme.

---

## Synchronisation par Rituel (Lien avec le Manifest 12)

La question « comment Bob sait-il qu'il doit scanner ? » n'a pas besoin de push, de notification, ni de WebSocket. La réponse est déjà dans GAFAM : **le rendez-vous mécanique** (manifest 12) et le **timer relatif** (manifest 15).

### Le rituel remplace le push

| Push (classique) | Rituel (GAFAM) |
| :--- | :--- |
| « Hé, viens maintenant ! » | « On se retrouve mardi à 18h36 » |
| Dépend d'un serveur de notif | Dépend d'un accord humain |
| Urgence permanente | Calme par design |
| L'autre doit être réveillé | Chacun vient à sa fenêtre |

Le push dit *quand* lire. Le rituel **conviend** de *quand* lire — à l'avance, comme un rendez-vous au café. Le café n'appelle personne.

### Le cycle rituel complet

```
ACCORD     Gary et Bob (ou le Cercle) conviennent d'une fenêtre
           Ex: « Mardi — Gary publie à 18h36, Bob scanne à 18h36 »
           Ou timer relatif (manifest 15): « Dans 90 secondes — 6 impulsions »

RITUEL     À l'heure convenue, les deux exécutent le geste mécanique
           (manifest 12): compte à rebours → impulsions → fenêtre ouverte

PUBLIER    Gary publie ses enveloppes sur 06.gafam.cloud/feed

SCANNER    Bob scanne ses Liens (ou l'agrégat Cercle)

LIRE       Bob trouve les enveloppes `for: +33607`, déchiffre, lit

RÉPONDRE   Bob publie sa réponse sur 07.gafam.cloud/feed

FERMETURE  La fenêtre se ferme. Silence jusqu'au prochain rendez-vous.
```

Aucun paquet n'a voyagé entre VPC. Aucune notification n'a été poussée. Le **temps partagé** était le seul bus de synchronisation.

### Trois niveaux de rituel

| Niveau | Qui | Exemple |
| :--- | :--- | :--- |
| **Lien bilatéral** | Deux contacts | Gary et Bob : challenge à deux (forge Partie 1) puis fenêtre hebdomadaire |
| **Cercle** | Groupe de Liens | « L'équipage Mars scanne le vendredi 09h00 UTC » |
| **Fenêtre d'antenne** | Contrainte physique | Sonde spatiale : publie quand le réseau est up, Terre scanne quand l'antenne DSN est orientée |

Le décalage Mars-Terre (4 à 24 minutes de lumière) ne casse rien : Gary publie à *sa* fenêtre, Bob scanne à *la sienne*, quatre heures plus tard. **C'est le mode normal.**

### Lien avec les autres manifestes

| Manifeste | Rôle dans la synchronisation |
| :--- | :--- |
| **12** (Rendez-vous mécanique) | Définit *quand* la fenêtre s'ouvre — heure + impulsions |
| **15** (Fuseaux horaires) | Timer relatif (« dans 90 secondes ») pour éviter les décalages |
| **Partie 1** (Forge Poneglyph) | Rituel initial pour établir un Lien de confiance |
| **Partie 3** (cette section) | *Quoi* publier et *où* lire — le rituel dit *quand* |

### La boucle fermée du manifeste 17

```
Partie 1  →  Comment établir la confiance (forge, co-présence)
Partie 2  →  Pourquoi ne pas réinventer le réseau (cartographie)
Partie 3  →  Comment communiquer (publier chez soi, lire chez l'autre)
Rituel    →  Quand communiquer (rendez-vous mécanique, manifest 12)
```

> **Publier chez soi. Lire chez l'autre. Se retrouver dans le temps.**

Ce n'est pas trois idées. C'est un seul système — poétique, souverain, et adapté au décalage temporel comme mode par défaut.

---

## Roadmap (Partie 3)

### Phase 1 — 1-to-1 par Liens

- [ ] Table `links` + `envelopes` dans SQLite
- [ ] `GET /feed` et `POST /feed/publish` sur le VPC
- [ ] Système d'invitations (accept/revoke)
- [ ] Scanner RSS côté Web Client
- [ ] Chiffrement E2E par destinataire (pubkey)

### Phase 2 — Cercles

- [ ] Table `circles` + agrégat miroir
- [ ] `circle.{hash}.gafam.cloud` sur Cloudflare
- [ ] Scan groupé (un GET pour tout le Cercle)

### Phase 3 — Fenêtres temporelles & rituels

- [ ] Configuration de fenêtres par Cercle ou par Lien
- [ ] Timer relatif (manifest 15)
- [ ] Intégration rendez-vous mécanique (manifest 12) comme déclencheur publish/scan
- [ ] Wake minimal optionnel (`has_new` bit) — mode dégradé uniquement

---

## Synthèse Partie 3

> **On ne communique pas. On publie chez soi, on va lire chez les autres, et on se retrouve dans le temps.**

Le VPC n'est plus un nœud réseau qui envoie des paquets. C'est une **porte d'affichage souveraine** — un flux RSS personnel chiffré, scanné par ceux qui ont accepté le Lien, **à l'heure du rendez-vous convenu**.

Les Cercles accélèrent la découverte sans sacrifier la souveraineté : chacun publie toujours chez soi, le miroir du Cercle n'est qu'un raccourci de lecture.

Le décalage temporel n'est pas un bug à patcher. C'est le mode de communication par défaut — sur Terre comme sur Mars. Le rituel (manifest 12) est le calendrier ; le scan est la visite.

*Recovery, GSM, mesh : hors scope de cette partie (manifestes 5, 11, Partie 1). La Partie 3 + le rituel = la messagerie du futur décalé.*