# Plan d'initiation au développement web — Stack gratuite & pragmatique

**Prérequis :** bases web déjà acquises (HTML/CSS/JS), bonnes capacités d'apprentissage, solides fondamentaux en informatique.

---

## 1. Outils & Comptes à mettre en place

| Outil | Rôle | Détail |
|---|---|---|
| **Compte Google/Gmail dédié** | Identité de dev centralisée | Utiliser "Connexion avec Google" partout pour simplifier l'auth |
| **Session Chrome dédiée** | Séparation des environnements | Profil Chrome distinct avec le compte Gmail dev, mots de passe isolés |
| **Cloudflare (gratuit)** | Hébergement + DNS + BDD | Workers, Pages, D1 database, R2 storage — tout gratuit en usage classique |
| **GitHub (gratuit)** | Versioning & backup | Dépôts privés illimités, rollback à n'importe quel état antérieur (`git reflog` = time machine) |
| **DeepSeek Chat** | Assistant de code & recherche | Pour tout : bugs, architecture, explications, recherches approfondies |

---

## 2. IDE

**Antigravity IDE** (gratuit, mature en 2026) :
- Connexion avec le compte Google dédié
- Premier message au modèle (Gemini Flash 2.x ou autre) :

> *"Construis-moi un site en Svelte dans mon dossier actif et publie-le avec un domaine Cloudflare .dev sur un Worker. Connecte-toi à mon compte Cloudflare pour créer et relier une base de données D1. Explique-moi comment lancer le site en local, comment fonctionnent les commandes de push sur Wrangler pour le mettre en production."*

---

## 3. GitHub en pratique

- **Pas juste de l'open source** — c'est un outil de backup gratuit et universel
- Tes projets restent **privés** par défaut
- `git add` / `git commit` / `git push` pour sauvegarder
- `git reflog` + `git reset --hard <hash>` pour revenir à un état antérieur
- À pratiquer directement dans l'IDE sur un projet réel (des commits, des rollbacks, explorer)

---

## 4. Lectures recommandées

1. **The Mom Test** — Ne jamais demander l'avis de ses amis ou de sa famille sur son produit. Leurs retours sont biaisés par la bienveillance. Ils jugent le résultat, pas le process. Ne pas montrer l'évolution du projet, juste demander un coup de main sur un blocage technique précis.

2. **Livres de marketing** — Comprendre la distribution, l'acquisition, le positionnement.

3. **Livres sur la qualité produit / rétrospectives (Defcon récurrentes)** — Améliorer la qualité du produit de façon itérative, revues régulières.

---

## 5. Règles d'or

- **DeepSeek d'abord** — Avant de demander de l'aide à qui que ce soit, poser la question à DeepSeek. Il répondra à 99% des cas.
- **Ne pas interférer dans le projet des autres** — Ta vision de ton projet est forcément la meilleure et la plus singulière. Pas d'avis extérieur non sollicité.
- **Savoir passer au projet suivant** — Parfois c'est une question de timing, et on peut le rater. Ranger ses projets proprement (GitHub) pour pouvoir y revenir.
- **Tout est une question de chance** — Et bien comprendre la définition de la chance : préparation × opportunité × action.

---

## Résumé de la stack

```
Google (auth) → Cloudflare (hosting, DNS, D1) → GitHub (backup, versioning) → DeepSeek (assistant)
```

Coût total : **0€** en usage classique.
