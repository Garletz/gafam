# 06 — Ghost eSIM & Émulation SIM pour GAFAM / Redroid

> **Guide d'architecture avancée & faisabilité R&D.**
> Format : Questions & Réponses stratégiques, diagrammes de topologies, flux de relais temps réel et modèles d'implémentation pour le projet GAFAM.

---

## 🛑 1. La Réalité Cryptographique : Peut-on « Cloner » une SIM ?

### Q : Pourquoi est-il mathématiquement impossible de copier-coller une carte SIM physique ou une eSIM active sur son serveur Redroid ?

Il s'agit d'une règle absolue de la cryptographie 3GPP :
1. **La clé secrète $K_i$ ne sort jamais du silicium** : Sur une carte SIM 3G/4G/5G ou une eUICC commerciale, la clé $K_i$ (128 ou 256 bits) est gravée dans la mémoire morte sécurisée. Aucun bus matériel, aucune commande APDU, aucun exploit logiciel ne permet de lire le fichier contenant $K_i$.
2. **Protection Anti-Tamper matérielle** : Tenter d'extraire la clé par analyse de consommation (DPA) ou décapage acide déclenche des capteurs internes qui détruisent la mémoire de la puce.
3. **Collision HLR/HSS (L'interdiction du clone simultané)** : Même si un hacker réussissait à dupliquer $K_i$, si deux appareils (le téléphone réel et le Redroid VPS) tentent de s'enregistrer simultanément sur le réseau avec le même IMSI, le compteur de séquence interne $SQN$ de l'algorithme Milenage se désynchronise immédiatement. Le cœur de réseau de l'opérateur détecte une fraude et **bannit définitivement la ligne et la carte SIM**.

---

## 🏗️ 2. Les 4 Architectures Viables pour un « Ghost SIM » dans GAFAM

Puisqu'on ne peut pas cloner la matière première de la carte SIM, comment donner à **Redroid (le clone dans le cloud)** une identité cellulaire vivante et un canal de messagerie SMS/MMS/RCS parfait ?

Voici les 4 modèles d'ingénierie réalisables :

```
┌────────────────────────────────────────────────────────────────────────────┐
│                    LES 4 PATTERNS GHOST SIM POUR GAFAM                     │
│                                                                            │
│  [ Modèle 1 : Soft-vRIL Bridge (100% Logiciel - Recommandé GAFAM) ]        │
│    Redroid Framework <-> vRIL Daemon <==== WebSocket ====> VPC Go Relay    │
│                                                              ▲             │
│                                                              │ AES-GCM     │
│                                                              ▼             │
│                                                     Smartphone Physique    │
│                                                                            │
│  [ Modèle 2 : Remote APDU over IP (SIM Remoting / OMAPI) ]                │
│    Redroid Baseband Mock <=== APDU ISO 7816 via WS ===> APK Real SIM       │
│    (La SIM réelle du téléphone répond aux challenges crypto à distance)   │
│                                                                            │
│  [ Modèle 3 : Hardware Dongle 4G/5G sur le Serveur VPS ]                  │
│    Redroid Container <--- Passthrough USB ---> Dongle Modem (Quectel EC25) │
│    (Une 2ème SIM physique ou eSIM programmable est branchée au serveur)    │
│                                                                            │
│  [ Modèle 4 : VoWiFi / ePDG Tunnel (SMS over IP pur) ]                    │
│    VPC Server <=== IPsec IKEv2 / VoWiFi ===> ePDG Opérateur (Orange/Free) │
└────────────────────────────────────────────────────────────────────────────┘
```

---

## 🎯 3. Modèle 1 : L'Architecture Soft-vRIL (L'Approche Pure GAFAM)

### Q : Comment fonctionne le Soft-vRIL pour synchroniser Redroid et le téléphone sans aucun matériel supplémentaire ?

Dans cette architecture, Redroid ne possède pas de modem radio mais un **Virtual Vendor RIL (vRIL)** développé sur mesure.

```
                      SMARTPHONE PHYSIQUE (Le Corps)
                                    │
                       APK GAFAM (SmsReceiver / Outbox)
                                    │
                                    │ Chiffrement AES-GCM (TCP 5150)
                                    ▼
                         VPC GAFAM (vpc-relay Go)
                                    │
                                    │ WebSocket Local / IPC
                                    ▼
┌──────────────────────────────────────────────────────────────────────────┐
│                         REDROID (Le Ghost Clone)                         │
│                                                                          │
│  ┌────────────────────────────────────────────────────────────────────┐  │
│  │                    APPLICATIONS ANDROID DANS REDROID               │  │
│  │     (WhatsApp, Signal, App SMS native, Appels, 2FA bancaires)      │  │
│  └──────────────────────────────────┬─────────────────────────────────┘  │
│                                     │ Binder IPC (ITelephony)            │
│  ┌──────────────────────────────────▼─────────────────────────────────┐  │
│  │               FRAMEWORK ANDROID (telephony-common.jar)             │  │
│  │   - Détecte une "fausse" carte SIM permanente prête                │  │
│  │   - Stocke les SMS reçus dans content://sms                        │  │
│  └──────────────────────────────────┬─────────────────────────────────┘  │
│                                     │ AIDL Radio Messaging / Sim         │
│  ┌──────────────────────────────────▼─────────────────────────────────┐  │
│  │                    GAFAM vRIL DAEMON (vRIL-daemon)                 │  │
│  │                                                                    │  │
│  │  - Réception SMS : Reçoit le PDU du VPC Go                         │  │
│  │    -> Invoque onNewSms(pdu) sur le HAL AIDL                        │  │
│  │    -> Android croit qu'une antenne 5G vient de lui livrer un SMS ! │  │
│  │                                                                    │  │
│  │  - Émission SMS : Intercepte sendSms(pdu)                          │  │
│  │    -> Transmet le PDU au VPC Go -> relayé au téléphone physique    │  │
│  │    -> Le téléphone physique émet le vrai SMS radio !               │  │
│  └────────────────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────────────────┘
```

### Avantages majeurs pour GAFAM :
- **Zéro coût matériel** : Fonctionne sur un simple VPS DigitalOcean à 6$/mois.
- **Transparence totale pour les apps Android** : Les applications installées dans Redroid voient un numéro de téléphone valide, une carte SIM active et une connectivité réseau continue.
- **Continuité de l'Outbox** : Tout SMS écrit depuis l'interface web de GAFAM ou depuis l'écran de Redroid est acheminé de manière transparente par le téléphone physique.

---

## 📡 4. Modèle 2 : Le Remote APDU (SIM Remoting over IP)

### Q : Peut-on déporter les calculs cryptographiques de la carte SIM du téléphone vers le cloud ?

**Oui !** C'est le principe du **SIM Remoting** (normalisé par l'Open Mobile API - OMAPI / 3GPP TS 27.007 `AT+CCHO` / `AT+CGLA`) :

1. L'application APK GAFAM sur le téléphone physique obtient l'accès bas-niveau à la carte SIM via l'API Android :
   ```kotlin
   val seService = SEService(context, executor, callback)
   val reader = seService.readers.firstOrNull { it.isSecureElementPresent }
   val session = reader?.openSession()
   val channel = session?.openLogicalChannel(USIM_AID)
   ```
2. Lorsque le démon RIL dans Redroid a besoin de calculer un challenge réseau ou de lire un fichier EF protégé (`EF_IMSI`, `EF_LOCI`), il envoie l'APDU brut via une socket WebSocket vers le smartphone.
3. L'APK transmet l'APDU au Secure Element réel :
   ```kotlin
   val responseBytes = channel.transmit(commandApduBytes)
   // Renvoie responseBytes (ex: RES + CK + IK) au Redroid
   ```
4. **Résultat** : Redroid se comporte comme s'il avait la puce physique de l'utilisateur insérée directement dans sa carte mère virtuelle !

---

## 🔌 5. Modèle 3 : Le Dongle 4G/5G USB Physique sur le VPS

### Q : Comment brancher un vrai modem autonome sur le serveur VPC ?

Si l'utilisateur souhaite que son VPS soit **100% indépendant** du smartphone physique (pour continuer à envoyer des SMS et avoir de la data même si son téléphone est éteint) :

1. On branche un modem industriel USB M.2 PCIe sur le serveur (ex: **Quectel EC25-E Mini PCIe** ou **RM500Q-GL 5G**).
2. On insère une **eSIM physique programmable (Estk.me / sysmocom)** ou une carte SIM dédiée (forfait secondaire).
3. Le serveur Linux expose trois périphériques TTY :
   - `/dev/ttyUSB0` : Port de diagnostic / DM.
   - `/dev/ttyUSB2` : Port de commandes AT (pour injecter les SMS PDU et piloter la SIM).
   - `/dev/cdc-wdm0` ou `wwan0` : Interface réseau haut débit (QMI / MBIM).
4. On passe ces périphériques dans le conteneur Docker Redroid via le flag `--device` :
   ```bash
   docker run -it -d --privileged \
     --device=/dev/ttyUSB2:/dev/ttyUSB2 \
     --device=/dev/cdc-wdm0:/dev/cdc-wdm0 \
     -v /data:/data \
     redroid/redroid:14.0.0-latest \
     ro.telephony.default_network=9 \
     rild.libpath=/vendor/lib64/libquectel-ril.so \
     rild.libargs=-d/dev/ttyUSB2
   ```
5. Redroid démarre alors avec **une vraie ligne téléphonique matérielle autonome** !

---

## 🌐 6. Modèle 4 : Le Tunnel VoWiFi / ePDG (SMS over IP sans antenne)

### Q : Comment émettre des SMS directement via le protocole VoWiFi de l'opérateur ?

La plupart des opérateurs modernes (Orange, SFR, Free, Verizon, T-Mobile) supportent le **VoWiFi (Voice/SMS over Wi-Fi)** défini par la norme 3GPP TS 23.402 :
- L'opérateur expose un serveur passerelle public appelé **ePDG** (*Evolved Packet Data Gateway*), par exemple `epdg.epc.mnc001.mcc208.pub.3gppnetwork.org`.
- Le serveur VPS établit un tunnel VPN **IPsec IKEv2** vers l'ePDG de l'opérateur en utilisant l'authentification **EAP-AKA'** (en déléguant les calculs cryptographiques à la SIM via le Remote APDU du Modèle 2).
- Une fois le tunnel IPsec monté, le serveur reçoit une adresse IP interne opérateur et s'enregistre sur le serveur SIP/IMS.
- **Le serveur peut alors envoyer et recevoir de vrais SMS opérateurs via des requêtes SIP MESSAGE**, sans aucune onde radio et sans dongle USB !
