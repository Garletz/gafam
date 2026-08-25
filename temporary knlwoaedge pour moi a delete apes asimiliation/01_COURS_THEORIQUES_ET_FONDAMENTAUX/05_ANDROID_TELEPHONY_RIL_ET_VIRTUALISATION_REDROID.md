# 05 — Android Telephony, RIL & Virtualisation Redroid

> **Guide d'ingénierie interne sur la pile de téléphonie AOSP et son émulation dans Docker / Redroid.**
> Format : Questions & Réponses approfondies, flux IPC Java/C++, décomposition des interfaces AIDL et techniques d'injection de fausses couches radio.

---

## 📱 1. La Pile Téléphonie Complète d'Android (AOSP Architecture)

### Q : Comment un appel ou un SMS traverse-t-il les couches logicielles d'Android jusqu'au modem physique ?

Dans Android (AOSP), la téléphonie est l'un des sous-systèmes les plus complexes et hiérarchisés du système d'exploitation :

```
┌────────────────────────────────────────────────────────────────────────────┐
│                       APPLICATIONS UTILISATEUR & SYSTÈME                   │
│   [ Google Messages ]    [ Dialer / Téléphone ]    [ APK GAFAM Relay ]     │
└─────────────────────────────────────┬──────────────────────────────────────┘
                                      │ Android SDK APIs
┌─────────────────────────────────────▼──────────────────────────────────────┐
│                    FRAMEWORK JAVA DE HAUT NIVEAU (SDK)                     │
│    - android.telephony.TelephonyManager                                    │
│    - android.telephony.SmsManager                                          │
│    - android.telephony.SubscriptionManager                                 │
└─────────────────────────────────────┬──────────────────────────────────────┘
                                      │ Binder IPC (ITelephony.aidl)
┌─────────────────────────────────────▼──────────────────────────────────────┐
│                 SYSTEM SERVICE (telephony-common.jar)                      │
│    - com.android.internal.telephony.Phone (GsmCdmaPhone)                   │
│    - com.android.internal.telephony.uicc.UiccController (Gestion SIM)      │
│    - com.android.internal.telephony.IccSmsInterfaceManager                 │
│    - com.android.internal.telephony.RIL (RILJ - Java Client)               │
└─────────────────────────────────────┬──────────────────────────────────────┘
                                      │ Android IPC (AIDL / HIDL HAL)
                                      │ Interface: android.hardware.radio.*
┌─────────────────────────────────────▼──────────────────────────────────────┐
│                  DAEMON NATIF SYSTÈME : RILD (rild.c)                      │
│    - Charge la bibliothèque Vendor RIL partagée (libril.so)                │
└─────────────────────────────────────┬──────────────────────────────────────┘
                                      │ C / C++ Native Calls
┌─────────────────────────────────────▼──────────────────────────────────────┐
│                 VENDOR RIL (libreference-ril.so / libqcril)                │
│    - Reçoit les requêtes structurées (ex: RIL_REQUEST_SEND_SMS)           │
│    - Traduit en commandes AT (3GPP 27.007) ou trames QMI / MBIM           │
│    - Gère les réponses asynchrones spontanées (UNSOL : Appel entrant, etc.)│
└─────────────────────────────────────┬──────────────────────────────────────┘
                                      │ Port Série TTY / Bus USB / Shared Mem
┌─────────────────────────────────────▼──────────────────────────────────────┐
│                      MODEM BASEBAND CELLULAIRE MATÉRIEL                    │
│    - Processeur radio dédié (DSP Qualcomm / MediaTek / Exynos)             │
│    - Pilote le Secure Element SIM physique via ISO 7816-3                  │
└────────────────────────────────────────────────────────────────────────────┘
```

---

### Q : Quelle est la transition de HIDL vers AIDL dans les versions modernes d'Android (12, 13, 14, 15) ?

Jusqu'à Android 11, la couche d'abstraction matérielle (**HAL**) utilisait **HIDL** (*HAL Interface Definition Language*).
Depuis **Android 12+**, tout le sous-système radio a été modularisé en interfaces **AIDL stables** (`android.hardware.radio.*`) :

1. **`android.hardware.radio.sim`** : Gestion de l'état de la carte SIM, vérification du code PIN, sélection des applications USIM/ISIM, lecture des fichiers EF via APDU (`sendIccApduLogicalChannel`).
2. **`android.hardware.radio.messaging`** : Envoi et réception de SMS au format PDU brut (`sendSms`, `sendSmsExpectMore`, `acknowledgeLastIncomingGsmSms`), configuration des accusés de réception et activation du Cell Broadcast.
3. **`android.hardware.radio.network`** : Scan des antennes disponibles, attachement manuel/automatique à un opérateur, rapport de force du signal (`SignalStrength`), sélection du mode radio (2G/3G/4G/5G).
4. **`android.hardware.radio.data`** : Négociation des contextes PDP / PDU Sessions, attachement des profils APN (Internet, MMS, IMS), attribution des adresses IP et création des interfaces réseau virtuelles (`rmnet_data0`, `ccmni0`).
5. **`android.hardware.radio.voice`** : Établissement des appels vocaux CS et VoLTE, gestion des conférences téléphoniques et tonalités DTMF.

---

## 🐳 2. La Téléphonie dans Redroid (Android in Docker)

### Q : Comment fonctionne la téléphonie par défaut dans Redroid sur un serveur VPS ?

**Redroid** (*Remote Android in Docker*) est une distribution AOSP compilée pour s'exécuter dans un conteneur Linux x86_64 ou ARM64 sans émulateur QEMU lourd (accès direct aux cibles graphiques Mesa / Gallium / Ashmem / Binder).

Par défaut dans une image standard Redroid :
1. Aucun composant modem physique n'est monté dans le conteneur (`/dev/ttyUSB*` ou `/dev/smd*` sont absents).
2. Le démon `rild` n'est pas démarré ou charge un stub vide.
3. Le framework Java `TelephonyManager` détecte l'absence de SIM (`SIM_STATE_ABSENT`) et bascule le réseau en mode `NO_SERVICE` ou Mode Avion forcé.
4. Les applications dépendantes de la téléphonie (WhatsApp, Signal, Telegram, SMS standard) affichent une absence de carte SIM ou refusent de s'enregistrer si elles vérifient le numéro local via `TelephonyManager.getLine1Number()`.

---

### Q : Comment fonctionne le composant `reference-ril.c` / `mock-ril` d'AOSP ?

AOSP inclut une implémentation de référence en C appelée **`reference-ril.c`** :
- Au lieu de dialoguer avec un vrai modem, `reference-ril` peut ouvrir une socket TCP ou un pseudo-terminal TTY (`/dev/pts/X`).
- Il intercepte les commandes AT envoyées par Android et renvoie des réponses simulées conformes au standard 3GPP 27.007 :

```c
// Extrait conceptuel de traitement de reference-ril.c
static void onRequest (int request, void *data, size_t datalen, RIL_Token t) {
    switch (request) {
        case RIL_REQUEST_GET_SIM_STATUS: {
            RIL_CardStatus_v6 card_status;
            memset(&card_status, 0, sizeof(card_status));
            card_status.card_state = RIL_CARDSTATE_PRESENT; // Force la présence SIM
            card_status.universal_pin_state = RIL_PINSTATE_ENABLED_VERIFIED;
            card_status.num_applications = 1;
            card_status.applications[0].app_type = RIL_APPTYPE_USIM;
            card_status.applications[0].app_state = RIL_APPSTATE_READY;
            RIL_onRequestComplete(t, RIL_E_SUCCESS, &card_status, sizeof(card_status));
            break;
        }
        case RIL_REQUEST_SEND_SMS: {
            const char **pdu_payload = (const char **)data;
            // On peut intercepter ici le PDU sortant généré par Redroid !
            send_pdu_to_gafam_vpc(pdu_payload[0], pdu_payload[1]);
            RIL_SMS_Response response = { .messageRef = 1, .ackPDU = NULL };
            RIL_onRequestComplete(t, RIL_E_SUCCESS, &response, sizeof(response));
            break;
        }
    }
}
```

---

## 💉 3. Injection d'État Réseau & Spoofing Téléphonique

### Q : Peut-on tromper Android sans recompiler tout l'OS via des propriétés système (System Properties) ?

Pour forcer Android à afficher un opérateur et une connexion réseau simulée, on peut injecter des propriétés système spécifiques via `setprop` dans Redroid :

```bash
# 1. Simuler l'état prêt de la carte SIM
setprop gsm.sim.state READY
setprop gsm.sim.operator.numeric 20801
setprop gsm.sim.operator.alpha "Orange"
setprop gsm.sim.operator.iso-country fr

# 2. Simuler l'enregistrement sur le réseau cellulaire
setprop gsm.network.type LTE
setprop gsm.operator.numeric 20801
setprop gsm.operator.alpha "Orange F"
setprop gsm.operator.iso-country fr
setprop gsm.operator.isroaming false

# 3. Forcer le signal radio au maximum
setprop gsm.network.signal 31
```

> **Attention :** Les applications modernes ne lisent plus uniquement ces propriétés `setprop` statiques ; elles interrogent l'API Binder de `TelephonyManager`. Pour une simulation 100% robuste, il est nécessaire d'intervenir au niveau du **HAL AIDL Radio** ou via un module Xposed/Frida (ex: `FakeContext.java`).
