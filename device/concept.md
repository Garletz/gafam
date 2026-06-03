# GAFAM Hardware Relay (Concept)

## Vision
Le "GAFAM Hardware Relay" est un appareil physique minimaliste conçu pour remplacer totalement un smartphone Android dédié à l'interception et au relai de la messagerie. En supprimant l'OS Android et ses restrictions, cet appareil offre un contrôle cryptographique et réseau absolu.

## Design Conceptuel
- **Esthétique** : Format ultra-compact, inspiré d'un Apple AirTag. Un design épuré en aluminium ou polymère haute densité.
- **Interface Physique** : Un unique bouton central multifonction (allumage, reset, mode appairage).
- **Entrées/Sorties** :
  - Un tiroir (ou fente) pour carte nano-SIM.
  - Une micro-caméra intégrée et dissimulée dans le design, dédiée *exclusivement* au scan de QR codes.
  - Un port USB-C caché ou pogo pins pour la charge/diagnostic.
- **Indicateurs** : Une LED d'état discrète (clignotement selon le statut de connexion : réseau cellulaire, VPC, erreur).

## Architecture Matérielle (Brouillon)
- **SoC (System on Chip)** : Microcontrôleur basse consommation (ex: ESP32 ou puce IoT légère) capable de gérer la cryptographie et le routage réseau.
- **Modem Cellulaire** : Module LTE/5G M2M avec accès direct aux commandes AT pour parler à la carte SIM.
- **Module Caméra** : Capteur très basse résolution (VGA suffit) couplé à une puce de décodage QR code matérielle.
- **Alimentation** : Batterie Li-Po intégrée avec optimisation d'énergie pour une longue autonomie (l'appareil étant en veille la plupart du temps, attendant un signal réseau).

## Flux de Fonctionnement (Workflow)

### 1. Initialisation & Connectivité
- La carte SIM est insérée dans l'appareil.
- **Connexion Internet Primaire** : L'appareil utilise la data cellulaire de la carte SIM (4G/5G) pour accéder à Internet. Pas besoin de configuration Wi-Fi, il est autonome dès l'insertion de la SIM.

### 2. Authentification & Connexion au Client Web
L'appareil sert de clé d'authentification physique pour se connecter à l'interface web (depuis un ordinateur ou un autre smartphone). L'unique bouton central prend ici tout son sens.
Deux méthodes de connexion (2FA physique) sont possibles :
1. **Méthode Optique (Scan QR)** : Le client web affiche un QR code. L'utilisateur clique une fois sur le bouton du Hardware Relay pour réveiller la caméra et scanne l'écran. La connexion est approuvée cryptographiquement.
2. **Méthode Synchrone (Click Simultané)** : Si l'appareil est déjà relié au VPC, l'utilisateur clique sur "Se connecter" sur le client web, puis appuie sur le bouton physique de l'appareil dans la foulée. Le serveur VPC valide la concordance temporelle et autorise l'accès.

### 3. Remote Scan (App Compagnon "Tricky")
Si le Hardware Relay est rangé dans un tiroir ou branché derrière un meuble, il n'est pas pratique d'utiliser sa caméra intégrée.
- **Concept** : Une application mobile Android/iOS "Compagnon" très légère.
- **Fonctionnement** : L'app compagnon utilise la caméra du smartphone de l'utilisateur pour filmer le QR code, et envoie le *flux vidéo en direct* (ou juste l'image du QR) de manière sécurisée (Bluetooth BLE ou Wi-Fi Direct) au Hardware Relay. 
- **Magie** : Le Hardware Relay "croit" qu'il utilise sa propre caméra physique, analyse le flux reçu, et valide l'appairage. Le smartphone devient donc la caméra déportée du boîtier matériel !

### 4. Traitement des Messages (Routing Intelligent)
L'appareil agit comme un routeur de messagerie intelligent à 3 voies :

1. **RCS & SMS Standard (Vers l'Extérieur)** :
   - L'appareil parle directement au réseau de l'opérateur (via les commandes IMS/SIP pour le RCS, et GSM pour le SMS).
   - En contrôlant directement la SIM, il génère les tokens EAP-AKA requis par les serveurs Jibe/Opérateur pour le RCS.
   - Les messages reçus (RCS ou SMS) sont transmis instantanément au serveur VPC.

2. **Protocole Chiffré "Maison" (Entre Devices)** :
   - Lors de l'envoi d'un message, le Hardware Relay interroge le VPC : *Le destinataire possède-t-il aussi un GAFAM Relay ?*
   - Si OUI : Le message ne passe **jamais** par l'opérateur cellulaire. Il est chiffré de bout en bout (E2EE) par l'appareil source, transite par Internet (via le VPC ou en P2P), et est déchiffré uniquement par le Hardware Relay de destination.
   - Si NON : L'appareil effectue un "fallback" et expédie le message via le réseau de l'opérateur en mode RCS (si compatible) ou SMS.

## Avantages du Hardware vs Android APK
- **Bypass de Google** : Contournement total des restrictions Android concernant l'accès aux clés SIM et au réseau RCS.
- **Discrétion & Fiabilité** : Pas d'OS lourd, pas d'applications qui se ferment en arrière-plan pour économiser de la batterie.
- **Sécurité Absolue** : La surface d'attaque est nulle. C'est une "Black Box" dont le seul code exécuté est celui du relai.
- **Expérience Utilisateur** : Plug & Play. On insère la SIM, on scanne le QR code, on le range dans un tiroir et on l'oublie.
