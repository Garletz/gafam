# Réflexion : Stack Technique pour gafam.cloud

## Nos Besoins Réels (Pas Plus, Pas Moins)

### Ce que fait gafam.cloud :
1. **Page d'accueil** : Un champ de saisie de numéro de téléphone → redirige vers `num.gafam.cloud`
2. **Page utilisateur (`num.gafam.cloud`)** :
   - Écran de login (le handshake bouton APK + bouton Web)
   - Une fois loggué : affiche les SMS depuis le VPC Go de l'utilisateur
   - Potentiellement : envoyer des messages, gérer ses contacts, configurer son compte

### Ce que gafam.cloud ne fait PAS :
- Stocker les messages (c'est le VPC Go privé de chaque user qui fait ça)
- Gérer l'authentification lourde (c'est le VPC + l'APK)
- Gérer les SMS eux-mêmes (c'est le Hardware Relay / APK)

### Donc gafam.cloud est essentiellement :
- Un **routeur d'URLs** (numéro → sous-domaine)
- Un **serveur de fichiers statiques** (le code HTML/JS/CSS du client web)
- Un **annuaire léger** (quelle IP de VPC correspond à quel numéro)

---

## Les Stacks Disponibles sur Cloudflare

### 1. Cloudflare Pages (Static) + Workers (Edge Functions)
- **Ce que c'est** : Hébergement de fichiers statiques (HTML/JS/CSS) + petites fonctions JavaScript serverless sur le Edge
- **Avantages** :
  - Gratuit (100K requêtes/jour sur le plan Free)
  - Extrêmement rapide (edge network mondial)
  - Zéro maintenance serveur
  - Parfait pour le routing wildcard (`*.gafam.cloud`)
- **Inconvénients** :
  - Pas de framework côté serveur (tout est client-side ou edge functions)
  - Le Worker est limité à 10ms CPU sur le plan gratuit (mais suffisant pour du routing)

### 2. Cloudflare D1 (Base de données SQLite sur le Edge)
- **Ce que c'est** : Une base SQLite hébergée par Cloudflare, accessible depuis les Workers
- **Pertinence pour nous** : On pourrait y stocker l'annuaire de routage (numéro → IP du VPC). C'est très léger : une seule table avec quelques dizaines de lignes
- **Avantages** : Gratuit (5M lectures/jour, 100K écritures/jour), SQL natif
- **Alternative** : Cloudflare KV (Key-Value store). Plus simple encore pour notre cas (clé = numéro, valeur = URL du VPC). Gratuit aussi (100K lectures/jour)

### 3. Cloudflare R2 (Object Storage)
- **Ce que c'est** : Stockage de fichiers (comme AWS S3)
- **Pertinence pour nous** : Inutile pour le moment. On n'a pas de fichiers lourds à stocker sur la plateforme

---

## Les Frameworks Frontend Possibles

### Option A : HTML/CSS/JS Pur (Vanilla)
- **Avantages** : Aucune dépendance, ultra léger, déployable directement sur Pages
- **Inconvénients** : Pas de composants réutilisables, le code devient vite du spaghetti si l'interface se complexifie (contacts, messagerie, settings)
- **Verdict** : OK pour un MVP rapide, mauvais sur le long terme

### Option B : Svelte + Vite (SPA, Client-Side Rendering)
- **Ce que c'est** : Le même setup que ton `frontend/` existant. On build en statique, on déploie le `dist/` sur Cloudflare Pages
- **Avantages** :
  - Tu connais déjà Svelte (Manager macOS + frontend existant)
  - Build en fichiers statiques → parfait pour Cloudflare Pages
  - Composants réutilisables, réactivité native
  - Le frontend existant a déjà une base de composants (DeviceCard, PhoneCard, etc.)
- **Inconvénients** :
  - Pas de Server-Side Rendering (SEO limité, mais on s'en fout pour un client privé)
  - Le routing doit être géré côté client (hash routing ou history API)
- **Verdict** : ⭐ Le meilleur compromis. On réutilise ce que tu connais, on déploie en statique

### Option C : SvelteKit + adapter-cloudflare
- **Ce que c'est** : Framework full-stack Svelte avec SSR, routing intégré, et un adapter officiel Cloudflare
- **Avantages** :
  - Routing propre (`/[phone]/` natif)
  - SSR pour le SEO (inutile pour nous)
  - Accès direct à D1/KV depuis les server routes (pratique pour l'annuaire)
- **Inconvénients** :
  - Plus lourd à configurer
  - Overhead inutile pour notre cas d'usage simple
  - Le Worker SvelteKit consomme plus de CPU (risque de dépasser les 10ms sur le plan gratuit)
- **Verdict** : Overkill pour notre besoin

### Option D : Next.js / Nuxt / Remix sur Cloudflare
- **Verdict** : Non. Tu ne connais pas React/Vue. Pas de raison de changer

### Option E : Hono (micro-framework JS pour Workers)
- **Ce que c'est** : Un micro-framework JavaScript ultra-léger conçu spécifiquement pour Cloudflare Workers
- **Avantages** : Routing élégant, accès natif à D1/KV, très rapide
- **Inconvénients** : C'est un framework backend (API), pas frontend. Il faudrait quand même un framework front
- **Verdict** : Intéressant comme couche API devant D1/KV, mais ne remplace pas le frontend

---

## Ma Recommandation Finale

### Architecture en 2 couches sur Cloudflare :

**Couche 1 : Cloudflare Worker (Le Routeur)**
Un Worker JavaScript très léger qui gère :
- Le wildcard DNS (`*.gafam.cloud`)
- La résolution du numéro : lecture dans Cloudflare KV (clé = numéro, valeur = URL du VPC)
- Le routage : si c'est `gafam.cloud` → sert la page d'accueil. Si c'est `0611223344.gafam.cloud` → sert le client web

**Couche 2 : Svelte + Vite (Le Frontend)**
Le client web construit en Svelte (SPA), buildé en statique (`vite build`), et servi par le Worker.
- Page d'accueil : barre de recherche + pavé numérique
- Page utilisateur : handshake + dashboard SMS
- Toute la logique de communication avec le VPC Go est côté client (fetch API)

**Couche 3 (optionnelle) : Cloudflare KV ou D1**
Pour l'annuaire de routage : `numéro → { vpc_url, phone_label }`
- KV est plus simple (pas de SQL, juste clé-valeur)
- D1 est plus flexible si on veut ajouter des métadonnées plus tard

### Pourquoi ce choix ?
- On garde Svelte (que tu connais)
- On reste sur du statique (pas de SSR inutile)
- Le Worker fait le minimum vital (routing + KV lookup)
- Tout est gratuit sur le plan Free Cloudflare
- On peut réutiliser des composants du `frontend/` existant
