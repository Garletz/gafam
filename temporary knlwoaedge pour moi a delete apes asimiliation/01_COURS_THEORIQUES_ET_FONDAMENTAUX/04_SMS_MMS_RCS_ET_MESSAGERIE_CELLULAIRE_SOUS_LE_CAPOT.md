# 04 — SMS, MMS, RCS & Messagerie Sous le Capot

> **Guide d'ingénierie des couches de transport de messages et d'encodage binaire.**
> Format : Questions & Réponses techniques, dissection binaire de trames PDU, flux WAP Push MMS et architecture de signalisation SIP/MSRP pour RCS.

---

## 📨 1. Les Canaux de Transport : Du Canal Radio Brut à l'IP

### Q : Comment un SMS voyage-t-il physiquement à travers les réseaux 2G, 4G, 5G et Wi-Fi ?

Un SMS n'est pas un message internet classique : il a été conçu à l'origine pour exploiter les **canaux de signalisation radio résiduels** qui servaient à maintenir le lien entre le téléphone et l'antenne.

```
┌──────────────────────────────────────────────────────────────────────────────────────────┐
│                         LES 4 MODES DE TRANSPORT D'UN SMS                                │
│                                                                                          │
│  1. SMS over NAS (2G / 3G / 4G / 5G natif) :                                             │
│     [ UE (Mobile) ] ===== Trames NAS (Signalisation) =====> [ MME / AMF ] ===> [ SMSC ]  │
│     (Aucune connexion Internet ou APN requise ! Fonctionne même avec 0 data)            │
│                                                                                          │
│  2. SMS over SGs (4G CSFB Hybride) :                                                     │
│     [ UE (4G) ] ===== NAS 4G =====> [ MME ] ===== Interface SGs =====> [ MSC 2G/3G ]     │
│                                                                                          │
│  3. SMSoIP / SMS over IMS (VoLTE / VoWiFi) :                                             │
│     [ UE ] ===== Session SIP (APN "ims") =====> [ P-CSCF ] ===> [ IP-SM-GW ] ===> [ SMSC]│
│     (Encapsulé dans une requête SIP "MESSAGE" chiffrée en IPsec)                         │
│                                                                                          │
│  4. 5G SMSF (5G Standalone Pur) :                                                        │
│     [ UE ] ===== Trames NAS 5G =====> [ AMF ] ===== Service HTTP/2 =====> [ SMSF ]       │
└──────────────────────────────────────────────────────────────────────────────────────────┘
```

1. **SMS over NAS (Non-Access Stratum)** : Le SMS est directement injecté dans les paquets de contrôle radio (`CP-DATA` / `RP-DATA`). Il ne consomme aucun octet de forfait data et transite instantanément même si les données mobiles sont désactivées.
2. **SMSoIP (SMS over IP)** : Sur les réseaux récents (VoLTE / VoWiFi), le SMS est transformé en paquet SIP :
   - Le smartphone envoie une requête `SIP MESSAGE` sur l'APN IMS (`ims`).
   - Le message est encapsulé dans du XML/MIME binaire et transmis sur le tunnel IPsec chiffré du sous-système multimédia.

---

## 🧩 2. Dissection Binaire d'une Trame SMS PDU (Protocol Data Unit)

### Q : Comment est encodé un SMS au niveau hexadécimal brut ?

Les modems cellulaires communiquent les SMS via le format binaire **PDU (Protocol Data Unit)** normalisé par la spécification **3GPP TS 23.040**.

#### Exemple d'une trame brute `SMS-SUBMIT` (Envoi) :
`0001000A81503810423000000BD4F29C0E6A97E7F3F0B90C`

```
┌──────┬──────┬──────┬────────────────┬──────┬──────┬──────┬────────────────────────────┐
│ SCA  │ MTI  │  MR  │   DA (Dest)    │ PID  │ DCS  │ UDL  │         UD (Texte)         │
│ (1B) │ (1B) │ (1B) │    (6 Bytes)   │ (1B) │ (1B) │ (1B) │          (12 Bytes)        │
│  00  │  01  │  00  │ 0A 81 5038104230 │  00  │  00  │  0B  │ D4 F2 9C 0E 6A 97 E7 ...   │
└──────┴──────┴──────┴────────────────┴──────┴──────┴──────┴────────────────────────────┘
```

### Décodage champ par champ :
- **`00` (SCA - Service Center Address Length)** : `00` signifie que le modem doit utiliser le centre SMSC configuré par défaut dans la carte SIM.
- **`01` (PDU Type Flags)** : 
  - Bit 0-1 (`01`) = `SMS-SUBMIT` (Message sortant).
  - Bit 6 (`0`) = Pas de header concaténé (UDHI = 0).
- **`00` (MR - Message Reference)** : Identifiant séquentiel du message géré par le modem.
- **`0A 81 50 38 10 42 30` (DA - Destination Address)** :
  - `0A` = Longueur du numéro (10 chiffres).
  - `81` = Format national/inconnu (`91` pour international avec `+`).
  - `50 38 10 42 30` = Numéro encodé en octets inversés (Semi-Octet BCD) $\rightarrow$ `05 83 01 24 03` $\rightarrow$ **`0583012403`**.
- **`00` (PID - Protocol Identifier)** : `00` = SMS standard point-à-point.
- **`00` (DCS - Data Coding Scheme)** : `00` = Encodage GSM 7-bit par défaut.
- **`0B` (UDL - User Data Length)** : `0B` = 11 caractères à décoder.
- **`D4 F2 9C 0E 6A 97 E7 F3 F0 B9 0C` (UD - User Data)** : Les 11 caractères compressés sur 7 bits $\rightarrow$ **"Hello World"**.

---

### Q : Comment fonctionne le packing 7-bit du GSM et pourquoi les émojis réduisent la taille du SMS à 70 caractères ?

1. **GSM 7-bit Default Alphabet** :
   - Chaque caractère ASCII de base est encodé sur **7 bits** au lieu de 8 bits.
   - Les bits sont compactés les uns à la suite des autres en décalant les bits vers la gauche. 8 caractères de 7 bits tiennent ainsi dans 7 octets de 8 bits.
   - Capacité maximale d'un SMS : $140 \text{ octets} \times 8 \text{ bits} / 7 \text{ bits} = \mathbf{160 \text{ caractères}}$.
2. **UCS2 (UTF-16 Big Endian)** :
   - Dès qu'un seul caractère non présent dans la table GSM 7-bit est inséré (un émoji 🚀, une lettre arabe, cyrillique, ou un accent complexe comme `ê` / `ç` selon les tables), le champ **DCS** passe à `0x08` (**UCS2**).
   - Chaque caractère consomme alors obligatoirement **16 bits (2 octets)**.
   - Capacité maximale : $140 \text{ octets} / 2 \text{ octets} = \mathbf{70 \text{ caractères}}$.

---

### Q : Comment fonctionnent les SMS longs (concaténés) et les Flash SMS ?

#### 1. SMS Concaténés (Multi-part SMS)
Quand un message dépasse 160 caractères (ou 70 en UCS2), le bit **UDHI** (*User Data Header Indicator*) est activé (`1`). Les premiers octets de la charge utile contiennent un en-tête **UDH (User Data Header)** :
```
05 00 03 A1 03 02
 │  │  │  │  │  └─ Numéro de la partie actuelle (Part 2)
 │  │  │  │  └──── Nombre total de parties (Total: 3 parties)
 │  │  │  └─────── ID unique du groupe de SMS (0xA1)
 │  │  └────────── Longueur des données du header (3 octets)
 │  └───────────── Information Element Identifier (00 = Concaténation 8-bit)
 └──────────────── Longueur totale de l'en-tête UDH (5 octets)
```
*Le smartphone bufferise les différentes parties reçues et les fusionne en un seul message une fois toutes les tranches collectées.*

#### 2. Flash SMS (SMS Classe 0)
- Encodé avec un DCS spécifique (`0x10` ou `0xF0`).
- Le terminal a l'obligation selon la norme 3GPP d'afficher **immédiatement le texte dans une popup modale bloquante au premier plan**, sans l'enregistrer dans la base de données SMS locale (`content://sms`).

#### 3. SMS OTA (SMS Classe 2 / SIM Data Download)
- Le SMS cible directement la carte SIM via le port de protocole `0x7F` ou `0x02`.
- Le SMS contient une charge utile binaire chiffrée avec les clés OTA de l'opérateur (norme 3GPP TS 31.115).
- Le smartphone transmet le SMS directement à la SIM via l'APDU `ENVELOPE (SMS-PP DOWNLOAD)`. La carte SIM met à jour ses fichiers ou exécute une applet sans allumer l'écran du téléphone.

---

## 🖼️ 3. Le Fonctionnement Intime du MMS (Multimedia Messaging Service)

### Q : Comment un MMS est-il acheminé et pourquoi nécessite-t-il un APN data dédié ?

Le MMS n'a jamais été un protocole radio direct : c'est un **pont hybride entre un SMS binaire déclencheur et un téléchargement HTTP**.

```
Expéditeur                Opérateur (MMSC)                    Destinataire
    |                            |                                 |
    |==== 1. HTTP POST M-Send.req ======>                         |
    |     (Contenu image/audio)  |                                 |
    |                            |==== 2. WAP Push SMS (PDU) =====>|
    |                            |     (M-Notification.ind)        |
    |                            |     URL: http://mmsc/msg123     |
    |                            |                                 |
    |                            |     [Destinataire réveille son  |
    |                            |      modem sur l'APN "mms"]     |
    |                            |                                 |
    |                            |<=== 3. HTTP GET /msg123 ========|
    |                            |==== 4. HTTP 200 OK =============|
    |                            |     (M-Retrieve.conf multipart) |
```

1. **Publication** : L'expéditeur compile un document binaire multipart (WAP MMS PDU encapsulant un fichier de synchronisation multimédia **SMIL** et des fichiers JPEG/AMR) et l'envoie par requête HTTP POST sur le serveur **MMSC** (*Multimedia Messaging Service Center*) via l'APN MMS.
2. **Notification Push** : Le MMSC envoie au destinataire un **SMS binaire spécial (WAP Push)** sur le port UDP/WSP standard `2948`. Ce SMS contient une notification `m-notification-ind` avec l'URL exacte du fichier sur le serveur MMSC.
3. **Récupération** : Dès réception du WAP Push, l'OS Android lance en arrière-plan un appel réseau dédié sur l'APN opérateur `mms`, télécharge le fichier binaire par HTTP GET, reconstruit les pièces jointes et les injecte dans la base SQLite locale `content://mms`.

---

## 🚀 4. RCS (Rich Communication Services) & Universal Profile

### Q : Comment fonctionne le protocole RCS et pourquoi est-il verrouillé par Google ?

Le protocole **RCS (Universal Profile 2.4+)** vise à remplacer le SMS/MMS par une expérience moderne (indicateurs de frappe, accusés de lecture, transferts de fichiers haute résolution, chiffrement E2EE).

```
┌────────────────────────────────────────────────────────────────────────┐
│                        STACK PROTOCOLAIRE RCS                          │
│                                                                        │
│  ┌──────────────────────────────────────────────────────────────────┐  │
│  │                    MESSAGERIE INSTANTANÉE (1-to-1 & Group)       │  │
│  │  - Encapsulation CPIM (Common Profile for Instant Messaging)     │  │
│  │  - Transport MSRP (Message Session Relay Protocol - RFC 4975)   │  │
│  └──────────────────────────────────┬───────────────────────────────┘  │
│                                     │                                  │
│  ┌──────────────────────────────────┴───────────────────────────────┐  │
│  │               SIGNALISATION & GESTION DE SESSION                 │  │
│  │  - SIP (Session Initiation Protocol - RFC 3261)                  │  │
│  │  - SDP (Session Description Protocol - RFC 4566)                 │  │
│  └──────────────────────────────────┬───────────────────────────────┘  │
│                                     │                                  │
│  ┌──────────────────────────────────┴───────────────────────────────┐  │
│  │                     AUTHENTIFICATION & PROVISIONING               │  │
│  │  - Authentification Digest-AKAv1-MD5 (via ISIM/USIM) ou Google   │  │
│  │  - Configuration automatique ACS (Auto-Configuration Server)     │  │
│  └──────────────────────────────────┬───────────────────────────────┘  │
│                                     │                                  │
│  ┌──────────────────────────────────┴───────────────────────────────┐  │
│  │               COUCHE TRANSPORT IP (TLS 1.3 / TCP)                │  │
│  │  - Serveurs Google Jibe Cloud ou IMS Opérateur                   │  │
│  └──────────────────────────────────────────────────────────────────┘  │
└────────────────────────────────────────────────────────────────────────┘
```

### Pourquoi RCS est complexe à intercepter dans GAFAM (Manifest 16) :
- Contrairement au SMS où Android expose un `BroadcastReceiver` public (`android.provider.Telephony.SMS_RECEIVED`), Google Messages traite le RCS **en vase clos** via l'application propriétaire **Carrier Services**.
- Le flux MSRP/SIP est chiffré en TLS entre Google Messages et les serveurs **Google Jibe**.
- Aucun ContentProvider public `content://rcs` n'existe dans AOSP.
- **La parade GAFAM** : Utiliser le `NotificationListenerService` pour écouter les notifications Android générées par Google Messages (style `Notification.MessagingStyle`) afin d'en extraire le texte et l'expéditeur en temps réel pour le relayer vers le VPC.
