# 4. Le Handshake GAFAM (Filtre Anti-Spam Absolu)

Grâce à l'architecture du VPC (qui intercepte 100% des messages avant de les afficher), le système possède une capacité native à résoudre l'un des pires fléaux modernes : le spam téléphonique (démarchage, robots, arnaques, etc.).

## Le Concept : "Zero-Trust Messaging"
Au lieu de subir les messages de tout le monde par défaut, l'architecture GAFAM Relay inverse la logique : **personne ne peut atteindre votre attention sans votre accord explicite.**

L'interface web sépare les messages en deux flux totalement distincts :
1. **La "Main Inbox" (Boîte Principale)** : Réservée aux contacts validés (la famille, les amis) et aux échanges P2P sécurisés approuvés.
2. **Le "Purgatoire" (Boîte d'Attente / Unknown)** : Réservé aux numéros inconnus, aux robots de spam, mais aussi aux SMS utilitaires (codes de banques, livreurs).

## Comment fonctionne le filtre de validation ?

### A. Via le Protocole Fédéré (Entre utilisateurs GAFAM Relay)
Si un utilisateur A veut parler à un utilisateur B sur le réseau fédéré :
1. Le VPC de A envoie une "Requête d'Invitation" (Handshake Request) chiffrée.
2. B reçoit une petite notification discrète : *"Le numéro 06XX souhaite établir une connexion sécurisée avec vous."*
3. Tant que B n'a pas cliqué sur "Accepter", le canal P2P n'est pas autorisé à relayer des messages ou des appels. Le spam robotisé sur le réseau fédéré devient mathématiquement impossible car il requiert une interaction humaine d'approbation asymétrique.

### B. Via le Réseau SMS/RCS Classique (Le monde extérieur)
1. Le boîtier matériel intercepte un SMS entrant en provenance du réseau de l'opérateur.
2. Le VPC vérifie si le numéro expéditeur existe dans la liste blanche (Contacts validés par l'utilisateur).
3. **S'il est inconnu**, le message est intercepté, inséré dans la base SQL du VPC, et étiqueté "Purgatoire". Le client web ne fait aucune notification sonore ou visuelle invasive.
4. L'utilisateur consulte ce Purgatoire uniquement lorsqu'il attend un code OTP ou un message d'un livreur Amazon. En un clic, il peut "Promouvoir" un numéro dans la Main Inbox, ou le supprimer sans jamais avoir été dérangé.

## La Puissance du Modèle
Actuellement, Apple, Google et les opérateurs tentent de contrer le spam en analysant le texte de milliards de messages (Machine Learning) ou en maintenant des "Blacklists" infinies de numéros frauduleux. 
Avec GAFAM Relay, le problème est réglé à la racine : ce n'est pas l'intelligence artificielle qui filtre, c'est **la ségrégation du réseau (Zero-Trust)** rendue possible par le fait que l'utilisateur héberge et contrôle sa propre base de données.
