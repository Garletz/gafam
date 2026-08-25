# 03 — eSIM, eUICC, iSIM & Provisioning GSMA

> **Guide d'ingénierie sur la dématérialisation de l'identité cellulaire.**
> Format : Questions & Réponses pointues, architectures cryptographiques GSMA RSP, parsing de paquets ASN.1 DER et outils d'administration d'eSIM en environnement Linux/Docker.

---

## 🏷️ 1. L'Architecture Matérielle et Logique de l'eUICC

### Q : Qu'est-ce qu'une eSIM (eUICC) par rapport à une SIM classique ?

Une **eSIM** n'est pas un concept purement logiciel : c'est un composant matériel physique appelé **eUICC** (*Embedded Universal Integrated Circuit Card*), souvent soudé directement sur la carte mère du smartphone (format WLCSP ou VQFN8), ou intégré dans une carte SIM amovible programmable.

La différence fondamentale réside dans sa capacité à **héberger simultanément plusieurs profils d'opérateurs complètement isolés**, et à pouvoir en télécharger, activer ou supprimer à chaud via un protocole cryptographique normalisé par la GSMA.

```
┌─────────────────────────────────────────────────────────────────────────┐
│                           PUCE eUICC PHYSIQUE                           │
│                                                                         │
│  ┌───────────────────────────────────────────────────────────────────┐  │
│  │     ECASD (eUICC Controlling Authority Security Domain)           │  │
│  │   - Certificat d'identité unique de l'eUICC (Cert.EUID)           │  │
│  │   - Clé privée racine de la puce (SK.EUICC.ECKA)                  │  │
│  │   - Certificats racines des Autorités de Certification GSMA (CI)  │  │
│  └───────────────────────────────────┬───────────────────────────────┘  │
│                                      │                                  │
│  ┌───────────────────────────────────┴───────────────────────────────┐  │
│  │             ISD-R (Issuer Security Domain - Root)                 │  │
│  │   - Moteur de cycle de vie des profils (Création, Switch, Delete) │  │
│  │   - Décodeur cryptographique des paquets ASN.1 BPP                │  │
│  └───────────────────────────────────┬───────────────────────────────┘  │
│                                      │                                  │
│         ┌────────────────────────────┴────────────────────────────┐     │
│         ▼                                                         ▼     │
│  ┌─────────────────────────────┐           ┌──────────────────────────┐ │
│  │    ISD-P (Profil 1: Orange) │           │  ISD-P (Profil 2: Free)  │ │
│  │  - Applet USIM (31.102)     │           │  - Applet USIM (31.102)  │ │
│  │  - Clé secrète Ki_1 + OPc_1 │           │  - Clé secrète Ki_2 + OPc│ │
│  │  - IMSI_1, EF_LOCI_1        │           │  - IMSI_2, EF_LOCI_2     │ │
│  │  [ ÉTAT : ACTIF / ÉVEILLÉ ] │           │  [ ÉTAT : DÉSACTIVÉ ]    │ │
│  └─────────────────────────────┘           └──────────────────────────┘ │
└─────────────────────────────────────────────────────────────────────────┘
```

### Les domaines de sécurité internes :
1. **ECASD** : Le coffre-fort ultime. Il est gravé en usine avec les certificats signés par la GSMA. C'est lui qui prouve aux serveurs des opérateurs que la puce est authentique et inviolable.
2. **ISD-R** : Le chef d'orchestre. Il reçoit les ordres d'installation, sélectionne le profil actif et garantit qu'un seul profil utilise les broches radio du modem à un instant $T$.
3. **ISD-P** : Des conteneurs étanches. Chaque profil opérateur téléchargé est enfermé dans son propre ISD-P avec son propre système de fichiers virtuel ISO 7816-4 et ses clés $K_i$.

---

## 🌐 2. Les Spécifications GSMA : SGP.22 (Consumer) vs SGP.02/32 (IoT/M2M)

### Q : Pourquoi existe-t-il plusieurs normes GSMA pour l'eSIM ?

La GSMA a développé des architectures différentes selon la présence ou non d'un humain devant l'écran :

```
+------------------+-----------------------------+-----------------------------------+-----------------------------------+
| Norme GSMA       | Cas d'usage principal       | Modèle de Provisioning            | Composant Décisionnel             |
+------------------+-----------------------------+-----------------------------------+-----------------------------------+
| **SGP.22**       | Smartphones, Tablettes, PC, | **PULL** : L'utilisateur scanne   | **LPA** (Local Profile Assistant) │
| (RSP Consumer)   | Montres connectées          | un QR Code ou clique "Télécharger"| sur le terminal ou l'eUICC        |
+------------------+-----------------------------+-----------------------------------+-----------------------------------+
| **SGP.02**       | Véhicules connectés, Smart  | **PUSH** : Le serveur opérateur   | **SM-SR** (Subscription Manager   |
| (RSP M2M Legacy) | Meters (Compteurs d'eau/gaz)| pousse le profil à distance       | Secure Routing) côté cloud        |
+------------------+-----------------------------+-----------------------------------+-----------------------------------+
| **SGP.32**       | Objets connectés modernes,  | **Hybride PUSH/PULL** adapté aux  | **eIM** (eUICC IoT Manager)       |
| (RSP IoT NextGen)| Passerelles industrielles   | microcontrôleurs basse conso      | + IPA léger sur le device         |
+------------------+-----------------------------+-----------------------------------+-----------------------------------+
```

---

## 📲 3. Le Protocole LPA (Local Profile Assistant) et le Téléchargement de Profil

### Q : Que contient un QR Code eSIM et comment se déroule l'activation ?

Un QR code d'eSIM n'est rien d'autre qu'une chaîne de caractères formatée selon la norme **GSMA SGP.22** :

```text
LPA:1$smdp.carrier.com$MATCHING-ID-TOKEN-HEX$1.2.840.113549.1.1.1
```
- `LPA:1` : Version du protocole GSMA RSP.
- `smdp.carrier.com` : FQDN du serveur **SM-DP+** (*Subscription Manager Data Preparation +*) de l'opérateur.
- `MATCHING-ID-TOKEN-HEX` : Token d'activation unique assigné à votre commande.

### Découpage interne du LPA :
Le **LPA** est composé de trois briques logicielles :
1. **LPU (LPA UI)** : L'interface graphique utilisateur (ex: menu Paramètres d'Android ou iOS).
2. **LPD (LPA Download)** : Le client HTTP/REST qui dialogue en TLS avec le serveur SM-DP+ sur Internet (interface **ES9+**).
3. **LPR (LPA Rendering)** : Le traducteur qui prend les paquets de données reçus d'Internet et les transforme en commandes APDU ISO 7816 pour les injecter dans l'eUICC (interface **ES10x**).

```
   Utilisateur (QR Code)
            │
            ▼
    ┌───────────────┐
    │    LPA UI     │
    └───────┬───────┘
            │
    ┌───────┴───────┐   Interface ES9+ (HTTP / TLS / JSON)   ┌──────────────────┐
    │    LPA LPD    │<======================================>│  Serveur SM-DP+  │
    └───────┬───────┘                                        │  (Opérateur)     │
            │                                                └──────────────────┘
    ┌───────┴───────┐   Interface ES10x (Commandes APDU)     ┌──────────────────┐
    │    LPA LPR    │<======================================>│   Puce eUICC     │
    └───────────────┘                                        │   (Matérielle)   │
                                                             └──────────────────┘
```

---

### Q : Quel est l'échange cryptographique de bout en bout lors de l'installation ?

Le serveur de l'opérateur (**SM-DP+**) et la puce **eUICC** établissent un canal chiffré de bout en bout **que le processeur du smartphone et le système d'exploitation ne peuvent pas déchiffrer** :

1. **Authentification mutuelle (InitiateAuthentication / AuthenticateClient)** :
   - L'eUICC envoie son certificat racine et un challenge cryptographique au SM-DP+.
   - Le SM-DP+ vérifie que l'eUICC est certifiée par la GSMA Root CA.
2. **Key Agreement (ECKA - Elliptic Curve Diffie-Hellman)** :
   - Le SM-DP+ et l'eUICC dérivent une clé de session symétrique éphémère ($K_{enc}$ et $K_{mac}$).
3. **Envoi du Bound Profile Package (BPP)** :
   - Le SM-DP+ compile le profil (incluant la clé $K_i$ et l'IMSI) dans une structure **ASN.1 DER** chiffrée avec $K_{enc}$.
   - Le LPA reçoit ce blob binaire opaque et l'injecte par fragments successifs dans l'eUICC via l'APDU `LOAD BOUND PROFILE PACKAGE` (`0xBF36`).
4. **Décapsulation & Stockage** :
   - L'ISD-R de l'eUICC déchiffre le blob en interne, alloue un nouvel ISD-P, y écrit les fichiers et clés, et renvoie un accusé de réception cryptographique signé à l'opérateur.

---

## ⚡ 4. iSIM (Integrated SIM) : La SIM intégrée au SoC

### Q : Qu'est-ce que l'iSIM et en quoi diffère-t-elle de l'eSIM soudée ?

L'**iSIM** (*Integrated SIM*) supprime purement et simplement le composant silicium dédié sur la carte mère.
Le Secure Element eUICC est directement gravé **à l'intérieur du processeur principal (SoC)** du smartphone (ex: Qualcomm Snapdragon 8 Gen 2/3/4, MediaTek Dimensity 9300).

```
┌────────────────────────────────────────────────────────────────────────┐
│                    SMARTPHONE SoC (Ex: Snapdragon)                     │
│                                                                        │
│  ┌──────────────────────┐  ┌────────────────────────────────────────┐  │
│  │   CPU Cores (Oryon)  │  │        Qualcomm SPU / ARM CryptoCell   │  │
│  │   (Android OS, Apps) │  │  ┌───────────────────────────────────┐ │  │
│  └──────────────────────┘  │  │       ISOLATION MATÉRIELLE        │ │  │
│                            │  │  - Zone mémoire protégée TrustZone │ │  │
│  ┌──────────────────────┐  │  │  - Secure Enclave matériel        │ │  │
│  │   Modem 5G X75/X80   │  │  │  - Firmware iSIM certifié GSMA    │ │  │
│  └──────────────────────┘  │  │    EAL4+ / EAL5+                  │ │  │
│                            │  └───────────────────────────────────┘ │  │
│                            │         (Héberge l'eUICC virtuelle)    │  │
│                            └────────────────────────────────────────┘  │
└────────────────────────────────────────────────────────────────────────┘
```

- **Avantages** : Gain de place immense sur le circuit imprimé, consommation électrique divisée par deux, coût de fabrication réduit.
- **Sécurité** : Certifiée au même niveau d'inviolabilité (**Common Criteria EAL5+**) qu'une puce à carte à puce physique grâce à une enclave sécurisée matérielle hermétique (**SPU** - *Secure Processing Unit*).

---

## 🛠️ 5. Hacking & Manipulation d'eSIM sur Linux / Cloud

### Q : Comment peut-on gérer une eSIM depuis une ligne de commande Linux ou un conteneur ?

Grâce au projet open-source **`lpac`** (écrit en C) ou **`OpenEUICC`**, il est possible d'administrer une eUICC connectée via un lecteur de carte à puce USB PC/SC ou via un modem AT :

```bash
# 1. Lister les profils installés sur l'eUICC connectée en USB
lpac profile list

# Sortie console type :
# [
#   {
#     "iccid": "89331012345678901234",
#     "profileState": "enabled",
#     "profileName": "Orange France",
#     "serviceProviderName": "Orange"
#   },
#   {
#     "iccid": "89012601234567890123",
#     "profileState": "disabled",
#     "profileName": "T-Mobile US",
#     "serviceProviderName": "T-Mobile"
#   }
# ]

# 2. Télécharger et activer un profil via un code d'activation SM-DP+
lpac profile download -s smdp.io -a "LPA:1$smdp.io$B39A-7781-9920-F012"

# 3. Activer un profil spécifique par son ICCID
lpac profile enable 89012601234567890123
```

> **Cartes SIM amovibles programmables (Estk.me / 9esim / sysmocom) :**
> Ces cartes physiques au format Nano-SIM embarquent une véritable puce eUICC avec un LPA interne. Elles permettent d'insérer une "fausse carte SIM" dans n'importe quel modem USB ou vieux téléphone 4G, et d'y flasher des profils eSIM commerciaux via une application Android ou un script bash Linux !
