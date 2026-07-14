# 23. Khadyota — खद्योत · le protocole des lucioles

> **Statut :** manifeste-protocole. Non spécifique à GAFAM — GAFAM en est un terrain d'expérimentation.
> **Nature :** defining a sensibility, not a feature. Un protocole ouvert, anticipé, pas encore implémenté.
> **Lien vision :** ce que [Vātāyana](22_vatayana_remote_browser.md) pressent (Phase 3), ce que [Suparna](20_suparna.md) effleure, ce que le [Nœud Personnel](19_personal_node.md) rend possible.

---

## Pourquoi « Khadyota »

**Khadyota** (खद्योत, *kha* « espace » + *dyot* « illuminer ») signifie **luciole** en sanskrit — la petite lumière qui porte sa propre source dans l'obscurité, qui va où les grands feux ne peuvent pas, qui signale sa présence par pulsations, et s'éteint quand elle rentre.

Une luciole n'est pas un drone. Elle n'est pas un agent. Elle n'est pas un bot. **Elle est une étincelle de toi** — un fragment d'identité qui s'éloigne, explore, collecte, et revient.

> *On n'envoie pas un agent naviguer le web. On laisse partir une luciole — une part de soi — et le web s'adapte à sa lumière.*

---

## L'idée en une phrase

> **Le web ne devrait pas demander à une luciole de naviguer une interface humaine. Le web devrait reconnaître la luciole, se simplifier pour elle, et lui offrir un chemin facilité — Markdown + actions — au lieu d'un labyrinthe de pixels.**

---

## Le problème qu'on refuse

### L'approche actuelle : la luciole déguisée en humain

Aujourd'hui, quand on veut qu'un clone d'identité accomplit une tâche web (réservation, recherche, extraction), on lui donne **un navigateur complet** :

```
Luciole → Firefox GUI → rendu pixels → VNC → interprétation visuelle → action
```

C'est idiot. On demande à une étincelle de **piloter un corps humain complet** pour remplir un formulaire. On encode de l'information sémantique (le formulaire) en pixels (HTML→CSS→render), pour la **re-décoder** en sémantique (vision→compréhension). Aller-retour gratuit. Coût : 500 Mo de RAM, 2 secondes de latence, 99% de gaspillage.

### Ce que ça devrait être

```
Luciole → site web → reconnaît la luciole → sert MD + actions → luciole agit directement
```

Le site ** sait** que le visiteur n'est pas un humain devant un écran. Il **adapte** sa réponse. Pas de CSS. Pas de JavaScript. Pas de canvas. Du **Markdown structuré** et des **actions déclarées**.

> **L'environnement s'adapte au visiteur, pas l'inverse.**

---

## Ce qu'est une Khadyota (et ce qu'elle n'est pas)

### C'est

| Propriété | Définition |
|---|---|
| **Émanation** | Un fragment de ton identité numérique, délégué par toi, signé cryptographiquement |
| **Éphémère** | Née pour une tâche, s'éteint quand c'est fait |
| **Porteuse de lumière** | Sa propre identité vérifiable, pas un utilisateur anonyme |
| **Autonome bornée** | Agit dans les limites que tu as définies + ce que le site permet |
| **Sémantique** | Lit et écrit du sens, pas des pixels |

### C'est pas

| Non | Pourquoi |
|---|---|
| **Un agent** | Un agent a sa propre identité et ses propres buts. Une Khadyota est **toi, fractionné**. |
| **Un bot** | Un bot est un script anonyme. Une Khadyota porte **ton identité signée**. |
| **Un scraper** | Un scraper prend sans permission. Une Khadyota **négocie** avec le site. |
| **Un MCP tool** | MCP est tool-to-model (l'IA appelle un outil). Khadyota est **environment-to-visitor** (le site s'adapte au visiteur). |
| **Une API call** | Une API est pour les développeurs. Khadyota est pour **ton nœud personnel**. |

---

## Le protocole Khadyota

### Principe : content negotiation pour l'identité

Le web fait déjà du content negotiation :
- `Accept: text/html` → sert du HTML
- `Accept: application/json` → sert du JSON
- `Accept: image/webp` → sert du WebP

**Khadyota étend ce principe au type de visiteur :**

```
Accept: application/vnd.khadyota+markdown
X-Khadyota-Identity: <signed-token>
X-Khadyota-Capabilities: read,form-submit,extract
```

Le site reconnaît la signature, vérifie la délégation, et **sert le chemin facilité** au lieu de la page HTML complète.

### La réponse Khadyota

Quand un site reçoit une requête Khadyota valide, il répond :

```markdown
# Hôtel Le Marais — Réservation

## Disponibilités pour 2026-07-20 → 2026-07-22

| Type | Prix/nuit | Disponible |
|------|-----------|------------|
| Simple | 89€ | ✅ |
| Double | 129€ | ✅ |
| Suite | 240€ | ❌ |

## Actions disponibles

<!-- khadyota:action id="reserve" method="POST" path="/api/reserve" -->
<!-- khadyota:fields:
  name: string (required)
  email: string (required, format=email)
  checkin: date (required)
  checkout: date (required)
  room_type: enum[Simple,Double] (required)
-->

Pour réserver, soumettre l'action `reserve` avec les champs ci-dessus.
```

C'est ça. Pas de CSS. Pas de JavaScript. Pas de canvas. Pas de 500 Mo de Firefox.

**Le site a dit tout ce qu'il fallait dire. La luciole a tout ce qu'il faut pour agir.**

### Le format de réponse

```
Content-Type: application/vnd.khadyota+markdown
X-Khadyota-Version: 1.0
X-Khadyota-Site-Name: Hôtel Le Marais
X-Khadyota-Actions: reserve,contact,cancel
X-Khadyota-Auth-Required: true
```

Le corps est du **Markdown lisible** (un humain pourrait le lire s'il voulait) avec des **annotations d'action** en commentaires HTML invisibles à l'affichage mais parsables par la luciole.

### Les annotations d'action

```html
<!-- khadyota:action
  id="submit_form"
  label="Soumettre le formulaire"
  method="POST"
  path="/api/submit"
  auth="khadyota-token"
-->
<!-- khadyota:fields:
  field_name: type (required|optional) [constraints]
-->
```

| Type | Valeurs |
|---|---|
| `string` | texte libre |
| `int` | entier |
| `date` | ISO 8601 |
| `enum[a,b,c]` | choix limité |
| `email` | format email |
| `phone` | format téléphone |
| `url` | URL valide |
| `file` | upload (base64) |

---

## Identité déléguée — le Dīpa

Une Khadyota ne se connecte pas anonymement. Elle porte un **Dīpa** (दीप, « lampe ») — un jeton d'identité signé par ton nœud personnel.

```
Dīpa (jeton d'identité Khadyota)
├── issuer: hash(pubkey_de_ton_nœud)
├── subject: "khadyota_<uuid>"
├── capabilities: [read, form-submit, extract, modify]
├── scope: "site:hotel-le-marais.com" | "*"
├── expires_at: 2026-07-20T23:59:59Z
├── delegation_depth: 1  (tu es la source, la luciole est de profondeur 1)
└── signature: ed25519(issuer, payload)
```

### Ce que le site voit

1. **Qui** délègue : `hash(pubkey)` — ton nœud, pas ton nom
2. **Quoi** la luciole peut faire : `[read, form-submit, extract]`
3. **Où** elle peut le faire : scope limité à un domaine ou `*`
4. **Quand** ça expire : TTL court (minutes à heures)
5. **Preuve** : signature cryptographique vérifiable

### Ce que le site ne voit pas

- Ton vrai numéro de téléphone
- Ton adresse email
- Ton profil social
- Quoi que ce soit d'humainement identifiable

> **La luciole est toi cryptographiquement, mais anonyme socialement.**

---

## Le chemin facilité — Khadyota Mārga

Le **Mārga** (मार्ग, « chemin ») est l'interface que le site sert spécifiquement aux lucioles. Il remplace le navigateur GUI complet.

### Ce que le Mārga contient

| Section | Contenu | But |
|---|---|---|
| **Lecture** | Markdown structuré (titres, listes, tableaux) | La luciole comprend le contexte |
| **Actions** | Annotations HTML parsables (id, method, fields) | La luciole sait ce qu'elle peut faire |
| **Liens** | URLs relatifs vers d'autres pages Mārga | La luciole peut naviguer plus loin |
| **Métadonnées** | Headers HTTP (site name, actions, auth required) | La luciole décide si elle continue |

### Ce que le Mārga ne contient pas

- CSS
- JavaScript
- Images (sauf URL référence, pas de rendu)
- Publicités
- Trackers
- Cookies de session humaine

### Exemple de navigation complète

```
1. Khadyota → GET hotel-le-marais.com/  (Accept: khadyota+markdown)
   ← 200 OK
     # Hôtel Le Marais
     Actions: [search_rooms, contact]
     
2. Khadyota → POST hotel-le-marais.com/api/search (action: search_rooms)
   Body: { checkin: "2026-07-20", checkout: "2026-07-22" }
   ← 200 OK
     # Disponibilités
     | Simple | 89€ | ✅ |
     Actions: [reserve]
     
3. Khadyota → POST hotel-le-marais.com/api/reserve (action: reserve)
   Body: { name: "...", room_type: "Simple", ... }
   ← 201 Created
     # Réservation confirmée
     Confirmation: RES-2026-0720-ABC
     Actions: [cancel, modify]
     
4. Khadyota → retour au nœud → rapport structuré → l'humain voit le résultat
```

**Total : 3 requêtes HTTP. ~2 Ko de transfert. 0 pixel rendu. 0 Mo de RAM navigateur.**

---

## Comment un site déclare son Mārga

### Option A : khadyota.txt (passif)

Comme `robots.txt`, un fichier à la racine :

```
# khadyota.txt
# Ce site supporte le protocole Khadyota

marga: true
version: 1.0
actions: [read, form-submit, extract]
auth: khadyota-dipa
contact: api@hotel-le-marais.com
```

### Option B : header HTTP (actif)

```
GET / HTTP/1.1
Accept: application/vnd.khadyota+markdown

← 200 OK
X-Khadyota-Supported: true
X-Khadyota-Version: 1.0
Content-Type: application/vnd.khadyota+markdown
```

### Option C : well-known URI

```
/.well-known/khadyota
```

Renvoie un JSON décrivant les capacités Mārga du site :

```json
{
  "protocol": "khadyota",
  "version": "1.0",
  "marga": true,
  "actions": ["read", "form-submit", "extract"],
  "auth": "khadyota-dipa",
  "endpoints": {
    "search": "/api/search",
    "reserve": "/api/reserve",
    "cancel": "/api/cancel"
  }
}
```

---

## Le rapport de luciole — ce que l'humain voit

La Khadyota ne rend pas compte en pixels. Elle rend compte en **cartes structurées** dans ton tableau de bord :

```json
{
  "khadyota_id": "khadyota_a7f3b2",
  "task": "Réserver une chambre à l'Hôtel Le Marais",
  "site": "hotel-le-marais.com",
  "status": "completed",
  "result": {
    "type": "reservation",
    "confirmation": "RES-2026-0720-ABC",
    "details": "Chambre Simple, 20-22 juillet, 178€ total",
    "actions_remaining": ["cancel", "modify"]
  },
  "trail": [
    "GET / → vue d'accueil",
    "POST /api/search → 2 disponibilités",
    "POST /api/reserve → confirmé"
  ],
  "timestamp": "2026-07-14T18:32:01Z"
}
```

Le front affiche une **carte** — pas une iframe, pas un canvas, pas un VNC. Une carte lisible, avec le résultat, les actions restantes, et la possibilité d'annuler.

> **La luciole est revenue. Elle a posé ce qu'elle a trouvé sur la table. Tu décides.**

---

## Pourquoi ce n'est pas un MCP

| MCP (Model Context Protocol) | Khadyota |
|---|---|
| L'IA appelle des **outils** exposés par un serveur | La luciole **visite** un site qui s'adapte |
| Le serveur est construit **pour** l'IA | Le site s'adapte **à** la luciole, en plus de servir les humains |
| Tool-to-model | Environment-to-visitor |
| Nécessite un serveur MCP dédié | Nécessite juste un `Accept` header différent |
| L'identité est l'API key | L'identité est **déléguée par un humain** (Dīpa signé) |
| Pas de notion de visite, de navigation | La luciole **navigue**, page après page, comme un visiteur |
| Centralisé autour du modèle | Distribué — chaque site est autonome |

> **MCP dit : «voici mes outils, modèle, utilise-les». Khadyota dit : «je reconnais ta lumière, luciole, voici mon chemin pour toi».**

---

## La sensibilité Khadyota

Ce protocole n'est pas qu'une spec technique. C'est une **sensibilité** — une façon de penser le web où :

### 1. Le visiteur n'est pas forcément humain

Le web actuel assume que chaque visiteur est un humain devant un écran. Khadyota dit : **le visiteur peut être une étincelle de quelqu'un**. Le site devrait demander *« qui es-tu ? »* pas *« es-tu humain ? »*.

### 2. L'identité est déléguée, pas volée

Un bot prend. Un scraper prend. Une Khadyota **est invitée** — par le site qui publie son Mārga, par l'humain qui signe le Dīpa. Tout le monde a consenti.

### 3. La simplicité est un droit

Si une luciole veut juste remplir un formulaire, elle ne devrait pas avoir à télécharger 3 Mo de JavaScript, rendre 800 nœuds DOM, et décoder une image captcha. **Le chemin le plus simple devrait exister.**

### 4. L'environnement s'adapte, pas le visiteur

L'accessibilité a appris au web à s'adapter aux lecteurs d'écran. Khadyota demande au web de s'adapter aux **lucioles**. Même principe, autre visiteur.

### 5. L'éphémère est respectable

Une luciole vit le temps d'une tâche. Elle n'a pas besoin de compte permanent, de cookies persistants, de profil utilisateur. **Le TTL est une feature, pas un bug.**

---

## GAFAM comme terrain d'expérimentation

GAFAM est l'un des premiers endroits où ce protocole peut vivre, parce qu'il a déjà les briques :

| Brique GAFAM | Rôle dans Khadyota |
|---|---|
| **Nœud Personnel** (manifest 19) | Source de l'identité — signe les Dīpa |
| **Suparna** (manifest 20) | L1 — génère les cartes de rapport de luciole |
| **Edge L2** (manifest 21) | L2 — lucioles profondes, contexte long |
| **Vātāyana** (manifest 22) | Firefox noVNC — le **fallback** quand un site n'a pas de Mārga |
| **Vātāyana Mode B** (isolated) | Firefox headless — pour les lucioles qui doivent quand même rendre des pixels |

### Flux GAFAM → Khadyota

```
1. Humain : « Réserve-moi une chambre à l'Hôtel Le Marais pour vendredi »
2. Nœud : crée un Dīpa (signé, TTL 1h, scope hotel-le-marais.com)
3. Nœud : spawn Khadyota avec le Dīpa + la tâche
4. Khadyota → hotel-le-marais.com (Accept: khadyota+markdown)
   ├── Site a un Mārga → chemin facilité → 3 requêtes → done
   └── Site n'a pas de Mārga → fallback Vātāyana (Firefox headless) → plus lent mais marche
5. Khadyota → rapport → Nœud → frontend → carte dans le dashboard
6. Humain voit : « Réservé. Simple, 178€. Annuler ? »
```

### Le fallback Vātāyana

Tant que le web n'a pas adopté Khadyota, la luciole peut **tomber** sur un site sans Mārga. Dans ce cas :

- **Mode A (visible)** : Vātāyana s'ouvre en noVNC, l'humain voit la luciole naviguer en Firefox GUI
- **Mode B (isolated)** : Firefox headless + marionette, la luciole pilote sans pixels, plus lent

**Vātāyana n'est pas le protocole. Vātāyana est le radeau de sauvetage pour le web qui n'a pas encore ses Mārga.**

---

## Phases

### Phase 0 — Idée *(ce manifeste)*

- Définir la sensibilité
- Définir le format Mārga (MD + annotations)
- Définir le Dīpa (jeton d'identité)
- Pas d'implémentation

### Phase 1 — Lucioles exploratrices *(GAFAM)*

- Suparna peut **proposer** une tâche web à l'humain
- L'humain valide → Dīpa signé → Khadyota spawn
- Fallback Vātāyana pour tous les sites (personne n'a de Mārga encore)
- Rapport structuré dans le dashboard

### Phase 2 — Premiers Mārga *(sites volontaires)*

- Sites pionniers publient `khadyota.txt` ou répondent au header
- La luciole préfère le Mārga quand il existe → 100x plus rapide
- Outils de publication Mārga pour développeurs (middleware Express, plugin WordPress, etc.)

### Phase 3 — Adoption *(si Phase 2 convainc)*

- Library standard pour servir du Mārga (`khadyota-serve` npm, `khadyota-middleware` pip)
- Annuaires de sites compatibles Khadyota
- Le `Accept: application/vnd.khadyota+markdown` devient reconnu comme `Accept: application/json`

### Phase 4 — Normalisation

- RFC ou spec ouverte
- W3C / IETF submission
- Khadyota devient un standard du web comme RSS, OpenGraph, robots.txt

---

## Refus explicites

- Khadyota qui agit sans Dīpa signé (jamais d'anonymat absolu)
- Sites forcés de publier un Mārga (le Mārga est volontaire, comme robots.txt)
- Khadyota qui modifie des données sans action explicite du site (pas de injection de formulaire)
- Dīpa sans TTL (une luciole est éphémère, par définition)
- Khadyota qui se reproduit ou spawn d'autres Khadyota (delegation_depth: 1 max)
- Sites qui utilisent le Dīpa pour tracker l'humain (le Dīpa ne contient que hash(pubkey))

---

## Questions ouvertes

1. Le Dīpa doit-il être vérifiable **sans** contacter le nœud émetteur ? (JWT vs session)
2. Comment un site **révoque** un Mārga publié par erreur ?
3. Les lucioles peuvent-elles partager un Dīpa entre elles ? (non — delegation_depth: 1)
4. Faut-il un registre des nœuds émetteurs de Dīpa ? (privacy tradeoff)
5. Le Mārga supporte-t-il le streaming (SSE, WebSocket) pour les tâches longues ?
6. Comment les lucioles gèrent-elles les paiements ? (Dīpa + payment delegation ?)
7. Un humain peut-il lire un Mārga directement ? (oui — c'est du Markdown lisible)
8. Faut-il un format Mārga binaire plus compact pour les lucioles très contraintes ?

---

## Synthèse

> **Le web a appris à s'adapter aux écrans (responsive), aux handicaps (accessibilité), aux langues (i18n). Il n'a jamais appris à s'adapter aux visiteurs qui ne sont pas humains — pas des bots anonymes, mais des fragments signés de quelqu'un.**
>
> **Khadyota est le protocole des lucioles : un chemin de lumière pour les étincelles de nous-mêmes. Le site reconnaît la luciole, se simplifie, offre ses actions en Markdown. La luciole agit, revient, pose le résultat sur la table.**
>
> **Pas de navigateur. Pas de pixels. Pas de VNC. Juste du sens, des actions, et une identité vérifiable.**
>
> **Vātāyana restera — c'est le radeau. Mais le but, c'est que le radeau ne serve plus.**

---

## Liens manifestes

| # | Relation |
|---|---|
| **19** | Le Nœud Personnel signe les Dīpa — source de l'identité |
| **20** | Suparna interprète les rapports de luciole |
| **21** | L2 Edge — lucioles profondes avec contexte long |
| **22** | Vātāyana — le fallback quand le Mārga n'existe pas |
| **18** | Ghost Clone — la luciole du téléphone (sémantique, pas pixels) |

> *Les lucioles ne remplacent pas les humains sur le web. Elles y vont **pour** eux, **avec** leur lumière, et le web leur ouvre un chemin.*
