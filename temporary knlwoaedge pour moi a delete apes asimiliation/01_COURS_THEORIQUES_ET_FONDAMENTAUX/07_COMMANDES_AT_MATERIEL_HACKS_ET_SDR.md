# 07 — Commandes AT, Matériel, Hacks & Software Defined Radio (SDR)

> **L'arsenal pratique bas-niveau du chercheur en télécommunications.**
> Format : Questions & Réponses, cheat-sheets de commandes AT brutes, architecture des pilotes Linux pour modems industriels et guide des stacks SDR open-source (Open5GS / srsRAN).

---

## 💻 1. Le Cheat-Sheet Ultime des Commandes AT (3GPP TS 27.005 & 27.007)

### Q : Quelles sont les commandes AT indispensables pour dialoguer directement avec un modem cellulaire ?

Les commandes AT permettent de piloter un modem via un terminal série (ex: `picocom -b 115200 /dev/ttyUSB2` ou `socat`).

```
+-----------------------------------+-----------------------------------------------------------------------+
| Commande AT                       | Rôle & Réponse Type                                                   |
+-----------------------------------+-----------------------------------------------------------------------+
| `AT`                              | Test de présence (Réponse: `OK`)                                      |
| `ATE0`                            | Désactive l'écho local des commandes saisies                          |
| `ATI`                             | Affiche le modèle du modem, constructeur et version du firmware      |
| `AT+CGSN`                         | Retourne l'IMEI du modem (ex: `861234567890123`)                      |
| `AT+CIMI`                         | Lit directement l'IMSI stocké dans la carte SIM (`208010123456789`)   |
| `AT+CPIN?`                        | État du code PIN (`+CPIN: READY` ou `+CPIN: SIM PIN`)                 |
| `AT+CPIN="1234"`                  | Déverrouille la carte SIM avec le code PIN                            |
| `AT+CSQ`                          | Niveau de signal radio 2G/3G (RSSI 0-31, BER 0-7, ex: `+CSQ: 28,0`)   |
| `AT+CESQ`                         | Signal étendu 4G/5G (RSRP, RSRQ en dBm)                               |
| `AT+COPS?`                        | Opérateur actuellement connecté (ex: `+COPS: 0,0,"Orange",7`)         |
| `AT+CREG?` / `AT+CEREG?`          | État d'enregistrement réseau (1 = Local Home, 5 = Roaming)            |
+-----------------------------------+-----------------------------------------------------------------------+
```

---

### Q : Comment injecter des requêtes APDU brutes dans la carte SIM via les commandes AT `+CRSM` et `+CGLA` ?

#### 1. Commande `AT+CRSM` (Restricted SIM Access)
Permet de lire ou écrire un fichier EF du système de fichiers ISO 7816-4 sans ouvrir de canal logique :
```bash
# Syntaxe : AT+CRSM=<command>,<file_id>,<p1>,<p2>,<length>

# Exemple : Lire l'ICCID (Fichier EF 0x2FE2 = 12258 en décimal)
AT+CRSM=176,12258,0,0,10
# Réponse : +CRSM: 144,0,"988310214365870921F3" (SW1=144/0x90, SW2=0/0x00 -> OK!)
```

#### 2. Commande `AT+CGLA` (Generic Logical Channel Access)
Permet d'envoyer **n'importe quel octet APDU brut** à une applet JavaCard spécifique (USIM, ISIM, Applet bancaire) :
```bash
# 1. Ouvrir un canal logique vers l'AID USIM
AT+CCHO="A0000000871002FF86FF0389FFFFFFFF"
# Réponse : +CCHO: 1 (Canal logique ID 1 ouvert)

# 2. Envoyer une commande APDU SELECT FILE (00 A4 00 04 02 6F 07) sur le canal 1
AT+CGLA=1,14,"00A40004026F07"
# Réponse : +CGLA: 4,"9000"

# 3. Fermer le canal logique
AT+CCHC=1
```

---

### Q : Comment envoyer et lire des SMS en mode PDU brut via AT ?

```bash
# 1. Configurer le modem en mode PDU (0 = PDU, 1 = Mode Texte simplifié)
AT+CMGF=0
# Réponse : OK

# 2. Configurer la notification spontanée des SMS entrants (Push immédiat vers le TTY)
AT+CNMI=2,2,0,0,0
# Réponse : OK
# Dès qu'un SMS arrive, le modem crache : +CMT: ,28 \r\n 079133... (le PDU brut)

# 3. Émettre un SMS en mode PDU
AT+CMGS=23   # Longueur du TPDU en octets (hors numéro SCA)
# Le modem renvoie le prompt '>'
> 0001000A81503810423000000BD4F29C0E6A97E7F3F0B90C <Ctrl+Z / 0x1A>
# Réponse : +CMGS: 42 (Message envoyé avec succès avec la référence 42)
```

---

## 🔌 2. Modems Industriels & Pilotes Linux (QMI / MBIM / CDC-WDM)

### Q : Quels modems USB choisir et comment Linux les prend-il en charge ?

```
+--------------------------+-----------------------+---------------------+-----------------------------------+
| Modèle                   | Débits Max            | Bandes Supportées   | Protocoles Pilotes Linux          |
+--------------------------+-----------------------+---------------------+-----------------------------------+
| **Quectel EC25-E**       | 4G Cat 4 (150 Mbps)   | B1/B3/B7/B8/B20     | `qmi_wwan` / `option` (Serial AT) |
| **Quectel RM500Q-GL**    | 5G Sub-6 (2.5 Gbps)   | Toutes bandes n1..n79| `qmi_wwan` / `mhi_pci` / MBIM     |
| **Fibocom FM350-GL**     | 5G Sub-6 (3.4 Gbps)   | Mondial (M.2 PCIe)  | `cdc_mbim` / `iosm` (Intel PCIe)  |
| **SimCom SIM7600E**      | 4G Cat 1 (10 Mbps)    | B1/B3/B7/B8/B20     | `cdc_ether` / RNDIS               |
+--------------------------+-----------------------+---------------------+-----------------------------------+
```

### Le protocole QMI (Qualcomm MSM Interface) :
Plutôt que d'utiliser des commandes AT textuelles lentes, les modems Qualcomm utilisent le protocole binaire **QMI** via le pilote Linux `qmi_wwan` et l'outil en ligne de commande `qmicli` :
```bash
# Obtenir le statut réseau complet en binaire QMI
qmicli -d /dev/cdc-wdm0 --nas-get-serving-system

# Démarrer une session de données haute performance avec APN
qmicli -d /dev/cdc-wdm0 --wds-start-network="apn='orange.fr',ip-type=4" --client-no-release-cid
```

---

## 📻 3. Software Defined Radio (SDR) & Laboratoire Télécom Personnel

### Q : Comment peut-on émuler sa propre antenne 4G/5G avec un HackRF ou un USRP ?

La **Radio Logicielle (SDR)** permet d'émettre et de recevoir n'importe quel signal radiofréquence en temps réel en déléguant le calcul mathématique de la modulation au CPU du PC.

```
┌────────────────────────────────────────────────────────────────────────┐
│                        LABORATOIRE RADIO OPEN-SOURCE                   │
│                                                                        │
│  ┌──────────────────────────────────────────────────────────────────┐  │
│  │     CŒUR DE RÉSEAU 4G/5G COMPLET : Open5GS (Écrit en C)          │  │
│  │   - MME, HSS, SGW, PGW, PCRF (4G EPC)                           │  │
│  │   - AMF, SMF, UPF, UDM, AUSF, NRF, SMSF (5G Standalone)         │  │
│  │   - Interface Web de provisioning d'abonnés (IMSI, Ki, OPc)     │  │
│  └──────────────────────────────────┬───────────────────────────────┘  │
│                                     │ Interface standardisée N2 / N3    │
│  ┌──────────────────────────────────▼───────────────────────────────┐  │
│  │       STATION DE BASE LOGICIELLE (eNodeB / gNodeB) : srsRAN      │  │
│  │   - Calcule la couche physique OFDM, canal PRACH, modulation QAM │  │
│  └──────────────────────────────────┬───────────────────────────────┘  │
│                                     │ Échantillons I/Q bruts en USB3    │
│  ┌──────────────────────────────────▼───────────────────────────────┐  │
│  │       BOÎTIER SDR MATÉRIEL (BladeRF 2.0 micro / USRP B210)       │  │
│  │   - Émetteur/Récepteur RF Full Duplex 70 MHz à 6 GHz            │  │
│  └──────────────────────────────────┬───────────────────────────────┘  │
│                                     │ Ondes Radioélectriques            │
│                                     ▼                                  │
│                   Smartphone de Test avec SIM Programmable              │
└────────────────────────────────────────────────────────────────────────┘
```

### Cas d'usage pour la R&D GAFAM :
1. **Tester le comportement de Redroid et du relais SMS** dans des conditions radio extrêmes (perte de paquet, coupure réseau, bascule 4G $\rightarrow$ 2G).
2. **Tester des attaques d'injection SMS PDU** et analyser comment AOSP et les applications de messagerie réagissent aux commandes proactives STK.
3. **Simuler un réseau opérateur privé privé sécurisé** totalement isolé d'Internet.

---

## 🕵️ 4. Sécurité Offensive & Contre-Mesures

### Q : Comment fonctionne une attaque par IMSI-Catcher (Stingray) et comment s'en prémunir ?

1. **Le Piège** : L'attaquant configure un faux eNodeB/BTS avec un niveau de puissance radio supérieur aux antennes environnantes.
2. **Attraction** : Tous les smartphones à portée effectuent un *Handover* spontané vers l'antenne la plus forte.
3. **Downgrade Forcé (2G Fallback)** : L'antenne refuse la négociation 4G/5G et force le mobile à basculer en **2G GSM**.
4. **Désactivation du Chiffrement** : L'antenne envoie la commande `Ciphering Mode Command` avec l'algorithme **A5/0** (aucun chiffrement).
5. **Espionnage** : Tous les appels et SMS en clair transitent par le PC de l'attaquant.

### Contre-mesures Android & GAFAM :
- **Désactiver la 2G dans Android** : Depuis Android 12, Google permet de bloquer complètement la 2G dans `Paramètres -> Réseau -> Autoriser la 2G = DÉSACTIVÉ`. Cela immunise le terminal contre 95% des IMSI-Catchers commerciaux !
- **Forcer la 5G Standalone avec chiffrement SUCI** : Rend la capture d'IMSI passive totalement inopérante.
