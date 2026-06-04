# 11. Architecture Réseau : Dual-Binding, SNI Spoofing & Zero-Touch

## Le Besoin : Furtivité Absolue face aux Opérateurs (DPI)
L'objectif est d'empêcher les opérateurs mobiles:
1. Lire le contenu des SMS envoyés au VPC.
2. Détecter que l'application Android communique avec un serveur privé d'API (via Deep Packet Inspection).
3. Scanner et lister les adresses IPv4 des utilisateurs de GAFAM Relay.

Pour cela, le VPC abandonne le concept de serveur VPN lourd (qui nécessiterait des pop-ups d'autorisation Android) pour adopter une architecture "Ninja" à multiples portes (Dual-Binding) couplée à des techniques de camouflage de pointe.

## L'Architecture "Dual-Binding" (Les 3 Portes)

Le VPC expose simultanément plusieurs ports, chacun ayant un protocole et une cible spécifique :

### 1. La Porte Android (Port 5151) : HTTPS + SNI Spoofing
C'est le lien exclusif entre le téléphone (l'antenne) et le VPC.
- **Le Protocole :** HTTPS via un Certificat TLS Auto-Signé (généré lors du déploiement).
- **Le Camouflage (SNI Spoofing) :** Pour empêcher l'opérateur de bloquer les requêtes vers une IP "nue" et pour masquer l'identité du serveur, l'APK Android va falsifier l'en-tête TLS (Server Name Indication). Le téléphone fera croire à l'opérateur qu'il se connecte à `google.com` ou `whatsapp.net`, alors que la requête est physiquement routée vers l'IP du VPC.
- **La Sécurité :** L'APK utilise le *Certificate Pinning* : il ne fera confiance qu'au certificat exact du VPC (transmis via le QR Code), rendant toute attaque de type "Man in the Middle" impossible.

### 2. La Porte Web Client (Port 5150) : HTTP Clair + AES E2E
C'est le lien exclusif entre Cloudflare (qui héberge le Web Client Svelte) et le VPC.
- **La Contrainte :** Cloudflare refuse d'établir des connexions TCP vers des serveurs utilisant des certificats TLS auto-signés.
- **La Solution :** Cloudflare se connecte en HTTP clair sur le port 5150.
- **La Sécurité (Le Paradoxe) :** Bien que le transport soit en clair, la sécurité est absolue. Le Web Client Svelte chiffre la donnée en AES-256-GCM *avant* qu'elle ne quitte le navigateur. Cloudflare et les routeurs Internet ne transportent qu'une bouillie cryptographique.

### 3. La Porte Fédération (Port 5152) : mTLS (V2)
Port réservé exclusivement aux communications entre plusieurs VPC GAFAM (ex: pour échanger des empreintes anti-spam sans passer par le réseau SMS). Il utilisera du Mutual TLS (les deux serveurs s'authentifient mutuellement avec leurs certificats).

## La Faille du Web Login : Le Patch "PIN Code" (Zero-State)

**Le Problème Historique :** Lors du pairing Web ↔ VPC, l'envoi du `session_token` au répertoire Cloudflare pouvait être intercepté par un attaquant qui pollait la base de données au même moment (Directory Hijacking).

**La Solution :**
1. Le navigateur Web Client (Svelte) génère un **Code PIN à 4 chiffres** aléatoire et l'affiche à l'écran.
2. L'utilisateur tape ce PIN sur l'application Android lorsqu'il clique sur "Authorize Web Login".
3. L'APK Android chiffre le `session_token` en AES avec ce Code PIN.
4. Le répertoire Cloudflare stocke le token chiffré.
5. Seul le navigateur web qui a généré le PIN original peut déchiffrer le token et s'authentifier auprès du VPC.

Cette architecture garantit la souveraineté, la furtivité face aux opérateurs (via le SNI Spoofing), et la sécurité d'accès web (via l'AES E2E et le PIN Code), sans jamais dépendre de noms de domaine ou de services tiers propriétaires.
