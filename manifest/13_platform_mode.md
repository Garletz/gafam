# 13. Platform Mode & Social Recovery

## 1. Vision Globale
L'application GAFAM intègre un **"Platform Mode"** (Mode Plateforme) qui sert de centre de contrôle névralgique et visuel pour l'utilisateur. Au lieu de se limiter à une simple liste de paramètres textuels, la gestion des accès, de la sécurité et du *Social Recovery* s'effectue dans un espace 3D infini et interactif.

Ce mode est accessible depuis l'interface principale (la page des chats/contacts) via une icône dédiée située en haut à gauche, à côté de l'indicateur du nombre de profils connectés. Cette icône (un rouage vectoriel ou un nuage de règles en points 3D) remplace visuellement l'interface de messagerie par la Plateforme 3D.

## 2. Esthétique & Navigation 3D
- **Environnement** : La plateforme s'étend de manière infinie vers le haut et les côtés.
- **Perspective** : L'arête inférieure de la page est visible pour accentuer l'effet de profondeur et l'inclinaison spatiale.
- **Déplacement** : L'utilisateur peut se déplacer librement sur cette plateforme (pan, zoom) pour explorer ses différents composants.

## 3. Représentation Nodale (via XYFlow)
Le cœur de la plateforme repose sur une représentation graphique et interactive des entités du réseau (en utilisant des bibliothèques comme [XYFlow / Svelte Flow](https://github.com/xyflow/xyflow)).

### Le Centre Névralgique : Le Relais VPC
- Au centre de la vue se trouve le nœud principal absolu : le **VPC**.
- Le VPC est l'intermédiaire dans le cloud qui stocke l'état du réseau et les *device fingerprints* dans ses tables (ex: `connected_devices`).

### Le Pont Matériel : Galet ou Android APK
- Connecté de manière privilégiée au VPC se trouve le **Pont Matériel** (qui peut être soit un smartphone Android via l'APK, soit l'appareil physique dédié "Galet").
- Ce nœud représente l'ancrage dans le monde réel (réseau GSM, stockage de la clé maître). S'il est déconnecté, la plateforme l'indiquera visuellement (lien rompu).

### Les Nœuds Périphériques : Les Clients Web
- Autour du VPC gravitent les cartes (nœuds) représentant chaque navigateur Web actuellement autorisé et connecté.
- **Actions directes** : Depuis ces nœuds, l'utilisateur peut auditer une session, déconnecter un client, ou révoquer son accès de manière permanente (bannissement du fingerprint au niveau du VPC).

## 4. Intégration du Social Recovery (Manifeste 5)
C'est sur cette même plateforme que le mécanisme de **Social Recovery** prend tout son sens. 
- L'utilisateur peut ajouter de nouveaux nœuds de type "Contact de Confiance".
- Ces nœuds s'attachent au VPC et définissent les numéros de téléphone autorisés à déclencher une procédure de récupération d'urgence (révocation de tous les autres accès, réinitialisation du Galet, etc.).
- L'interface visuelle permet de comprendre instantanément qui a le droit de récupération et de modifier ces permissions par simple glisser-déposer ou clic sur le nœud.

## 5. Topologie Visuelle Globale (La Carte)
L'agencement des nœuds sur la plateforme 3D suit une cartographie très précise pour que l'utilisateur comprenne instantanément la structure de son réseau :

- **En bas au centre** : Le **Galet** (ou le smartphone Android avec l'APK). Il représente l'ancrage physique (la racine de l'arbre).
- **Au-dessus (Centre)** : Le **VPC Relay de l'utilisateur**. C'est le moyeu cloud principal, relié directement au Galet en dessous.
- **À droite du VPC** : Les **Clients Web Personnels** connectés. C'est l'espace de gestion des sessions utilisateurs (pour révoquer l'accès d'un navigateur).
- **À gauche du VPC** : Les **VPC Amis (Fédération)**. Des connexions directes vers d'autres VPC de confiance pour des échanges chiffrés ultra-rapides, sans repasser par le réseau SMS.
- **À gauche du Galet** : Les **Numéros de Confiance (Social Recovery)**. Représentation des numéros de téléphone autorisés à déclencher la procédure de récupération d'urgence, accompagnés des "codes" secrets nécessaires à cette action.

## 6. Synthèse Technique
1. **Frontend Svelte** : Importation du dossier `frontend-old` contenant les bases de la plateforme 3D/XYFlow vers la nouvelle architecture Svelte 5.
2. **Backend VPC** : Création d'une table SQLite `device_fingerprints` (pour les clients à droite) et `trusted_recovery_contacts` (pour le recovery en bas à gauche).
3. **API Proxy** : Mise en place d'une route API sécurisée pour transmettre cette topologie réseau complexe (nœuds et liens) de Go vers Svelte Flow / 3D CSS.
