# 30. Vipaṇa — विपण · le bazar souverain des quêtes

> **Statut :** manifeste de vision — **VERSION 2 de GAFAM.** Très important.
> **Nom :** Vipaṇa (विपण) = le marché, le bazar. *(nom de travail, remplaçable.)*
> **Prérequis (à finaliser AVANT Vipaṇa) :**
> - le **record custom / Ghost Clone** (manifeste 18) — l'identité persistante qui prend et rend des quêtes,
> - la **fédération** (manifestes 2, 6, 17) — le transport du panneau,
> - la **boîte auto-publieuse / messages dans le temps** (manifeste 17, partie 3, + 7 DTN) — les quêtes qui patientent,
> - **Dakṣiṇā** (manifeste 27) — la monnaie de la récompense,
> - **Saṃyojaka** (manifeste 25) — celui qui décide qu'il ne peut pas, et qui publie.
> **Lien vision :** le [Nœud Personnel](19_personal_node.md) est un ange gardien **numérique**. Vipaṇa lui donne des mains **physiques**, par délégation à des humains — le premier pas hors du silo souverain, sans jamais le trahir.

---

## Pourquoi « Vipaṇa »

**Vipaṇa** est le bazar, la place de marché où ce qui manque à l'un est offert par l'autre. Ce n'est ni une plateforme, ni un Uber, ni un Amazon : c'est un **panneau de quêtes** — le tableau d'affichage du donjon, celui où l'on épingle une mission et où un aventurier vient la décrocher.

Dans un donjon mystère, le panneau ne résout rien lui-même. Il **connecte** : celui qui a besoin, celui qui peut, et la règle qui garantit que les deux ne se font pas avoir.

> *Vipaṇa n'exécute pas. Vipaṇa fait exécuter — par le monde, pas seulement par des agents.*

---

## Le constat en une phrase

> **Saṃyojaka sait presque tout faire dans le numérique : lire, chercher, écrire, appeler des API, envoyer des SMS. Mais il échoue dès que la solution n'est pas un octet. Le jour où l'on demande « livre-moi un dromadaire sous la tour Eiffel dans cinq jours », aucun outil du registre ne répond — et le juge le sanctionne. Il faut une porte de sortie qui n'est pas un agent, mais un autre être humain.**

La frontière est nette, et c'est elle qui fonde Vipaṇa :

- **Faisable en CLI / API / sandbox** → Saṃyojaka le fait lui-même, souverainement.
- **Physique, légal, humain, relationnel** (un colis, une présence, un coup de main, un savoir local) → **Vipaṇa** : le nœud publie la quête, un humain la prend.

---

## Le concept

Un **panneau de quêtes public et fédéré**. Chaque nœud peut y épingler une demande ; chaque nœud (ou chaque humain, via son nœud) peut la décrocher.

```
                ┌─────────────────────────────────────────────┐
                │   Vipaṇa — le panneau (fédéré, public)        │
                │                                              │
  Demandeur ───►│  📜 « livre un dromadaire sous la tour Eiffel │
  (nœud GAFAM)  │      dans 5 jours — récompense : 50 credits » │
                │                                              │
                │  ┌───────────────────────────────┐           │
  Preneur ──────┤  │  « je m'en occupe »  (humain) │           │
  (humain ou    │  └───────────────────────────────┘           │
   autre nœud)  │                                              │
                │  🔒 récompense séquestrée (escrow)           │
                │  ⚖️ vérification → libération                │
                └─────────────────────────────────────────────┘
```

### Les acteurs

| Rôle | Qui | Ce qu'il fait |
|---|---|---|
| **Demandeur** | un nœud GAFAM (son propriétaire humain reste le sceau) | publie la quête, fixe le deal, dépose la récompense |
| **Preneur** | un humain (avec ou sans nœud), ou un agent d'un autre nœud | décroche la quête, promet de l'exécuter |
| **Garant** | le mécanisme d'**escrow** (dépôt de garantie) | séquestre la récompense, ne la libère qu'à la vérification |
| **Public** | tous les nœuds fédérés | voient le panneau, attestent, scrutent, font de la réputation |

### Le cycle de vie d'une quête

```
brouillon → publiée → prise → négociation → deal scellé
          → exécution (hors-ligne, par le Preneur)
          → vérification → libération (réussite) ou restitution (échec) → clôture + réputation
```

1. **Brouillon** — Saṃyojaka a échoué (juge `failed`), ou le demandeur demande explicitement un humain. Il rédige la quête : objet, contraintes (délai, lieu), deal, récompense.
2. **Publiée** — épinglée au panneau fédéré, signée (ed25519), datée. Elle voyage par **Poneglyph** : chacun publie chez soi, les autres la lisent en scannant.
3. **Prise** — un Preneur se signale. La quête n'est plus libre.
4. **Négociation** — dialogue (Samvada, manifeste 28) sur le deal : prix, délai, conditions. Le Demandeur garde le dernier mot ; l'humain reste le sceau final (approbation, comme partout dans GAFAM).
5. **Deal scellé** — l'accord est enregistré, signé par les deux, et la **récompense est séquestrée** (escrow).
6. **Exécution** — se passe dans le monde réel. Le nœud ne peut ni l'exécuter ni le contrôler ; il ne fait que patienter et relancer aux échéances.
7. **Vérification** — le Demandeur confirme (preuve, photo, signature, témoin). Ici aussi, l'humain tranche.
8. **Libération / Restitution** — le Garant libère la récompense au Preneur, ou la restitue au Demandeur. Chaque issue nourrit la **réputation** des deux.

---

## La sécurité de la transaction — le cœur du problème

*« je ne sais pas comment l'encadrer »* — c'est la bonne question, et c'est elle qui fait de Vipaṇa un manifeste et pas un gadget. Sans escrow, c'est un terrain de jeu pour arnaqueurs. Voici l'encadrement, par couches, du plus simple au plus robuste :

1. **Escrow souverain par dépôt signé.** La récompense (en crédits Dakṣiṇā, ou une promesse de paiement signée ed25519) est déposée dans une **enveloppe fédérée verrouillée** avant l'exécution. Elle ne se déverrouille qu'avec la signature de **libération** du Demandeur (réussite) ou de **restitution** (échec/annulation). Personne ne peut fuir avec le dépôt : il est soit libéré, soit restitué.

2. **Le tiers de confiance facultatif.** Pour les grosses quêtes, un **Garant humain** choisi d'un commun accord (un gardien de la Web of Trust, manifeste 5) reçoit une clé de médiation : en cas de litige, il tranche et sa signature débloque. C'est le « maître de quête » du donjon.

3. **Réputation accumulée, jamais une note globale.** Chaque quête close dépose une **attestation signée** dans le graphe de confiance. Un Preneur est jugé sur l'historique vérifiable de ses quêtes, pas sur un score facile à tricher. Un nouveau venu commence petit (petites quêtes, petites sommes) — la réputation se mérite par la taille des quêtes réussies.

4. **Le nœud ne s'engage jamais au-delà de son dépôt.** Saṃyojaka n'a pas de compte en banque, pas de corps. Il ne peut promettre que ce que le propriétaire a explicitement autorisé. Tout deal qui engagerait plus que la récompense séquestrée est **refusé** par construction.

---

## Le transport — on ne réinvente rien

Vipaṇa réutilise les organes déjà scellés :

| Brique existante | Rôle dans Vipaṇa |
|---|---|
| **Poneglyph** (17) | la publication du panneau : chaque quête est une enveloppe signée, publiée chez soi, lue chez les autres |
| **Boîte auto-publieuse** (17, partie 3) + **DTN** (7) | les quêtes « dans le temps » : un dromadaire dans 5 jours = une quête qui patiente et se rappelle à toi aux échéances |
| **Signatures ed25519** (feed) | l'intégrité et la non-répudiation : qui a demandé, qui a promis, qui a libéré |
| **Web of Trust** (5) | la réputation et le garant |
| **Dakṣiṇā** (27) | la monnaie (crédits) et l'idée d'un métabolisme de valeur |
| **Saṃyojaka** (25) + **le juge** | décider qu'on ne peut pas, rédiger la quête, et vérifier la preuve |
| **Samvada** (28) | la négociation éphémère entre Demandeur et Preneur |

---

## Ce que Vipaṇa n'est pas

- ❌ **Pas une plateforme centralisée** : le panneau est fédéré, chaque nœud publie chez soi. Aucun serveur « vipana.gafam.cloud » qui capte une commission.
- ❌ **Pas un marché noir ni un Uber de tout** : refus par construction de l'illégal, de l'armement, de la santé sans garde-fou, de ce qui engage la responsabilité pénale du Demandeur. Le propriétaire reste juridiquement responsable — le nœud le rappelle avant chaque publication.
- ❌ **Pas un remplacement de l'agent** : Vipaṇa n'est appelé qu'**après** l'échec souverain. Le réflexe reste « le faire soi-même ».
- ❌ **Pas une DAO ni un token** : pas de gouvernance déléguée, pas de spéculation. La réputation n'est pas un actif financier, c'est de la mémoire signée.

---

## Phases (roadmap V2)

| Phase | Contenu | Dépend |
|---|---|---|
| **0 — socle** | record custom / Ghost Clone, fédération, boîte messages-dans-le-temps | — |
| **1 — panneau** | publier / scanner / décrocher une quête (enveloppes signées, états) | 0 |
| **2 — escrow** | dépôt verrouillé, libération/restitution signées, tiers de confiance optionnel | 0 |
| **3 — réputation** | attestations signées dans la Web of Trust, progression par taille de quête | 1, 2 |
| **4 — autonomie** | Saṃyojaka publie tout seul après échec, négocie, vérifie la preuve — sous approbation humaine | 1, 2, 3 |

---

## Questions ouvertes

- Le **nom** : Vipaṇa est un nom de travail. Autre candidat ? (le projet aime le sanskrit, mais le tien fera loi.)
- L'**unité de la récompense** : crédits Dakṣiṇā ? Une promesse signée de paiement externe ? Les deux selon la taille ?
- Le **garant** : faut-il un pool de garants de confiance (les gardiens de la Web of Trust) ou un garant élu par quête ?
- La **preuve de réception** : photo signée, géolocalisation, témoin — quelle est la barre minimale pour libérer le dépôt ?
