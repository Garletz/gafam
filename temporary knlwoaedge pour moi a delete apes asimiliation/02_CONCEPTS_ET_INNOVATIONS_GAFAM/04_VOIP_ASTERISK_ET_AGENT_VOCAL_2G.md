# 10 — VoIP, Asterisk & Agent Vocal IA sur Réseau 2G pour GAFAM

> **Guide d'implémentation complet : Transformer un appel GSM gratuit à 0€ de data en passerelle IA souveraine.**
> Format : Architecture logicielle, configurations prêtes à l'emploi (`pjsip.conf`, `extensions.conf`), méthodes 100% gratuites (0€) et analyse des **numéros à vie sans abonnement**.

---

## 🤯 1. La Vision Globale : Le Téléphone GSM comme Terminal Vocal Universel

```
┌────────────────────────────────────────────────────────────────────────────┐
│                  L'ARCHITECTURE DU NŒUD VOCAL SOUVERAIN                    │
│                                                                            │
│  [ N'IMPORTE QUEL TÉLÉPHONE (Nokia 3310, iPhone, Android, Fixe) ]          │
│       │                                                                    │
│       │ 📞 Appel classique vers ton numéro fixe (01 89 XX XX XX)           │
│       │    (Inclus dans n'importe quel forfait à 2€, 0 DATA REQUIS)        │
│       ▼                                                                    │
│  [ ANTENNE GSM / OPÉRATEUR (Orange, Free, SFR...) ]                        │
│       │                                                                    │
│       │ 🌐 Protocole SIP / RTP Audio (Internet)                            │
│       ▼                                                                    │
│  ┌──────────────────────────────────────────────────────────────────────┐  │
│  │                     TON SERVEUR VPS GAFAM (Port 5060)                │  │
│  │                                                                      │  │
│  │   ┌───────────────────────────────────────────────────────────────┐  │  │
│  │   │             ASTERISK 20+ (Moteur VoIP / PBX)                  │  │  │
│  │   │  - Décroche automatiquement la ligne                          │  │  │
│  │   │  - Détecte les touches du clavier (DTMF : 1, 2, 3...)         │  │  │
│  │   │  - Stream le flux audio en direct (Bidirectionnel)            │  │  │
│  │   └───────────────────────────────┬───────────────────────────────┘  │  │
│  │                                   │ Audio Stream / ARI WebSockets    │  │
│  │   ┌───────────────────────────────▼───────────────────────────────┐  │  │
│  │   │                 PIPELINE IA TEMPS RÉEL (Go / Python)          │  │  │
│  │   │                                                               │  │  │
│  │   │   [ Whisper Fast / STT ] ──> Convertit ta voix en texte       │  │  │
│  │   │            │                                                  │  │  │
│  │   │            ▼                                                  │  │  │
│  │   │   [ Saṃyojaka / LLM (Qwen / DeepSeek) ] ──> Réfléchit & Agit  │  │  │
│  │   │            │                                                  │  │  │
│  │   │            ▼                                                  │  │  │
│  │   │   [ Piper / Coqui TTS ] ──> Synthétise la voix                │  │  │
│  │   └───────────────────────────────┬───────────────────────────────┘  │  │
│  │                                   │                                  │  │
│  │   Tu entends l'IA te répondre directement dans le combiné ! 🎧       │  │
│  └──────────────────────────────────────────────────────────────────────┘  │
└────────────────────────────────────────────────────────────────────────────┘
```

---

## 🆓 2. Les 4 Méthodes 100% GRATUITES (0 € / 0 Abonnement)

Tu n'es absolument **pas obligé de payer un abonnement de numéro à 1€**. Voici les alternatives 100% gratuites :

```
+-----------------------------------+--------------------+------------------------+------------------------------------+
| Méthode                           | Coût Mensuel       | Type d'Identifiant     | Accessibilité Réseau               |
+-----------------------------------+--------------------+------------------------+------------------------------------+
| **1. Freebox SIP / Box Maison**   | **0,00 €**         | Vrai numéro fixe (09..) | ✅ Accessible depuis n'importe quel tel |
| **2. SIP Direct IP / URI**        | **0,00 €**         | `sip:agent@gafam.cloud`| ⚠️ Nécessite Data/Wi-Fi             |
| **3. WebRTC Audio Stream**        | **0,00 €**         | Micro sur Web/App      | ⚠️ Nécessite Data/Wi-Fi             |
| **4. Dongle USB + SIM existante** | **0,00 €**         | Numéro de ta SIM       | ✅ Accessible depuis n'importe quel tel |
+-----------------------------------+--------------------+------------------------+------------------------------------+
```

### Détail des options gratuites :
1. **Le Compte SIP de ta Box Internet (Freebox, etc.)** : Les box internet incluent gratuitement une ligne fixe. Tu récupères les identifiants SIP de ta Freebox et tu les injectes dans Asterisk sur ton VPS. Dès que tu appelles le fixe de ta maison, c'est ton IA GAFAM qui décroche !
2. **SIP Direct IP (Comme un email)** : Tu composes `sip:gary@ton-vps.com` depuis une application SIP gratuite (Linphone). 0 numéro, 0 opérateur, 100% chiffré.
3. **Le WebRTC dans le Dashboard GAFAM** : Tu cliques sur le micro dans ton interface web `gafam.cloud`, et tu parles à ton IA en direct en audio HD ultra-rapide (codec Opus).
4. **Le Dongle USB branché avec une carte SIM préexistante** : Utilise une vieille SIM à 0€ ou un forfait secondaire branché sur le serveur.

---

## ♾️ 3. Peut-on acheter un Numéro de Téléphone "À VIE" sans abonnement mensuel ?

### La réalité juridique des numéros E.164 :
Les numéros de téléphone normaux (`+33...`, `+1...`) appartiennent aux États (ARCEP en France, FCC aux USA). Les États prélèvent une taxe annuelle de gestion aux opérateurs, ce qui explique pourquoi la plupart des opérateurs facturent un loyer mensuel.

### MAIS il existe 3 solutions réelles pour avoir un numéro "À Vie / One-Time Payment" :

```
┌─────────────────────────────────────────────────────────────────────────┐
│              LES 3 SOLUTIONS "NUMÉRO À VIE SANS ABONNEMENT"             │
│                                                                         │
│  1. LES CARTES SIM IOT "10 ANS / PAIEMENT UNIQUE" (Ex: 1NCE)            │
│     - 10 € payés UNE SEULE FOIS pour 10 ANS de validité                 │
│     - 500 Mo de data + 250 SMS inclus, valable dans 150 pays            │
│     - Tu la branches dans un dongle USB sur ton VPS et tu es tranquille │
│                                                                         │
│  2. LES NUMÉROS SIP PAY-AS-YOU-GO À RECHARGE UNIQUE (Ex: VoIP.ms/Telnyx)│
│     - Tu déposes 10 € UNE FOIS sur ton compte                           │
│     - Le numéro coûte ~0,40 $ / mois débité automatiquement du solde    │
│     - 10 € couvrent plus de 2 ans de fonctionnement sans prélèvement CB │
│                                                                         │
│  3. LES IDENTIFIANTS SIP DÉCENTRALISÉS WEB3 / BLOCKCHAIN (À VIE PUR)   │
│     - Achat d'un domaine ENS (`sip:gary.eth`) ou Silent.link en crypto  │
│     - Payé une fois à vie, infalsifiable, zéro abonnement, zéro KYC     │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## 🛠️ 4. Configuration Prête à l'Emploi d'Asterisk pour GAFAM

### Configuration du Trunk SIP (`/etc/asterisk/pjsip.conf`)
Exemple avec un numéro SIP ou Box Internet :

```ini
[global]
type=global
user_agent=GAFAM-Sovereign-PBX

[transport-udp]
type=transport
protocol=udp
bind=0.0.0.0:5060

[sip-provider-reg]
type=registration
outbound_auth=sip-auth
server_uri=sip:sip.fournisseur.com
client_uri=sip:MON_IDENTIFIANT@sip.fournisseur.com
retry_interval=60

[sip-auth]
auth_type=userpass
username=MON_IDENTIFIANT
password=MON_MOT_DE_PASSE_SECRET

[sip-aor]
type=aor
contact=sip:sip.fournisseur.com

[sip-endpoint]
type=endpoint
context=gafam-inbound
disallow=all
allow=alaw,ulaw,g722,opus
aors=sip-aor
outbound_auth=sip-auth
```

---

### Le Plan de Numérotation Intelligent (`/etc/asterisk/extensions.conf`)

```ini
[gafam-inbound]
exten => s,1,NoOp(--- Appel entrant sur le noeud GAFAM ---)
 same => n,Answer()                       ; Décroche immédiatement la ligne
 same => n,Wait(1)
 
 ; Joue le message d'accueil généré par TTS
 same => n(menu),Background(/var/lib/asterisk/sounds/gafam-welcome) 
 
 ; Attend que l'utilisateur tape une touche (1, 2, ou 3)
 same => n,WaitExten(5)

; --- TOUCHE 1 : Rapport Système & Missions ---
exten => 1,1,NoOp(Option 1 : Rapport Système)
 same => n,AGI(gafam_bridge.py, status)   ; Interroge l'API Go de GAFAM
 same => n,Playback(/tmp/status_response) ; Lit la réponse synthétisée
 same => n,Goto(s,menu)

; --- TOUCHE 2 : Écouter les 3 derniers SMS reçus ---
exten => 2,1,NoOp(Option 2 : Lecture SMS)
 same => n,AGI(gafam_bridge.py, read_sms)
 same => n,Playback(/tmp/sms_response)
 same => n,Goto(s,menu)

; --- TOUCHE 3 : Conversation vocale en direct avec l'Agent IA ---
exten => 3,1,NoOp(Option 3 : Agent Vocal IA Full-Duplex)
 same => n,Playback(/var/lib/asterisk/sounds/start-ai)
 same => n,Record(/tmp/user_voice.wav,3,20) ; Enregistre la voix
 same => n,AGI(gafam_voice_ai.py, /tmp/user_voice.wav) ; Whisper -> LLM -> Piper
 same => n,Playback(/tmp/ai_answer)        ; Diffuse la réponse de l'IA
 same => n,Goto(3,1)                       ; Reste dans la boucle de conversation !
```

---

## 🐍 5. Le Script Passerelle Go / Python (`gafam_voice_ai.py`)

```python
#!/usr/bin/env python3
import sys
import subprocess
import requests

audio_input = sys.argv[1] # Fichier WAV enregistré depuis l'appel GSM

# 1. Speech-To-Text local ultra-rapide (Whisper)
text_prompt = subprocess.check_output([
    "whisper-cli", "-m", "/models/whisper-tiny.bin", "-f", audio_input
]).decode('utf-8').strip()

# 2. Envoi du prompt à l'orchestrateur GAFAM (API Go vpc-relay)
res = requests.post("http://localhost:5150/api/web/llm/chat", json={
    "prompt": f"Tu es au téléphone avec Gary. Réponds de façon concise en 2 phrases max : {text_prompt}"
})
ai_reply_text = res.json().get("reply", "Je n'ai pas compris votre demande.")

# 3. Synthèse vocale instantanée (Piper TTS)
subprocess.run([
    "piper", "--model", "fr_FR-siwis-medium.onnx",
    "--output_file", "/tmp/ai_answer.wav"
], input=ai_reply_text.encode('utf-8'))
```
