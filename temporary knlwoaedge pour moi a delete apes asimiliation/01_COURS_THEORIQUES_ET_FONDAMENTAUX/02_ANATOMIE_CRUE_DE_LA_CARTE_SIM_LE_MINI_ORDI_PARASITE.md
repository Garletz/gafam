# 02 — Anatomie Crue de la Carte SIM : Le Mini-Ordinateur Parasite

> **Guide d'ingénierie matérielle, cryptographique et logicielle du Secure Element.**
> Format : Questions & Réponses techniques, architectures microcontrôleur, dumps APDU réels et reverse-engineering du système de fichiers ISO 7816-4.

---

## 💻 1. Le Hardware : Un Ordinateur Complet dans 5 mm²

### Q : Pourquoi dit-on qu'une carte SIM est un « ordinateur parasite » autonome ?

Une carte SIM (ou **UICC** - *Universal Integrated Circuit Card*) n'est absolument pas une simple clé mémoire flash ou un fichier passif. C'est un **ordinateur sécurisé complet (Secure Element)** qui possède son propre CPU, sa propre mémoire volatile, son stockage persistant, son horloge et son système d'exploitation multitâche temps réel.

```
┌────────────────────────────────────────────────────────────────────────┐
│                        PUCE UICC / SECURE ELEMENT                      │
│                                                                        │
│   ┌────────────────────────┐         ┌─────────────────────────────┐   │
│   │   CPU Sécurisé         │         │     Coprocesseurs Crypto    │   │
│   │   ARM SecurCore SC000  │<=======>│  - TRNG (True Random)       │   │
│   │   ou SC300 (32-bit)    │         │  - Moteur Matériel AES/3DES │   │
│   │   (Fréq: 5 - 30 MHz)   │         │  - Moteur Asymétrique RSA/EC│   │
│   └───────────┬────────────┘         └──────────────┬──────────────┘   │
│               │                                     │                  │
│   ════════════╪═══════════════ BUS SYSTÈME ═════════╪══════════════    │
│               │                                     │                  │
│   ┌───────────┴────────────┐         ┌──────────────┴──────────────┐   │
│   │       RAM (Volatile)   │         │    ROM / Flash / EEPROM     │   │
│   │   8 KB à 32 KB         │         │  - ROM : Bootloader OS      │   │
│   │   (Exécution volatile) │         │  - Flash (128 KB - 1 MB) :  │   │
│   │                        │         │    Java Cardlets, Clés Ki   │   │
│   │                        │         │    Système de fichiers      │   │
│   └────────────────────────┘         └─────────────────────────────┘   │
│                                                     ▲                  │
│   ┌─────────────────────────────────────────────────┴──────────────┐   │
│   │    Capteurs Anti-Tamper Physiques (Blindage Actif)             │   │
│   │  - Détecteur de glitch de tension (Anti-VCC glitching)         │   │
│   │  - Détecteur d'attaque laser / lumière sur silicium            │   │
│   │  - Générateur de bruit DPA/EMA (Anti-analyse de consommation)  │   │
│   └────────────────────────────────────────────────────────────────┘   │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │ Interface ISO 7816-3 (CLK, I/O, RST, VCC)
                                    ▼
                         Modem Baseband du Téléphone
```

### Caractéristiques matérielles types :
- **CPU** : ARM SecurCore (ex: SC000, SC300) ou cœurs propriétaires 8051 durcis / RISC-V sécurisés.
- **RAM** : 8 Ko à 64 Ko (utilisée uniquement pour les piles d'applets et le calcul crypto temporaire).
- **ROM** : 128 Ko à 512 Ko (contient le firmware d'usine et la JVM JavaCard immuable).
- **Flash / EEPROM** : 256 Ko à 2 Mo (contient les applets Java, les fichiers de configuration opérateur, le carnet de contacts et les clés secrètes $K_i$).
- **Anti-Tampering** : La puce est conçue pour **détruire ses clés ou se briquer** en cas d'attaque par microscope électronique, décapage acide, attaque par faute laser (laser fault injection) ou analyse différentielle de consommation électrique (DPA - *Differential Power Analysis*).

---

### Q : Quel est le brochage physique (Pinout) et le protocole électrique d'une carte SIM ?

La norme **ISO/IEC 7816-3** définit 8 contacts physiques métalliques (6 sont utilisés en pratique) :

```
       ┌───────────┐
  C1 --│ VCC   GND │-- C5
  C2 --│ RST   VPP │-- C6 (Non connecté / Obsolète)
  C3 --│ CLK   I/O │-- C7
  C4 --│ D+    D-  │-- C8 (USB UICC haute vitesse - optionnel)
       └───────────┘
```

1. **C1 (VCC)** : Alimentation électrique fournie par le modem. Trois classes de tension :
   - Classe A : 5V (vieux téléphones 2G)
   - Classe B : 3V (standard 3G/4G)
   - Classe C : 1.8V (standard moderne smartphone 4G/5G/eSIM)
2. **C2 (RST)** : Ligne de Reset contrôlée par le modem.
3. **C3 (CLK)** : Signal d'horloge fourni par le modem (généralement entre 3.57 MHz et 5 MHz).
4. **C5 (GND)** : Masse électrique.
5. **C7 (I/O)** : Ligne de communication **série asynchrone half-duplex** sur un seul fil. Toutes les commandes et réponses passent par ce fil unique !

---

## ⚙️ 2. Le Système d'Exploitation : JavaCard OS & GlobalPlatform

### Q : Quel système d'exploitation tourne à l'intérieur de la SIM ?

Presque toutes les cartes SIM modernes exécutent **JavaCard OS** (conforme à la spécification GlobalPlatform) :
- **Une machine virtuelle Java allégée (JVM)** : Elle exécute un bytecode compacté (CAP files).
- **Pas de Garbage Collector traditionnel** : Pour éviter les pannes de mémoire non déterministes, la mémoire est allouée de façon statique lors de l'installation de la Cardlet.
- **Multitâche et Isolation (Firewalling)** : Chaque application (appelée **Cardlet**) est isolée dans un bac à sable strict. L'applet USIM (qui gère la 4G/5G) ne peut pas être lue par une applet bancaire ou une applet d'un tiers installée sur la même SIM.

```
┌────────────────────────────────────────────────────────┐
│                   APPLETS JAVACARD                     │
│  ┌────────────────┐  ┌────────────────┐  ┌───────────┐ │
│  │   USIM Applet  │  │   ISIM Applet  │  │ STK / OTA │ │
│  │   (3GPP 31.102)│  │   (VoLTE/IMS)  │  │ Applet    │ │
│  └───────┬────────┘  └───────┬────────┘  └─────┬─────┘ │
│          │                   │                 │       │
│  ════════╪═══════════════════╪═════════════════╪══════ │
│          ▼                   ▼                 ▼       │
│                JAVACARD RUNTIME ENVIRONMENT            │
│               (JavaCard API + Cardlet Firewall)        │
├────────────────────────────────────────────────────────┤
│             GLOBALPLATFORM FRAMEWORK                   │
│        (Security Domains, Secure Channel Protocol SCP) │
├────────────────────────────────────────────────────────┤
│                 NATIVE CARD OS / KERNEL                │
│    (Drivers I/O ISO 7816, Crypto Hardware HAL, Flash)  │
└────────────────────────────────────────────────────────┘
```

---

## 🗂️ 3. Le Système de Fichiers ISO/IEC 7816-4

### Q : Comment sont organisés les fichiers dans une carte SIM ?

La carte SIM utilise une hiérarchie arborescente standardisée par l'ISO 7816-4 et le 3GPP :
- **MF (Master File, ID `0x3F00`)** : La racine absolue du système de fichiers (équivalent de `/`).
- **DF (Dedicated File)** : Un dossier ou sous-dossier (ex: `0x7F20` = DF_GSM, `0x7F25` = DF_TELECOM).
- **ADF (Application Dedicated File)** : Un dossier d'application USIM/ISIM identifié par un AID (Application Identifier).
- **EF (Elementary File)** : Un fichier contenant de la donnée brute.

```
                                [ MF : 3F00 ] (Racine)
                                      │
       ┌──────────────────────────────┼──────────────────────────────┐
       │                              │                              │
 [ EF_ICCID : 2FE2 ]           [ DF_TELECOM : 7F25 ]         [ ADF_USIM : 7FF0 ]
 (Numéro de série SIM)                │                              │
                               ┌──────┴──────┐                ┌──────┴──────────────────┐
                               │             │                │                         │
                         [ EF_SMS : 6F3C ] [ EF_ADN : 6F3A ] [ EF_IMSI : 6F07 ]  [ EF_LOCI : 6F7E ]
                         (SMS stockés)     (Contacts SIM)    (Identité IMSI)     (Localisation TMSI/LAI)
```

### Table des fichiers vitaux d'une SIM 4G/5G :

| ID Fichier | Nom Standard | Rôle & Contenu |
| :--- | :--- | :--- |
| `0x2FE2` | **EF_ICCID** | Identifiant matériel unique de la carte (19-20 chiffres). Lisible sans code PIN. |
| `0x6F07` | **EF_IMSI** | L'International Mobile Subscriber Identity (MCC + MNC + MSIN). |
| `0x6F7E` | **EF_LOCI** | Information de localisation : TMSI, Location Area Information (LAI), état de mise à jour radio. |
| `0x6F7B` | **EF_FPLMN** | Liste des réseaux interdits (*Forbidden PLMNs*) où la SIM a été rejetée. |
| `0x6FAD` | **EF_AD** | Administrative Data : Mode normal, mode test (Test SIM), mode constructeur. |
| `0x6F46` | **EF_SPN** | Service Provider Name (ex: "Orange", "Verizon", "Free") affiché sur l'écran. |

---

## 📡 4. Le Protocole APDU (Application Protocol Data Unit)

### Q : À quoi ressemble une trame APDU échangée entre le téléphone et la carte SIM ?

La communication est régie par le modèle **Maître-Esclave** : le terminal (modem) envoie une **Command APDU**, la SIM répond immédiatement par une **Response APDU**.

#### Structure binaire d'une Command APDU :
```
+-------+-------+----+----+------+------------------------+----+
|  CLA  |  INS  | P1 | P2 |  Lc  |       DATA FIELD       | Le |
| (1 B) | (1 B) |(1B)|(1B)| (1B) |     (0 à 255 octets)   |(1B)|
+-------+-------+----+----+------+------------------------+----+
```
- **CLA** : Classe d'instruction (`0x00` = ISO 7816, `0x80` ou `0xA0` = GSM/3GPP).
- **INS** : Code de l'instruction (`0xA4` = SELECT FILE, `0xB0` = READ BINARY, `0x88` = AUTHENTICATE, `0x12` = FETCH).
- **P1, P2** : Paramètres de l'instruction (ex: offset de lecture, ID du fichier).
- **Lc** : Longueur des données envoyées dans la commande.
- **Data** : Charge utile binaire.
- **Le** : Longueur maximale attendue en réponse.

#### Structure d'une Response APDU :
```
+------------------------------------+-------+-------+
|             DATA FIELD             |  SW1  |  SW2  |
|          (0 à 256 octets)          | (1 B) | (1 B) |
+------------------------------------+-------+-------+
```
- **SW1 SW2 (Status Words)** : Le code de retour HTTP-like de la SIM :
  - `90 00` : Succès total (OK).
  - `61 xx` : Succès, `xx` octets de données sont prêts à être lus via la commande `GET RESPONSE`.
  - `98 04` : Échec de sécurité (code PIN faux).
  - `6A 82` : Fichier non trouvé (*File Not Found*).

#### Exemple Réel : Lecture de l'ICCID (`0x2FE2`)
```
1. Sélection du fichier 2FE2 :
   Tx -> 00 A4 00 04 02 2F E2
   Rx <- 90 00 (Fichier sélectionné avec succès)

2. Lecture des 10 octets du fichier :
   Tx -> 00 B0 00 00 0A
   Rx <- 98 83 10 21 43 65 87 09 21 F3 90 00
   Décodage BCD Swappé : 89 38 01 12 34 56 78 90 12 3F -> ICCID = "8938011234567890123"
```

---

## 🔐 5. La Cryptographie Milenage & l'Authentification 3GPP

### Q : Comment fonctionne l'algorithme secret Milenage qui protège l'accès au réseau ?

Chaque carte SIM contient une clé maîtresse secrète **$K_i$** (128 bits) et une constante opérateur **$OP$** (ou sa dérivée **$OP_c$**).
Ces clés sont **gravées dans la mémoire Flash sécurisée et ne peuvent JAMAIS sortir de la puce**.

Lors de l'authentification réseau (**3GPP TS 35.206**) :
1. Le cœur de réseau envoie un challenge : $RAND$ (128 bits aléatoires) et $AUTN$ (Jeton d'authentification réseau).
2. Le modem envoie l'APDU `AUTHENTICATE` à la SIM avec $(RAND, AUTN)$.
3. L'algorithme **Milenage** exécute en interne 5 fonctions basées sur le chiffrement AES-128 :

```
                      ┌────────────────────────────────────────┐
                      │   Clé Secrète Ki + OPc (DANS LA SIM)   │
                      └───────────────────┬────────────────────┘
                                          │
                  ┌───────────────────────┴───────────────────────┐
                  ▼                                               ▼
         [ Fonction f1 / f1* ]                           [ Fonctions f2, f3, f4, f5 ]
    (Calcule le MAC d'intégrité)                         (Calcule les clés de session)
                  │                                               │
      Vérifie que AUTN est valide                     Génère :
   (Empêche les fausses antennes)                     - RES (Réponse au challenge réseau)
                  │                                   - CK  (Cipher Key - Chiffrement 128b)
                  ▼                                   - IK  (Integrity Key - Intégrité 128b)
    Si Valide -> Calcule RES et Clés !                - AK  (Anonymity Key)
```

> **Le résultat ?**
> Le réseau compare la réponse $RES$ calculée par la SIM avec la réponse $XRES$ calculée par son propre serveur. Si les deux correspondent, l'abonné est connecté. La clé $K_i$ n'a jamais voyagé sur le réseau ni été transmise au processeur du smartphone !

---

## 👾 6. Le SIM Toolkit (STK / USAT / BIP) : Le Parasite en Action

### Q : Comment la carte SIM peut-elle prendre le contrôle du téléphone à l'insu de l'utilisateur ?

Le standard **3GPP TS 31.111 (USIM Application Toolkit - USAT)** confère à la carte SIM le statut d'entité **proactive**. La SIM peut forcer le smartphone à exécuter des actions via des **Commandes Proactives** :

```
                 Modem Baseband                         Carte SIM (UICC)
                       |                                       |
                       |<==== Status Word "91 XX" =============|
                       |      (SIM: "J'ai un ordre pour toi!") |
                       |                                       |
                       |==== APDU FETCH ======================>|
                       |<==== Commande Proactive ==============|
                       |      (Ex: DISPLAY TEXT, SEND SMS,     |
                       |       OPEN CHANNEL BIP...)            |
                       |                                       |
                       | [Le modem exécute l'ordre]            |
                       |                                       |
                       |==== APDU TERMINAL RESPONSE ==========>|
                       |    (Rapport de succès ou d'échec)     |
```

### Exemples de commandes proactives redoutables :
1. **`SEND SHORT MESSAGE`** : La SIM ordonne au modem d'envoyer un SMS en tâche de fond vers un numéro surtaxé ou un serveur de monitoring opérateur, **sans que l'OS Android ne soit notifié et sans laisser de trace dans la base SMS d'Android**.
2. **`SET UP CALL`** : La SIM force le smartphone à composer un numéro de téléphone.
3. **`LAUNCH BROWSER`** : La SIM force l'ouverture d'une URL dans le navigateur du téléphone.
4. **`PROVIDE LOCAL INFORMATION`** : La SIM exige la géolocalisation GPS exacte, l'IMEI du terminal, la Cell ID et le niveau de batterie.
5. **`OPEN CHANNEL` / `SEND DATA` (BIP - Bearer Independent Protocol)** :
   - C'est la fonctionnalité la plus stupéfiante : la SIM ordonne au modem de lui ouvrir une **connexion socket TCP/IP directe** via la connexion 4G/5G du forfait.
   - La SIM télécharge des exécutables bytecode et met à jour ses fichiers internes de manière autonome (**Over-The-Air OTA**), en contournant totalement les pare-feux, les VPNs installés sur Android et l'OS hôte.
