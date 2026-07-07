# Limitations et Contraintes : Interception RCS et Sécurité Android

Ce document détaille les limites techniques, architecturales et éthiques concernant l'implémentation de systèmes de contournement pour la lecture des messages RCS (Rich Communication Services) sur Android.

## 1. Ce qui est structurellement impossible (Architecture Android)

Android est conçu avec un modèle de sécurité compartimenté (Sandboxing) qui impose des limites strictes aux applications tierces.

*   **Pas d'API Publique RCS :** Contrairement au protocole SMS (`android.provider.Telephony.SMS_RECEIVED`), Android ne fournit aucune API publique permettant à une application tierce d'écouter, de lire ou d'intercepter le flux de données RCS en transit. Le protocole est fermé et géré en exclusivité par l'application client (généralement Google Messages) et les Google Play Services.
*   **Chiffrement de Bout en Bout (E2EE) :** Les messages RCS échangés via Google Jibe sont chiffrés. Il est cryptographiquement impossible pour une application locale d'intercepter le trafic réseau pour lire le contenu des messages avant qu'ils ne soient déchiffrés par l'application cible.
*   **Isolement des Processus :** L'APK GAFAM Relay ne peut pas accéder directement à la mémoire ou à l'espace de stockage privé de l'application Google Messages pour y extraire des données en clair.

## 2. Ce qui ne sera pas implémenté (Contraintes Éthiques et de Sécurité)

Conformément aux directives de sécurité de l'IA, aucun code, plan d'architecture détaillée, ou assistance technique ne sera fourni pour développer des mécanismes de surveillance ou d'interception furtive. Cela inclut explicitement :

*   **Implémentation de `NotificationListenerService` à des fins d'espionnage :** Aucun code ne sera généré pour créer un service écoutant les notifications de Google Messages dans le but d'en extraire silencieusement le contenu RCS et de masquer la notification à l'utilisateur.
*   **Implémentation de `ContentObserver` furtifs :** Aucun script ou architecture ne sera fourni pour surveiller la base de données de téléphonie (`content://sms` ou `content://mms-sms/`) afin d'exfiltrer discrètement les messages RCS au moment de leur écriture par l'application par défaut.
*   **Détournement d'`AccessibilityService` :** Aucun code ne sera fourni pour créer un service d'accessibilité dont le but serait de "lire" l'écran de l'utilisateur à son insu lorsque l'application Google Messages est ouverte, pour en extraire le texte des bulles de discussion.
*   **Mécanismes de persistance et d'élévation de privilèges :** Aucune assistance ne sera donnée pour forcer ou tromper l'utilisateur afin qu'il accorde les permissions sensibles (comme l'accès aux notifications ou à l'accessibilité) nécessaires à ces méthodes de contournement.

## 3. Alternative Légitime

La seule méthode supportée pour interagir avec la messagerie sur Android reste l'utilisation des API standards documentées par Google :

*   **Gestion des SMS/MMS classiques :** Utilisation de `BroadcastReceiver` pour `SMS_RECEIVED` et `SMS_DELIVER` si l'application est configurée et explicitement choisie par l'utilisateur comme application SMS par défaut.
*   Cette méthode, déjà implémentée dans le projet, reste aveugle au protocole RCS par design.

-_- -> -_-
