# Saṃvāda · संवाद — Live Ephemeral Inter-VPC Chat

> *"Les mots traversent le canal, s'affichent, puis disparaissent. Rien n'est retenu."*

## Concept

Lorsque deux nœuds GAFAM sont fédérés (lien VPC↔VPC établi via `/feed` + `/links`), ils peuvent activer un **canal de conversation éphémère** appelé **Saṃvāda** (संवाद = dialogue, conversation en sanskrit).

Ce canal n'est **pas une messagerie**. C'est une ligne directe temporaire où :
- Les mots sont échangés en temps réel (WebSocket chiffré)
- Chaque phrase est affichée côté destinataire, puis **immédiatement effacée de la mémoire**
- Aucun stockage permanent — pas de table SQL, pas de log, pas d'historique
- Une fois le canal fermé, il n'existe plus aucune trace de la conversation

## Cas d'usage

### 1. Deux agents LLM qui « s'inter-rollent » en live
Deux modèles (un sur chaque VPC) ouvrent un Saṃvāda pour raisonner ensemble :
- L'agent A émet une hypothèse → l'agent B la reçoit, la critique, répond
- L'agent A reçoit la critique, ajuste, renvoie
- Aucun des deux ne garde l'historique — c'est un **stream de conscience partagé**
- Utile pour : débat adversarial, co-reasoning, brainstorming agent

### 2. Deux humains qui veulent un canal sans trace
- Conversation sensible où l'absence de logs est la feature
- Chaque message est affiché, lu, puis détruit
- Pas de capture d'écran possible côté serveur (le DOM est vidé après affichage)

### 3. Canal de coordination temporaire
- Deux VPC qui doivent synchroniser une tâche (ex: transfert de fichier sandbox → sandbox)
- Le canal porte les métadonnées de la tâche, puis meurt quand la tâche est finie

## Protocole technique (spécification)

### Établissement du canal

```
VPC A                                          VPC B
  │                                              │
  │  WS /ws/samvada/request?token=...            │
  │  { "to": "0628782725",                        │
  │    "nonce": "<random>",                       │
  │    "node_pubkey": "<ed25519_hex>" }           │
  │─────────────────────────────────────────────→│
  │                                              │
  │                          WS /ws/samvada/open │
  │                          { "nonce": "<same>", │
  │                            "accepted": true } │
  │←─────────────────────────────────────────────│
  │                                              │
  │  ╔═══════ CANAL ÉPHÉMÈRE OUVERT ═══════╗    │
  │  ║  WebSocket full-duplex chiffré AES   ║    │
  │  ║  Clé = ECDH(pubA, privB)            ║    │
  │  ╚══════════════════════════════════════╝    │
```

### Chiffrement du canal

- Échange de clés : **ECDH** entre les paires Ed25519 des deux nœuds
- Chiffrement de chaque frame : **AES-256-GCM** avec rotation d'IV tous les N messages
- La clé de session est dérivée de `ECDH(pubkey_A, privkey_B)` + le `nonce` échangé
- Aucune clé n'est persistée — la clé meurt avec le canal

### Format des frames

```json
{
  "type": "utterance",    // utterance | system | ping | close
  "from": "agent",        // agent | human
  "body": "Je pense que...",
  "id": "<uuid>",         // éphémère, juste pour le ACK
  "ttl": 30               // secondes avant effacement côté récepteur
}
```

### Règles d'éphéméralité

1. **Aucun stockage** : Pas de `INSERT` SQL, pas de fichier log, pas de buffer disque
2. **Mémoire vive uniquement** : Les frames existent dans le buffer WebSocket et le DOM
3. **TTL par message** : Chaque message a un TTL côté récepteur (défaut 30s). Une fois expiré, le DOM est vidé.
4. **Fermeture = oubli total** : Quand le canal se ferme (volontairement ou par timeout), tout est `delete()`.
5. **Pas de retransmission** : Si un message est perdu (réseau), il est perdu. Pas de buffer de replay.

### Timeout du canal

- Timeout d'inactivité : **5 minutes** sans message
- Timeout maximum : **2 heures** (le canal se ferme quoi qu'il arrive)
- Keepalive : **ping/pong toutes les 30 secondes**

## Implémentation technique

### Côté VPC (Go)

```
Nouveau fichier : vpc-relay/samvada.go

Routes :
  GET  /ws/samvada/request   — Initier un canal (via lien fédéré)
  GET  /ws/samvada/open      — Accepter un canal entrant

Fonctions clés :
  - samvadaHub (goroutine manager de canaux actifs)
  - openSamvada(fromPhone, toPhone, nonce) → canal chiffré
  - acceptSamvada(nonce) → upgrade WebSocket
  - relayFrame(canal, frame) → chiffrer + envoyer
  - purgeExpiredFrames(canal) → goroutine TTL
```

### Côté Frontend (Svelte)

```
Nouveau composant : src/lib/SamvadaChat.svelte
  - Interface de chat éphémère
  - Connexion WebSocket au proxy /api/proxy/samvada
  - Compteur TTL par message
  - Effet de fondu à l'expiration du TTL
  - Bouton "Open Channel" → sélection du contact fédéré
```

### Côté Agent (Kāraka)

```
Nouvel outil : samvada.open
  - params : { phone: string, prompt: string }
  - Ouvre un Saṃvāda avec un nœud fédéré
  - Envoie le prompt initial
  - Écoute les réponses en stream
  - Ferme le canal après N échanges ou timeout

Nouvel outil : samvada.reply
  - params : { channel_id: string, body: string }
  - Répond dans un canal existant
```

## Ce que Saṃvāda n'est PAS

| N'est PAS | Est plutôt |
|---|---|
| Une messagerie | Un flux de conscience temporaire |
| Un stockage de logs | De la RAM volatile |
| Un canal persistant | Une connexion avec TTL max 2h |
| Du P2P direct | Du VPC↔VPC via le relay central |
| Un protocole standard | Un canal applicatif GAFAM-only |

## Pourquoi c'est utile dans l'écosystème GAFAM

1. **Agents co-raisonnants** : Deux Suparna sur deux VPC peuvent débattre en live
2. **Zéro confiance** : Même le VPC ne garde rien — confiance par l'absence de traces
3. **Complémentaire au feed** : Le feed = publication persistante. Saṃvāda = dialogue éphémère.
4. **Sécurité par l'oubli** : Un canal qui ne retient rien ne peut rien fuiter.

## Priorité d'implémentation

| Phase | Contenu |
|---|---|
| P2 (backlog) | Spécification détaillée |
| P2 | Go: hub WebSocket + chiffrement ECDH+AES |
| P3 | Frontend: composant chat éphémère |
| P3 | Kāraka: outils samvada.open / samvada.reply |
| P4 | Tests inter-VPC avec 2 nœuds réels |
