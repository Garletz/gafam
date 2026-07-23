# 6. Topologie Réseau et Hébergement du Web Client

L'un des défis majeurs d'un réseau décentralisé est l'accessibilité. Comment rendre le système aussi simple à utiliser que Google pour un utilisateur lambda, tout en permettant aux utilisateurs avancés d'être totalement souverains sur leur hébergement ?

Pour résoudre ce paradoxe, le projet sépare strictement l'**Interface Graphique** (le Web Client) des **Données** (le VPC).

## Les 3 Couches du Projet
Pour bien comprendre, il faut séparer le système en 3 morceaux physiques et logiciels :
1. **La Couche Matérielle (Le Hardware Relay)** : Chez l'utilisateur. Intercepte les SMS/RCS via la carte SIM.
2. **La Couche Données (Le Serveur VPC / Go)** : Héberge la base de données SQLite et l'API. C'est le cerveau privé de l'utilisateur. **Peu importe le tuyau Internet** — ce qui compte, c'est que *toi* tu contrôles la machine (voir ci-dessous).
3. **La Couche Interface (Le Web Client / Svelte)** : C'est juste du code HTML/JS statique "vide". Son seul rôle est de s'afficher dans le navigateur et d'aller requêter la Couche Données pour afficher les messages.

---

## Où héberger la Couche Données (VPC)

Le nœud n'impose pas un datacenter terrestre. Quatre familles d'accueil — **même binaire Docker**, même port `5150`, même handshake avec le portail. Ce qui change, c'est *où* tourne la machine :

| Accueil | Exemple | Nature | Pour qui |
| :--- | :--- | :--- | :--- |
| **Provider cloud terrestre** | DigitalOcean, Hetzner, OVH… | VPS dans un datacenter | Clé en main, 1 Go suffit pour démarrer |
| **Maison / VPS perso derrière box** | Mini-PC, NUC, laptop derrière la box fibre | Auto-hébergement local ; IP dynamique → **FreeDNS** (ou tunnel) | Souveraineté max, électricité chez soi |
| **Cloud orbital (satellites)** | VPS *dans* la constellation (ex. compute Starlink / SpaceX et successeurs) | Le serveur **vit en orbite** — ce n'est pas un dish chez toi qui relie un PC au sol, c'est le cloud lui-même qui est spatial | Nœud hors juridiction territoriale unique, latence globale plus homogène, continuité quand le sol brûle ou se coupe |
| **Lien satellite → machine au sol** *(autre chose)* | Dish Starlink → routeur → Docker chez soi | Tuyau Internet seulement ; le VPC reste terrestre | Nomades / rural — distinct du cloud orbital |

> *Le VPC est un cerveau, pas une adresse IP terrestre. Provider, box, ou satellite : le contrat est le même — le disque et les clés restent à toi. Le cloud orbital n'est pas « Internet Starlink » : c'est **héberger le nœud dans l'espace**.*

**Cloud orbital — vision :** demain des providers vendront des VPS sur des racks en LEO (SpaceX et d'autres). Un nœud GAFAM doit pouvoir s'y poser sans rewrite : Docker, SQLite, TLS self-signed, annuaire qui pointe vers l'endpoint orbital. Lien naturel avec le [canal DTN / sub-Internet](7_dtn_subnetwork_secret.md) : quand le lien est intermittent, le nœud spatial est déjà pensé pour l'asynchrone.

**Aujourd'hui :** provider + maison. **Bientôt :** cocher « orbital VPS » dans le même tableau de déploiement — pas un nouveau produit, juste un nouvel *endroit*.

---

## Le Portail Central (L'Annuaire de Routage)
Pour simplifier l'accès, le projet dispose d'une page d'accueil centrale (ex: `gafam.com`).
Cette page est minimaliste : un simple champ de recherche où l'on tape son **numéro de téléphone**.

Ce portail central **ne stocke aucun message**. C'est un simple annuaire d'aiguillage. Quand un utilisateur tape son numéro, l'annuaire regarde dans sa table de routage et le redirige vers **son** Interface Web personnelle.

---

## Les 2 Pistes d'Hébergement de l'Interface (Web Client)

Pour gérer cette redirection, le système propose deux modes selon le niveau technique de l'utilisateur :

### Piste A : Le Mode "Clé en Main" (Pour les proches)
L'utilisateur veut juste que ça marche sans rien configurer.
- **Hébergement de l'Interface** : Le code Svelte statique est hébergé par le Portail Central (ou sur des CDN gratuits comme Vercel/Cloudflare).
- **Redirection** : Quand il tape son numéro, l'annuaire le redirige vers `https://06XX.gafam.com`.
- **Fonctionnement** : La page web s'ouvre, elle est vide, mais le code Javascript sait qu'il doit se connecter discrètement à l'IP du VPC privé de cet utilisateur pour charger les données.
- **Avantage** : Zéro configuration DNS pour l'utilisateur.

### Piste B : Le Mode "Souverain" (Custom Domain & FreeDNS)
L'utilisateur avancé refuse que son interface web soit servie par l'infrastructure centrale. Il héberge tout chez lui.
- **Hébergement de l'Interface** : L'utilisateur héberge son propre code HTML/JS sur son propre domaine (ex: `https://sms.gary.com`).
- **Redirection** : Sur l'annuaire central, le numéro est configuré avec un *Custom Domain*. Quand il tape son numéro sur l'accueil, il est immédiatement redirigé (HTTP 302) vers `https://sms.gary.com`.
- **Gestion des IP Dynamiques (Serveur Maison)** : Si cet utilisateur héberge aussi sa **Couche Données (VPC)** sur un ordinateur chez lui avec une connexion Internet grand public (dont l'IP change tous les jours), il utilise un service comme **FreeDNS (afraid.org)**. Le VPC met à jour l'IP en temps réel (ex: `gary-vpc.mooo.com`). Son Interface Web pointera toujours vers ce sous-domaine gratuit pour récupérer les messages, sans jamais perdre la connexion.
- **Avantage** : Indépendance totale. Même si le Portail Central explose, l'utilisateur a son propre domaine et son propre serveur DNS dynamique.

---

## Résumé
L'annuaire central permet une expérience "Google-like" ultra fluide pour démarrer, mais l'architecture permet à chaque nœud du réseau d'extraire et de s'auto-héberger (Frontend statique + Backend dynamique) à n'importe quel moment — **sur un Droplet terrestre, derrière une box, ou demain sur un VPS en orbite**.
