# 5. Social Recovery & Web of Trust (Authentification sans appareil)

Que se passe-t-il si vous oubliez votre *Hardware Relay* chez vous, ou que vous le perdez, et que vous devez vous connecter urgemment à votre Client Web depuis l'ordinateur d'un ami ?

Grâce au réseau fédéré GAFAM Relay, le système intègre une mécanique de sécurité dérivée de la cryptographie avancée : le **Social Recovery** (Récupération Sociale) basé sur un réseau de confiance (Web of Trust).

## Le Concept : "Trusted Guardians" (Les Gardiens de Confiance)
Lors de la configuration de votre VPC, vous pouvez définir une liste de "Gardiens". Ce sont des numéros de téléphone d'amis proches ou de membres de votre famille qui possèdent également l'écosystème GAFAM Relay.

Ces personnes ne peuvent *absolument pas* lire vos messages, mais leurs VPC respectifs possèdent une fraction chiffrée de votre clé d'autorisation (selon un principe mathématique appelé le *Partage de secret de Shamir*, ou "Shamir's Secret Sharing").

## Le Flux d'Authentification (Le "Login d'Urgence")

Imaginez que vous êtes chez Alice (votre amie, qui est une de vos Gardiennes de Confiance) sans votre boîtier matériel :

1. Vous ouvrez le Client Web sur l'ordinateur d'Alice et tapez votre numéro de téléphone pour vous connecter.
2. Le Client Web affiche un QR Code de connexion et attend la validation matérielle.
3. Puisque vous n'avez pas votre appareil, vous cliquez sur le bouton **"Authentification via un Tiers de Confiance"**.
4. Vous sélectionnez "Alice" dans la liste de vos gardiens.
5. Alice reçoit immédiatement une notification sur son propre Client Web : *"Le numéro 06XX (Vous) demande une autorisation d'accès d'urgence à son VPC. Approuvez-vous qu'il se trouve physiquement avec vous ?"*
6. Alice valide la demande en appuyant sur le bouton physique de **son propre** *Hardware Relay*.
7. Le VPC d'Alice envoie un jeton cryptographique d'approbation à votre VPC via le tunnel réseau fédéré.
8. **Magie : Votre session web est autorisée et vous êtes connecté !**

## Pourquoi c'est brillant ?
- **Liberté matérielle totale** : Vous pouvez voyager léger, sans aucun appareil électronique (ni smartphone, ni boîtier), et quand même accéder à vos messages de n'importe où dans le monde, à condition de rencontrer un de vos nœuds de confiance.
- **Sécurité anti-hacker** : C'est une authentification à deux facteurs "Biologique et Sociale". Un hacker à l'autre bout du monde ne pourra jamais pirater ou simuler la validation physique asynchrone de vos amis.
- **Résilience (Perte/Vol)** : Si votre domicile brûle avec votre *Hardware Relay* à l'intérieur, vos amis peuvent valider l'association d'un tout nouveau boîtier matériel à votre VPC. C'est le niveau ultime de sécurité décentralisée.

## Feature Supplémentaire : Le "Ping Recovery" (via un numéro de confiance classique)

En plus des Gardiens équipés de GAFAM, vous pouvez définir un **"Numéro de Recovery"** classique dans votre VPC (souvent un proche). Ce proche **n'a pas besoin** d'utiliser le réseau GAFAM, il lui suffit d'avoir un téléphone standard capable d'envoyer des SMS.

**Le Mécanisme :**
1. Vous êtes à distance, sans votre téléphone ni votre boîtier matériel. Votre ami est avec vous avec son téléphone normal.
2. Vous lui demandez d'envoyer un mot-clé par SMS (par exemple le code par défaut `num.gafam.cloud`) à votre propre numéro de téléphone (votre téléphone relais resté chez vous).
3. Votre téléphone relais (à la maison) reçoit ce SMS. L'APK Android intercepte le message, analyse l'expéditeur et détecte qu'il s'agit de votre Numéro de Recovery autorisé.
4. En tâche de fond, l'APK demande à votre VPC de générer un coffre valide (avec l'heure et les impulsions requises).
5. L'APK répond alors automatiquement au téléphone de votre ami avec un SMS contenant les accès d'urgence : *"Code généré : Heure XX:XX, Impulsions X"*.
6. Votre proche reçoit ce SMS. Il vous le montre.
7. Vous vous connectez sur votre espace en ligne (`votre-numero.gafam.cloud`), vous rentrez le code (Heure + Impulsions) que votre ami vient de recevoir, et vous accédez à votre compte !