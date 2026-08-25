# 01 — Histoire & Physique des Réseaux : De la 2G à la 6G sous le Capot

> **Guide d'ingénierie radio et protocole cœur de réseau.**
> Format : Questions & Réponses techniques approfondies, analyses de flux, décompositions de trames et tables fréquentielles.

---

## 📡 1. L'Évolution des Cœurs de Réseau (Core Network)

### Q : Quelle est la différence fondamentale d'architecture entre le cœur 2G/3G, le 4G EPC et le 5G Standalone (5GC) ?

Pour comprendre comment un smartphone ou un conteneur émulé dialogue avec un opérateur, il faut comprendre ce qui se trouve derrière l'antenne radio :

```
[2G/3G : Commutation de Circuits + Paquets]
  UE (Mobile) <---> BTS/NodeB <---> BSC/RNC <---> MSC / VLR (Voix CS)  <---> HLR / AuC
                                            <---> SGSN <---> GGSN (Data PS) <---> Internet

[4G LTE : Tout-IP / EPC (Evolved Packet Core)]
  UE (Mobile) <---> eNodeB <===================> MME (Contrôle / NAS)  <---> HSS (Abonnés)
                                           \---> SGW <---> PGW (Data)  <---> Internet / IMS

[5G Standalone : Service-Based Architecture (SBA)]
  UE (Mobile) <---> gNodeB <===================> AMF (Mobilité / NAS)  <---> UDM / AUSF
                                           \---> SMF (Session) <---> UPF <---> Data Network
                                           \---> SMSF, PCF, NRF (Microservices HTTP/2)
```

1. **2G (GSM) & 3G (UMTS)** : Architecture bicéphale.
   - **Circuit Switched (CS)** : Géré par le **MSC** (*Mobile Switching Center*) et le **VLR** (*Visitor Location Register*). Un canal dédié physique/temporel est réservé pour chaque appel voix ou SMS. Protocoles : **SS7 / SIGTRAN** (MAP, ISUP, TCAP).
   - **Packet Switched (PS)** : Ajouté avec GPRS/EDGE. Géré par le **SGSN** (*Serving GPRS Support Node*) et le **GGSN** (*Gateway GPRS Support Node*). Protocole d'encapsulation : **GTP-U / GTP-C** (*GPRS Tunnelling Protocol*).
2. **4G (LTE / EPC - Evolved Packet Core)** : Rupture totale, abandon du Circuit Switched.
   - **MME** (*Mobility Management Entity*) : Cerveau du plan de contrôle. Gère la signalisation NAS, l'authentification et le tracking de localisation.
   - **SGW** (*Serving Gateway*) & **PGW** (*Packet Data Network Gateway*) : Routeurs de paquets data haute vitesse avec allocation d'adresses IP.
   - **HSS** (*Home Subscriber Server*) : Base de données centrale des abonnés (fusion du HLR et de l'AuC).
   - Protocole de signalisation interne : **Diameter** (successeur de RADIUS/SS7 sur TCP/SCTP) remplace SS7.
3. **5G Standalone (5GC - 3GPP Release 15/16/17)** : Architecture orientée microservices (**SBA** - *Service-Based Architecture*).
   - Les entités physiques sont remplacées par des **Network Functions (NF)** conteneurisées (Docker/Kubernetes).
   - Communication interne : **REST APIs sur HTTP/2 avec sérialisation JSON** et TLS 1.3.
   - **AMF** (*Access and Mobility Management Function*) : Équivalent moderne du MME.
   - **SMF** (*Session Management Function*) : Gère l'allocation IP et les tunnels UPF.
   - **UPF** (*User Plane Function*) : Plan de données pur, ultra-optimisé (programmable via P4 ou eBPF).
   - **UDM** (*Unified Data Management*) & **AUSF** (*Authentication Server Function*) : Remplacent le HSS.
   - **SMSF** (*SMS Function*) : Gère les SMS natifs sans passer par le plan data.

---

### Q : Qu'est-ce que le protocole SS7 et pourquoi la 2G/3G était-elle une passoire de sécurité ?

**SS7** (*Signaling System No. 7*) a été conçu dans les années 1970 sous l'hypothèse (aujourd'hui absurde) que **seuls les opérateurs d'État légitimes avaient accès au réseau**.

Dans le réseau mondial SS7 :
- Aucun chiffrement entre commutateurs opérateurs.
- Aucune authentification d'origine des messages.
- Si un attaquant achète un accès au réseau SS7 (via un petit opérateur corrompu ou un broker de télécom aux îles Fidji/Guernesey), il peut injecter des messages **MAP** (*Mobile Application Part*) :
  - `MAP_SEND_ROUTING_INFO_FOR_SM` (SRI-SM) : Demande au HLR la localisation exacte (Cell ID) et le MSC où est connecté un numéro de téléphone cible.
  - `MAP_UPDATE_LOCATION` : Fait croire au HLR mondial que le numéro cible se trouve sur un faux MSC contrôlé par l'attaquant. Résultat : **interception totale et silencieuse de tous les SMS entrants (codes 2FA de banque, tokens OTP WhatsApp/Signal)**.
  - `MAP_PROVIDE_SUBSCRIBER_INFO` (PSI) : Géolocalise en temps réel la cellule radio de la victime.

*En 4G/5G, Diameter et les firewalls SEPP (Security Edge Protection Proxy en 5G) filtrent ces requêtes, mais les attaques de downgrade radio (forcer le smartphone en 2G) permettent encore aujourd'hui de réactiver ces failles.*

---

## 📻 2. Les Fréquences Radio et Bandes Mondiales

### Q : Comment sont structurées les bandes de fréquences (FDD vs TDD) et pourquoi un téléphone US ne capte pas toujours en Europe ?

Le spectre électromagnétique cellulaire s'étend de 600 MHz à plus de 40 GHz. Deux modes de duplexage existent :

1. **FDD (Frequency Division Duplex)** : Utilise **deux fréquences distinctes** en simultané.
   - Une fréquence pour la voie descendante (Downlink - Tour vers Téléphone).
   - Une fréquence pour la voie montante (Uplink - Téléphone vers Tour).
   - *Exemple :* B20 (800 MHz) utilise 832–862 MHz en Uplink et 791–821 MHz en Downlink.
2. **TDD (Time Division Duplex)** : Utilise **une seule et même fréquence**, mais alterne l'émission et la réception par micro-créneaux temporels (slots de quelques millisecondes).
   - Idéal pour le trafic asymétrique (énormément de download, peu d'upload).
   - *Exemple :* Bande n78 5G (3.5 GHz) ou B38/B40 en 4G.

```
FDD : [ Fréquence Uplink Tx ] ====== Téléphone émet en continu =======>
      [ Fréquence Downlink Rx ] <===== Tour émet en continu ============

TDD : [ Fréquence Unique ] [ Slot Tx (Tel) ] [ Guard ] [ Slot Rx (Tour) ] [ Slot Rx (Tour) ] ...
```

---

### Q : Quelle est la table des bandes majeures par continent ?

```
+-----------+-------------------------+-----------------------------------+-----------------------------------+
| Région    | Bandes Basses (<1 GHz)  | Bandes Moyennes (1.5 - 2.6 GHz)   | Bandes 5G C-Band & mmWave         |
|           | (Portée max, pénétration| (Capacité urbaine standard)       | (Débit gigabit, courte portée)    |
+-----------+-------------------------+-----------------------------------+-----------------------------------+
| Europe    | B20 (800 MHz)           | B1 (2100 MHz)                     | n78 (3.5 GHz - C-Band reine UE)   |
| (CEPT)    | B28 / n28 (700 MHz)     | B3 (1800 MHz)                     | n258 (26 GHz mmWave)              |
|           | B8 (900 MHz - GSM/3G)   | B7 (2600 MHz)                     |                                   |
+-----------+-------------------------+-----------------------------------+-----------------------------------+
| USA / NA  | B12 / B17 (700 MHz AT&T)| B2 / B25 (1900 MHz PCS)           | n77 / n41 (2.5 - 3.7 GHz C-Band)  |
| (FCC)     | B71 / n71 (600 MHz T-Mo)| B4 / B66 (1700/2100 AWS)          | n260 (39 GHz mmWave)              |
|           | B13 (700 MHz Verizon)   | B5 (850 MHz CLR)                  | n261 (28 GHz mmWave)              |
+-----------+-------------------------+-----------------------------------+-----------------------------------+
| Asie /    | B8 (900 MHz)            | B1 (2100 MHz), B3 (1800 MHz)      | n78 / n79 (4.8 GHz)               |
| Chine     | B5 (850 MHz)            | B38 / B39 / B40 / B41 (TDD)       | n257 (28 GHz)                     |
+-----------+-------------------------+-----------------------------------+-----------------------------------+
```

> **Conséquence pour le projet GAFAM / Matériel :**
> Si un dongle 4G/5G USB (comme un Quectel EC25 ou RM500Q) est branché sur un serveur VPS pour faire office de passerelle matérielle, il faut impérativement choisir la déclinaison régionale adéquate (ex: `EC25-E` pour l'Europe, `EC25-AF` pour les USA), sous peine que le modem ne puisse physiquement pas accrocher les fréquences locales de l'opérateur.

---

## 🔐 3. Le Processus d'Attachement Réseau (Attach & Authentication Flow)

### Q : Que se passe-t-il exactement, octet par octet, quand un smartphone s'allume et cherche le réseau ?

Voici le flux complet d'enregistrement d'un terminal sur un réseau 4G LTE (**EPS Attach Procedure**) défini par la norme **3GPP TS 23.401** :

```
Terminal (UE)            eNodeB (Antenne)             MME (Cœur)               HSS / AuC
    |                           |                         |                        |
    |---- 1. RRC Setup -------->|                         |                        |
    |---- 2. Attach Request --->|                         |                        |
    |     (IMSI / GUTI)         |--- 3. S1-AP Initial --->|                        |
    |                           |       Attach Request    |                        |
    |                           |                         |--- 4. Auth-Info-Req -->|
    |                           |                         |    (IMSI, PLMN ID)     |
    |                           |                         |<-- 5. Auth-Info-Ans ---|
    |                           |                         |    (Vector: RAND,      |
    |                           |                         |     AUTN, XRES, K_ASME)|
    |                           |<-- 6. NAS Auth Req -----|                        |
    |                           |    (RAND, AUTN, eKSI)   |                        |
    |<--- 7. NAS Auth Req ------|                         |                        |
    |     (Passe à la SIM)      |                         |                        |
    | [SIM: Vérifie AUTN]       |                         |                        |
    | [SIM: Calcule RES, K_ASME]|                         |                        |
    |---- 8. NAS Auth Resp ---->|                         |                        |
    |     (RES)                 |--- 9. S1-AP Response -->|                        |
    |                           |                         | [MME: Compare RES==XRES]
    |                           |                         | [Authentification OK!] |
    |                           |<-- 10. Security Mode ---|                        |
    |                           |    Command (NAS-Enc)    |                        |
    |<--- 11. Security Mode ----|                         |                        |
    |    Command (Chiffrement activé)                     |                        |
    |---- 12. Security Mode Complete -------------------->|                        |
    |                                                     |--- 13. Create Session -> SGW/PGW
    |<--- 14. Attach Accept (IP assignée, Bearer OK) -----|                        |
```

### Détail des étapes cryptographiques cruciales :
1. **Identité initiale** : Le mobile envoie son **IMSI** ou un identifiant temporaire chiffré (**GUTI** en 4G, **SUCI** en 5G).
2. **Génération du vecteur d'authentification** :
   - Le HSS/AuC possède la clef secrète $K_i$ de la SIM.
   - L'AuC génère un nombre aléatoire $RAND$ (128 bits) et un jeton d'authentification $AUTN$ (128 bits).
   - L'AuC calcule la réponse attendue $XRES$, et dérive la clef maîtresse de session $K_{ASME}$ via l'algorithme **Milenage**.
3. **Challenge de la carte SIM** :
   - Le mobile transmet $RAND$ et $AUTN$ à la carte SIM via la commande APDU `AUTHENTICATE`.
   - La SIM calcule en interne la validité de $AUTN$ (grâce à son $K_i$ et son compteur interne $SQN$). Cela garantit au mobile qu'il dialogue avec le **vrai réseau** et non une fausse antenne.
   - Si valide, la SIM calcule $RES$ et dérive $K_{ASME}$, puis renvoie $RES$ au baseband.
4. **Vérification** : Le MME vérifie si $RES == XRES$. Si oui, l'abonné est authentifié. Aucun mot de passe n'a jamais transité sur la radio !

---

### Q : Qu'est-ce que le SUCI en 5G et comment met-il fin aux IMSI-Catchers ?

En 2G, 3G et 4G, lors du tout premier attachement (ou si le réseau le demande), le mobile envoie son **IMSI en clair** sur les ondes radio dans le message `Identity Response`. C'est cette faille fondamentale qui a permis l'existence des **IMSI-Catchers** (fausses antennes qui forcent les téléphones proches à cracher leur IMSI pour les pister).

En **5G Standalone (3GPP TS 33.501)** :
- L'IMSI est renommé **SUPI** (*Subscription Permanent Identifier*).
- Le SUPI n'est **JAMAIS** transmis en clair sur la radio.
- La carte SIM (USIM 5G) embarque la **clef publique de l'opérateur** ($PK_{Home}$).
- Avant d'émettre, la SIM génère une paire de clefs éphémères et chiffre le SUPI via **ECIES** (*Elliptic Curve Integrated Encryption Scheme*, typiquement Curve25519 ou Secp256r1).
- Le résultat chiffré est le **SUCI** (*Subscription Concealed Identifier*).
- Seul l'UDM de l'opérateur (qui possède la clef privée $SK_{Home}$) peut déchiffrer le SUCI pour retrouver le SUPI.
- Un pirate captant les ondes ne voit qu'un hash cryptographique aléatoire qui change à chaque connexion.

---

## 🔒 4. Les Algorithmes de Chiffrement Radio (Air Interface Security)

### Q : Quels algorithmes chiffrent la communication entre le smartphone et l'antenne ?

La sécurité radio se divise en deux couches :
1. **AS (Access Stratum)** : Chiffre et intègre les données entre l'UE et l'antenne (eNodeB/gNodeB).
2. **NAS (Non-Access Stratum)** : Chiffre la signalisation de bout en bout entre l'UE et le cœur de réseau (MME/AMF), invisible pour l'antenne.

```
+------------+--------------------+----------------------+-------------------------------------------+
| Génération | Algorithme Nom     | Primitives Crypto    | Statut de Sécurité                        |
+------------+--------------------+----------------------+-------------------------------------------+
| 2G GSM     | A5/1               | LFSR (3 registres)   | ❌ Cassé (Rainbow tables kraken en <1 sec)|
| 2G GSM     | A5/2               | LFSR affaibli        | ❌ Trivialement cassé en temps réel (ban) |
| 2G GSM     | A5/3               | KASUMI (Bloc 64-bit) | ⚠️ Attaques théoriques existantes         |
+------------+--------------------+----------------------+-------------------------------------------+
| 3G UMTS    | UEA1 / UIA1        | KASUMI               | ⚠️ Vulnérabilités partielles              |
| 3G UMTS    | UEA2 / UIA2        | SNOW 3G              | ✅ Robuste (128-bit)                      |
+------------+--------------------+----------------------+-------------------------------------------+
| 4G LTE     | EEA1 / EIA1        | SNOW 3G              | ✅ Standard robuste                       |
| 4G LTE     | EEA2 / EIA2        | AES-128 (CTR / CMAC) | ✅ Standard industriel invincible         |
| 4G LTE     | EEA3 / EIA3        | ZUC (Chiffre flux Ch)| ✅ Très rapide et sécurisé                |
+------------+--------------------+----------------------+-------------------------------------------+
| 5G NR      | NEA1 / NIA1        | SNOW 3G              | ✅ 128-bit et 256-bit (Rel 16+)           |
| 5G NR      | NEA2 / NIA2        | AES-128 / AES-256    | ✅ Chiffrement militaire                  |
| 5G NR      | NEA3 / NIA3        | ZUC-128 / ZUC-256    | ✅ Haute performance radio                |
+------------+--------------------+----------------------+-------------------------------------------+
```

---

## 🔮 5. Et la 6G ? (Horizon 2030)

### Q : Quelles seront les ruptures techniques de la 6G ?

La 6G (standardisation 3GPP Releases 21-23 vers 2028-2030) apporte trois révolutions majeures :
1. **Fréquences Terahertz (Sub-THz : 100 GHz à 1 THz)** : Débits cibles de 100 Gbps à 1 Tbps, latence < 100 microsecondes.
2. **JCAS (Joint Communication and Sensing)** : L'onde radio ne transporte plus seulement des données numériques ; le faisceau radio sert simultanément de **radar haute résolution**. Le réseau 6G peut cartographier les objets, les mouvements et les personnes dans une pièce sans caméra.
3. **Native AI Air Interface** : La couche physique (modulation QAM, égalisation de canal, formation de faisceau MIMO) ne sera plus codée en dur avec des formules mathématiques de Fourier, mais gérée par des modèles de réseaux de neurones auto-entraînés s'adaptant à l'environnement radio en temps réel.
