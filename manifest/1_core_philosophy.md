# 1. Philosophie Globale & Souveraineté

Le projet GAFAM Relay n'a pas pour vocation de devenir un énième service commercial centralisé. Sa philosophie absolue repose sur **l'auto-hébergement (Self-Hosting)** et la **Souveraineté Numérique**.

Chaque utilisateur est un îlot indépendant. Il possède son propre serveur cloud (VPC), son propre boîtier matériel (Relay) et son propre client web. Personne ne possède le réseau dans sa globalité, et aucune entreprise centrale ne peut lire ou intercepter les messages.

## L'Architecture en 3 Piliers

### A. Le Hardware Relay (La Passerelle Physique)
Un boîtier minimaliste (format "galet") qui remplace totalement le smartphone Android.
- **Fonction** : Héberge la carte SIM de l'utilisateur. Il agit comme un routeur/passerelle matériel physique chez l'utilisateur.
- **Sécurité** : Il empêche l'OS Android ou Google d'interférer avec le réseau cellulaire. Il intercepte les SMS en force brute matérielle et gère la cryptographie.

### B. Le VPC (Le Cerveau Cloud Personnel)
Chaque utilisateur déploie son propre Serveur Privé Virtuel (ex: un Droplet DigitalOcean).
- **Fonction** : Il fait le pont entre le boîtier matériel (Relay) et l'interface utilisateur (Client Web). Il stocke la base de données privée.
- **Liberté** : L'utilisateur est libre de coder, de customiser ou d'utiliser l'interface web (Front-End) de son choix. Le VPC s'occupe uniquement du routage en arrière-plan.

### C. Le Client Web (L'Interface Utilisateur)
Le "Front-End". C'est là que l'utilisateur lit et écrit ses messages. Il peut s'agir d'une web-app moderne, d'une application terminal, ou d'une application bureau, connectée de manière chiffrée au VPC de l'utilisateur.
