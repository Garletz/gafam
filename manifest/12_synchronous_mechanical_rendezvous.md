# 12. Le Rendez-vous Synchrone Mécanique (Authentification Web 2.0)

## Le Besoin : Sécurité et Furtivité Absolues
L'objectif est d'authentifier le Web Client (navigateur) auprès du VPC de manière ultra-sécurisée sans jamais :
1. Exposer l'adresse IP du VPC en clair sur un serveur tiers (Cloudflare).
2. Permettre le "Directory Hijacking" (vol du token par un attaquant écoutant Cloudflare).
3. Obliger l'utilisateur à taper un mot de passe complexe ou scanner un QR code sur le Web Client (UX asymétrique type "agent secret").

## Le Concept : L'Action Physique comme Clé Cryptographique
L'idée repose sur la génération d'un "Challenge temporel et physique" par l'APK Android (ou un futur matériel dédié type "Galet"). Ce challenge doit être résolu par l'utilisateur sur l'interface Web à une **minute précise**, via une série d'**impulsions (clics)**.

---

## Déroulement Complet du Protocole

### Étape 1 : La Programmation du Challenge (APK ➡️ VPC)
1. L'utilisateur appuie sur le bouton **"Authorize Web Login"** sur l'APK Android.
2. L'APK génère aléatoirement un challenge composé de :
   - **Une heure précise** dans les prochaines minutes (ex: `18h36`).
   - **Un nombre d'impulsions** entre 1 et 8 (ex: `4 clics`).
3. L'APK affiche à l'écran : *"Rendez-vous à 18h36 — 4 impulsions"*.
4. L'APK envoie ce challenge au VPC via le tunnel sécurisé HTTPS + SNI Spoofing (Port 5151). L'APK ne contacte **JAMAIS** Cloudflare directement (furtivité opérateur).

### Étape 2 : Le VPC prépare le Coffre-Fort Chiffré (VPC ➡️ Cloudflare)
1. Le VPC génère un nouveau `sessionToken` de 64 caractères aléatoires.
2. Le VPC prépare le contenu du coffre : `{ vpcUrl: "http://IP:5150", sessionToken: "xxx..." }`.
3. Le VPC dérive une **clé AES-256 via PBKDF2** à partir de la combinaison `"1836-4"` (heure + clics) avec un sel aléatoire (`salt`) et **500 000 itérations** (chaque tentative de déchiffrement prend ~1 seconde).
4. Le VPC chiffre le contenu du coffre en **AES-256-GCM** avec cette clé dérivée.
5. Le VPC dépose sur Cloudflare : le coffre chiffré + le `salt` (non chiffré, nécessaire pour PBKDF2) + l'heure cible `1836` comme **clé d'accès** au guichet Cloudflare.

### Étape 3 : Le Bruit de Fond et les Pièges (OPSEC & Honeypots)
Pour masquer le moment exact du vrai dépôt et empoisonner les attaquants :
1. Le VPC génère automatiquement des **faux coffres (leurres)** à intervalles aléatoires tout au long de la journée (ex: toutes les 5 à 45 minutes).
2. Chaque faux coffre est **indiscernable** d'un vrai : il contient une fausse adresse IPv4 publique parfaitement crédible, un faux `sessionToken`, une fausse heure d'accès, et un faux nombre de clics. Le chiffrement AES et le sel sont identiques en structure.
3. **La Frustration de l'Attaquant :** Si un attaquant parvient miraculeusement à récupérer et déchiffrer un coffre, il trouvera une fausse IPv4 et un faux token. Il tentera de se connecter et échouera inévitablement. Même si par malchance extrême l'IPv4 correspond à un autre VPC GAFAM, le faux token sera immédiatement rejeté.
4. Le vrai dépôt se noie dans la masse des leurres, rendant toute analyse temporelle du trafic réseau impossible.

### Étape 4 : Le Guichetier Strict (Cloudflare D1 + Rate Limiting)
Cloudflare sert de "boîte aux lettres morte" ultra-protégée :
- L'API Cloudflare **exige l'heure exacte** (`1836`) en paramètre pour délivrer le coffre associé au numéro de téléphone.
- Un mécanisme de **Rate Limiting strict** est appliqué : max **3 tentatives erronées par tranche de 10 minutes** par IP/numéro. Au-delà, l'IP est temporairement bannie.
- Sur 1440 heures possibles dans une journée, un attaquant avec 3 essais a **0.2% de chance** de deviner la bonne heure. Et s'il tombe juste, il a de fortes chances de récupérer un faux coffre (leurre) plutôt que le vrai.

### Étape 5 : La Saisie et l'Attente (Web Client Svelte)
1. L'utilisateur ouvre la page Web GAFAM Relay sur n'importe quel ordinateur.
2. L'interface affiche : *"Veuillez entrer l'heure prévue pour le challenge."*
3. L'utilisateur tape l'heure affichée par son téléphone (`18:36`).
4. Le Web Client interroge Cloudflare avec l'heure `1836` + le numéro de téléphone. Si l'heure est correcte, Cloudflare donne le coffre chiffré + le sel.
5. **Suppression immédiate :** Le coffre est aussitôt supprimé de Cloudflare D1 (Ephemeral Token). Personne d'autre ne pourra le récupérer.
6. L'interface bascule en mode attente : *"En attente de 18h36..."* avec un compte à rebours visuel.

### Étape 6 : L'Exécution du Challenge (Web Client)
1. Dès que l'horloge locale de l'ordinateur passe à **18:36 pile**, un gros **bouton d'action** apparaît avec un **compte à rebours de 30 secondes**.
2. L'utilisateur effectue les **4 impulsions** (clics) sur le bouton.
3. Le code Javascript du navigateur prend l'heure entrée (`1836`) et le nombre de clics (`4`), et fabrique la clé `"1836-4"`. Il la passe dans **PBKDF2** avec le sel récupéré du coffre pour dériver la clé AES-256.
4. Le navigateur tente de déchiffrer le coffre localement :
   - *En cas d'erreur de clics* : le déchiffrement AES-GCM échoue (tag d'authentification invalide). Le navigateur affiche "Challenge échoué".
   - *En cas de succès* : le navigateur obtient l'adresse IP cachée du VPC (`vpcUrl`) et le `sessionToken` en clair !
5. Le navigateur se connecte au VPC (Port 5150) avec le token valide. L'interface de messagerie s'affiche.

---

## Analyse de Sécurité et Patches Appliqués

### Critique N°1 : Cloudflare connaît l'heure (moitié de la clé AES)
**Le Risque :** L'heure `1836` est envoyée à Cloudflare comme clé d'accès. Si Cloudflare est compromis, l'attaquant connaît l'heure et n'a que 8 valeurs de clics à tester.
**L'Atténuation (multicouche) :**
- Les **Honeypots** font que l'attaquant ne sait pas s'il a le vrai coffre ou un leurre. S'il y a 20 leurres et 1 vrai, il a 1/21 de chance d'avoir le bon.
- Le **PBKDF2 à 500 000 itérations** rend chaque tentative de déchiffrement lente (~1 seconde). Les 8 valeurs de clics prennent 8 secondes, mais il doit le faire sur chacun des 21 coffres potentiels = ~168 secondes.
- Le **TTL de 5 minutes** côté VPC fait que le token expire rapidement, réduisant la fenêtre d'exploitation.
- **Probabilité combinée de réussite pour un attaquant contre Cloudflare compromis :** `0.2% (rate limiting) × 1/21 (honeypots) × 5min (TTL)` = quasi-nulle.

### Critique N°2 : Cloudflare voit l'IP du VPC (headers HTTP)
**Le Risque :** Quand le VPC dépose le coffre, Cloudflare voit son IP source via `cf-connecting-ip`.
**L'Atténuation :** Limitation acceptée et documentée. Cloudflare ne sait pas à quel utilisateur correspond cette IP, ne peut pas distinguer les vrais dépôts des leurres, et l'IP seule sans token est inutile.

### Critique N°3 (Patch Appliqué) : Pas de TTL côté VPC
**Le Risque :** Sans expiration, un token volé reste valide indéfiniment.
**Le Patch :** Le VPC ajoute un **Time-To-Live de 5 minutes** sur chaque `sessionToken`. Le `sessionMiddleware` en Go vérifie :
```sql
WHERE session_token = ? AND status = 'confirmed'
AND device_confirmed_at > datetime('now', '-5 minutes')
```
Si le token a plus de 5 minutes, le VPC le rejette. Même un brute-force réussi après ce délai ne donnera rien.

### Critique N°4 (Patch Appliqué) : Renforcement de la clé AES via PBKDF2
**Le Risque :** Sans PBKDF2, les 8 valeurs possibles de clics sont testables en microsecondes.
**Le Patch :** La clé AES est dérivée via `PBKDF2(passphrase="1836-4", salt=random, iterations=500000)`. Chaque tentative de déchiffrement prend ~1 seconde, rendant le brute-force significativement plus lent.

---

## Synthèse des Failles Bloquées

| Faille Théorique | Mécanismes de Blocage Combinés |
| :--- | :--- |
| **Directory Hijacking** (Écoute de l'API Cloudflare) | AES-GCM + PBKDF2 + Ephemeral Token (suppression après 1ère lecture) |
| **Offline Brute-Force** (Crackage du coffre par un Bot) | Rate Limiting (3 essais) + Honeypots (1 vrai parmi N faux) + PBKDF2 (1 sec/essai) + TTL 5 min |
| **Cloudflare Compromis** (Accès interne) | Honeypots indiscernables + PBKDF2 + TTL 5 min |
| **Traffic Analysis** (Observation du trafic VPC→Cloudflare) | OPSEC : faux coffres envoyés en continu, le vrai se noie dans le bruit |
| **Décalage de Fuseau Horaire** | L'utilisateur tape l'heure cible. Le navigateur utilise son horloge locale pour déclencher le bouton. |
| **Man In The Middle (MITM)** | Tout le déchiffrement est local (navigateur). La clé AES ne transite jamais sur le réseau. |
| **DPI par l'Opérateur Mobile** | L'APK ne contacte JAMAIS Cloudflare. Furtivité absolue (SNI Spoofing) derrière le VPC. |
