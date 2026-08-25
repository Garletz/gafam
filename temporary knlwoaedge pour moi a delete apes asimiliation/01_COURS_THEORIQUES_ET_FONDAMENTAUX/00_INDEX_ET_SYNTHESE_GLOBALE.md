# 00 — Index & Synthèse Stratégique : Du Réseau Cellulaire au Ghost eSIM GAFAM

> **Document de référence & Guide d'apprentissage technique.**
> Format : Questions / Réponses expertes, Architecture système, Protocoles 3GPP, Radio, Hardware & Emulation.
> Contexte projet : **GAFAM (Ghost Android Framework / Sovereign VPC Relay & Redroid Phone Clone)**.

---

## 🗺️ Cartographie des Modules de Connaissance

Cette base de connaissances est structurée en **8 volumes techniques exhaustifs**, pensés pour démonter brique par brique le fonctionnement des réseaux cellulaires (2G à 6G), de la puce SIM physique jusqu'aux couches les plus profondes d'AOSP et de la virtualisation dans Redroid.

| Fichier | Thématique Principale | Concepts Clés Décortiqués |
| :--- | :--- | :--- |
| **[01. Histoire & Physique des Réseaux 2G à 6G](./01_HISTOIRE_ET_PHYSIQUE_DES_RESEAUX_2G_A_6G.md)** | Évolution des cœurs de réseau & Couche Radio (RAN) | SS7/MAP, EPC, 5GC SBA (HTTP/2 + JSON), Bandes mondiales FDD/TDD (B1..B85, n1..n260), IMSI Attach, TAU, PDU Session, Handover, Chiffrement A5/1..3, ZUC, SNOW 3G. |
| **[02. Anatomie Crue de la Carte SIM](./02_ANATOMIE_CRUE_DE_LA_CARTE_SIM_LE_MINI_ORDI_PARASITE.md)** | Le Secure Element matériel et son OS | Microcontrôleur, JavaCard OS, GlobalPlatform, Système de fichiers ISO 7816-4 (MF, DF, EF_IMSI, EF_LOCI), APDU (T=0, T=1), Milenage/TUAK, Ki/OPc, SIM Toolkit (STK/USAT/BIP). |
| **[03. eSIM, eUICC, iSIM & Provisioning GSMA](./03_ESIM_EUICC_ISIM_ET_PROVISIONING_GSMA.md)** | La dématérialisation et la sécurité GSMA RSP | Architecture eUICC (ECASD, ISD-P, ISD-R), Spécifications GSMA SGP.22 & SGP.32, LPA (LPD/LPR/LPU), Dialogues SM-DP+ en ASN.1 DER / BER-TLV, Certificats CI, iSIM SoC. |
| **[04. SMS, MMS, RCS & Messagerie Sous le Capot](./04_SMS_MMS_RCS_ET_MESSAGERIE_CELLULAIRE_SOUS_LE_CAPOT.md)** | Encodage PDU, transport radio et protocoles IP | SMS over NAS, SMSoIP (VoLTE/IMS SIP MESSAGE), SMS over SGs (CSFB), Découpage TPDU (GSM 7-bit, UCS2, UDHI concaténation, Flash SMS classe 0, OTA classe 2), MMS WAP Push, RCS MSRP & Google Jibe API. |
| **[05. Android Telephony, RIL & Redroid](./05_ANDROID_TELEPHONY_RIL_ET_VIRTUALISATION_REDROID.md)** | Le pipeline Téléphonie d'AOSP et sa virtualisation | `TelephonyManager` → `RILJ` → AIDL/HIDL `IRadio` / `ISim` → Vendor RIL (`libril.so`) → Modem Baseband (AT/QMI/MBIM). Émulation dans Redroid : `libril-mock`, fake SIM, fake radio. |
| **[06. Ghost eSIM & Émulation SIM pour GAFAM](./06_GHOST_ESIM_ET_EMULATION_SIM_POUR_GAFAM_REDROID.md)** | Faisabilité, Patterns d'architecture & R&D | Impossibilité cryptographique du clone brut sans Ki, Architecture Soft-vRIL, Remote APDU over WebSocket (SIM Remoting), Dongle USB physique sur VPC, Bridge Hybride Smartphone GAFAM. |
| **[07. Commandes AT, Hardware Hacks & SDR](./07_COMMANDES_AT_MATERIEL_HACKS_ET_SDR.md)** | L'arsenal bas-niveau de l'ingénieur radio | TS 27.005/27.007, Commandes raw AT (`AT+CRSM`, `AT+CGLA`, `AT+CMGS`), Modems Quectel/Fibocom, Multiplexage 3GPP 27.010, Software Defined Radio (SDR, HackRF, Open5GS, srsRAN), IMSI-Catchers. |

---

## 🎯 Problématique Centrale : Pourquoi cette recherche pour GAFAM ?

### Le constat actuel du projet GAFAM
1. **Ce qui tourne déjà impeccablement :** L'APK Android Kotlin intercepte les SMS reçus, surveille l'Outbox, écoute les notifications RCS et pousse le tout via AES-GCM avec Certificate Pinning vers le VPC Go (`vpc-relay`). L'interface Web SvelteKit 5 affiche et gère tout en direct.
2. **La limite physique :** Le smartphone réel reste le *point d'ancrage unique* sur le réseau radio de l'opérateur (Orange, SFR, Free, Verizon, T-Mobile...). Si le téléphone n'a plus de batterie, est confisqué ou est hors ligne, le VPC perd la passerelle cellulaire native.
3. **L'ambition "Ghost Clone" / Redroid (Manifests 18 & 14) :**
   - Comment héberger un clone Android (Redroid sous Docker) dans le cloud qui soit **conscient du réseau cellulaire** ?
   - Peut-on y injecter une **Ghost eSIM** ?
   - Peut-on faire en sorte que Redroid puisse croire qu'il a une carte SIM active, émettre et recevoir de vrais SMS, ou négocier des sessions data et IMS ?
   - Quelles sont les limites infranchissables de la cryptographie 3GPP (clef $K_i$ / $OP_c$) et quelles sont les contournements viables d'ingénierie (APDU over IP, vRIL, VoIP/IMS Gateway, VoWiFi, SMS over IP) ?

---

## 🧠 Lexique & Acronymes Incontournables (Cheat-Sheet 3GPP / Telco)

| Acronyme | Signification | Rôle Fondamental |
| :--- | :--- | :--- |
| **3GPP** | 3rd Generation Partnership Project | Consortium international définissant les spécifications 2G, 3G, 4G, 5G et 6G. |
| **AIDL** | Android Interface Definition Language | Mécanisme IPC d'Android moderne remplaçant HIDL pour le lien RILJ ↔ Vendor RIL. |
| **AKA** | Authentication and Key Agreement | Protocole cryptographique de challenge/réponse (RAND, AUTN, RES, $K_{ASME}$, $K_{SEAF}$) entre le cœur de réseau et la SIM. |
| **APDU** | Application Protocol Data Unit | Paquet binaire standard (ISO 7816-4) échangé entre le lecteur/baseband et la carte SIM (`CLA INS P1 P2 Lc Data Le`). |
| **ATR** | Answer To Reset | Octets envoyés par la carte SIM à l'allumage pour déclarer ses paramètres physiques et de transmission (T=0 ou T=1). |
| **BIP** | Bearer Independent Protocol | Protocole du SIM Toolkit permettant à la SIM d'ouvrir des sockets TCP/UDP directes via le modem pour des updates OTA sans passer par l'OS Android. |
| **CSFB** | Circuit-Switched Fallback | Mécanisme 4G forçant le smartphone à rétrograder temporairement en 2G/3G pour passer un appel vocal ou un SMS si la VoLTE n'est pas activée. |
| **eUICC** | Embedded Universal Integrated Circuit Card | Puce physique soudée (eSIM) capable d'accueillir et de commuter plusieurs profils d'opérateurs de façon sécurisée. |
| **EPC** | Evolved Packet Core | Cœur de réseau 4G LTE (composé du MME, SGW, PGW, HSS). |
| **5GC** | 5G Core | Cœur de réseau 5G Standalone bâti sur une architecture orientée microservices (SBA) avec APIs HTTP/2 et JSON (AMF, SMF, UPF, UDM, AUSF, SMSF). |
| **FDD / TDD** | Frequency / Time Division Duplex | Séparation émission/réception radio par deux fréquences distinctes (FDD) ou par des créneaux temporels alternés sur une seule fréquence (TDD). |
| **GSMA RSP** | GSMA Remote SIM Provisioning | Norme (SGP.22 / SGP.32) encadrant le téléchargement et l'activation chiffrée de profils eSIM over-the-air. |
| **ICCID** | Integrated Circuit Card Identifier | Numéro de série unique à 19 ou 20 chiffres gravé sur la carte SIM (EF_ICCID `0x2FE2`). |
| **IMSI** | International Mobile Subscriber Identity | Identifiant unique mondial de l'abonné (MCC + MNC + MSIN), stocké dans le fichier SIM `0x6F07`. Remplacé par le **SUCI** en 5G pour éviter l'écoute passive. |
| **Ki** | Secret Authentication Key | Clef symétrique secrète de 128 ou 256 bits partagée exclusivement entre le Secure Element de la SIM et le HLR/AuC/UDM de l'opérateur. **Non extractible par design**. |
| **LPA** | Local Profile Assistant | Logiciel (dans Android ou dans l'eUICC) responsable de télécharger et gérer les profils eSIM (découpage en LPD, LPR, LPU). |
| **Milenage** | Algorithme d'authentification 3GPP | Ensemble de fonctions cryptographiques ($f_1$ à $f_5$) basées sur AES-128 utilisées pour dériver les clefs de chiffrement de session et vérifier le réseau. |
| **NAS** | Non-Access Stratum | Couche protocolaire reliant directement le smartphone au cœur de réseau (MME / AMF), indépendamment de l'antenne radio intermédiaire. Gère l'authentification, la mobilité et les SMS over NAS. |
| **PDU** | Protocol Data Unit | Format binaire brut utilisé pour encoder les SMS (`SMS-SUBMIT`, `SMS-DELIVER`) contenant les timestamps, adresses et octets de charge utile GSM 7-bit ou UCS2. |
| **RIL** | Radio Interface Layer | Couche logicielle dans Android faisant le pont entre les APIs de haut niveau (Java/Kotlin) et le driver matériel du modem baseband. |
| **SDR** | Software Defined Radio | Matériel radio reconfigurable par logiciel (HackRF, USRP, BladeRF) capable d'émettre et recevoir n'importe quel signal brut I/Q de 1 MHz à 6 GHz. |
| **SM-DP+** | Subscription Manager Data Preparation + | Serveur de l'écosystème GSMA qui chiffre et délivre le profil eSIM (paquet ASN.1 Bound Profile Package) à destination de l'eUICC du client. |
| **STK / USAT** | SIM Toolkit / USIM Applet Toolkit | Système permettant à la carte SIM d'envoyer des commandes proactives au terminal (afficher un menu, envoyer un SMS secret, composer un numéro, lancer une URL). |
| **VoLTE / IMS** | Voice over LTE / IP Multimedia Subsystem | Architecture SIP/IP permettant de faire transiter les appels voix haute définition et les SMS (`SMSoIP`) sur le canal paquet 4G/5G/Wi-Fi sans 2G/3G. |

---

## ⚡ Conseils de Lecture

1. Si vous cherchez à comprendre **pourquoi une carte SIM est inviolable et comment fonctionnent ses algorithmes secrets**, commencez par le **[Module 02](./02_ANATOMIE_CRUE_DE_LA_CARTE_SIM_LE_MINI_ORDI_PARASITE.md)**.
2. Si vous voulez concevoir l'architecture **Ghost eSIM / Redroid pour GAFAM**, sautez directement au **[Module 05](./05_ANDROID_TELEPHONY_RIL_ET_VIRTUALISATION_REDROID.md)** puis au **[Module 06](./06_GHOST_ESIM_ET_EMULATION_SIM_POUR_GAFAM_REDROID.md)**.
3. Si vous voulez maîtriser **l'artillerie bas-niveau (PDU, AT commands, reverse radio)**, explorez les **[Modules 04](./04_SMS_MMS_RCS_ET_MESSAGERIE_CELLULAIRE_SOUS_LE_CAPOT.md)** et **[07](./07_COMMANDES_AT_MATERIEL_HACKS_ET_SDR.md)**.
