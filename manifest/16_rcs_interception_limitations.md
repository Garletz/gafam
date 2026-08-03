# 16. RCS : Limites d'interception, contournements légitimes, et la voie du remplacement

Ce document clarifie trois choses : pourquoi le RCS ne peut pas être intercepté comme un SMS, ce que GAFAM fait légitimement sur le téléphone relai de l'utilisateur, et pourquoi la réponse stratégique au RCS n'est pas l'interception mais le remplacement.

## 1. Le constat technique : RCS est une forteresse fermée

L'interception du RCS par une application tierce est **structurellement impossible** sur Android, indépendamment de l'ingéniosité déployée :

* **Aucune API publique.** Contrairement au SMS (`SMS_RECEIVED`, `SMS_DELIVER`, `content://sms`), Android n'expose aucun broadcast ni aucun provider pour le RCS. Les messages RCS n'apparaissent jamais dans `content://sms` ni `content://mms` — ils vivent dans la base privée de Google Messages.
* **Privilèges opérateur.** Le RCS repose sur l'enregistrement IMS (SIP/MSRP) auprès du réseau mobile. Cet enregistrement exige des privilèges "carrier" réservés aux applications préinstallées ou signées par l'opérateur ou Google. Une application tierce ne peut pas s'enregistrer IMS sur la quasi-totalité des appareils.
* **Infrastructure Jibe + E2EE.** La majorité du RCS mondial transite par les serveurs Jibe de Google, et le trafic entre utilisateurs Google Messages est chiffré de bout en bout (migration vers MLS en cours). Même une capture réseau ne donnerait que du chiffré.
* **Un seul client par SIM.** L'enregistrement IMS est exclusif : implémenter le protocole soi-même entrerait en conflit avec le client existant, en plus de représenter un effort démesuré.

Conclusion : le RCS n'est pas une cible d'interception. C'est un protocole fermé, sur un serveur fermé, avec un client fermé. Le contourner est une impasse.

## 2. Ce que GAFAM fait légitimement sur le téléphone relai

Le téléphone relai appartient à l'utilisateur, transporte les messages de l'utilisateur, et toutes les permissions sensibles sont accordées explicitement par l'utilisateur dans l'interface de l'APK. Dans ce cadre — et uniquement dans ce cadre — les mécanismes suivants sont légitimes et cohérents avec le reste du projet :

* **Lecture des notifications (`NotificationListenerService`).** Le projet utilise déjà ce mécanisme pour Gmail/Outlook (`EmailNotificationListener.kt`). Le même service peut lire les notifications de Google Messages : expéditeur et texte des messages RCS entrants sont ainsi relayés au VPC (les pièces jointes RCS n'y figurent pas — une photo apparaît comme "📷 Photo"). Les notifications ne sont jamais masquées à l'utilisateur.
* **Réponses via `RemoteInput`.** Les notifications de messagerie exposent une action "Répondre". L'APK peut y injecter une réponse, ce qui permet d'envoyer un message RCS sortant à travers le client officiel, sans contourner son chiffrement.
* **Fallback SMS par désactivation du RCS.** Le relai n'a pas besoin du RCS : il a besoin que les messages soient lisibles. Désactiver les "fonctionnalités de chat" dans Google Messages sur le téléphone relai fait basculer automatiquement tous les correspondants en SMS/MMS classiques (le fallback est natif côté expéditeur). L'intégralité du trafic redevient alors interceptable par l'infrastructure existante.

Ces mécanismes sont des relais de première personne, documentés et visibles dans l'interface de l'APK — à l'image du relais SMS qui est la fonctionnalité fondatrice du projet.

## 3. La réponse stratégique : ne pas intercepter RCS, le remplacer

RCS/Jibe et GAFAM occupent exactement la même case conceptuelle — à ceci près que l'un est fermé et l'autre souverain :

| | RCS / Google | GAFAM |
|---|---|---|
| Identité | Numéro de téléphone | Numéro de téléphone |
| Serveur de routage | Jibe (Google) | Le VPC de l'utilisateur |
| Client | Google Messages | Client web + APK relai |
| Chiffrement | E2EE opa que, clés gérées par Google | AES-256-GCM + Ed25519, clés gérées par l'utilisateur |
| Fédération | Aucune (Jibe centralise) | VPC ↔ VPC (manifest 17, Poneglyph) |
| Fallback réseau | SMS/MMS | SMS/MMS |

Le manifest 2 décrit déjà le routing intelligent : le VPC d'Alice découvre si le numéro de Bob est fédéré ; si oui, tunnel chiffré direct VPC↔VPC ; sinon, fallback vers la carte SIM du relai. C'est précisément la sémantique du RCS ("data si possible, SMS sinon") — sans Jibe, sans Google, sans intermédiaire autre que les VPC des pairs.

À mesure que le réseau de nœuds grandit, la part du trafic routée en fédération augmente et la part dépendante des opérateurs et de Google diminue mécaniquement. Le RCS ne se pirate pas : il se rend obsolète, nœud après nœud.

## Références croisées

* [2. Le Protocole Fédéré (Messagerie)](2_federated_messaging.md) — le routing discovery → tunnel P2P → fallback SMS.
* [4. Le Handshake GAFAM](4_anti_spam_handshake.md) — le filtre zero-trust à l'entrée des messages.
* [17. Le Canal Poneglyph](17_poneglyph_conjugation_channel.md) — la fédération par publication souveraine entre VPC.

-_- -> -_-
