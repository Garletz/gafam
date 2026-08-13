# 30. Phalaka — फलक · le panneau des missions de secours

> **Statut :** manifeste de vision — **VERSION 2 de GAFAM.** Très important.
> **Nom :** Phalaka (फलक) = le panneau, la plaque d'affichage. *(nom de travail, remplaçable.)*
> **Inspiration :** le **panneau d'affichage** de *Pokémon Donjon Mystère* (Red/Blue Rescue Team) — le tableau devant le **Poste Bekipan**, où une **équipe de secours** décroche les missions des Pokémon en détresse.
> **Prérequis (à finaliser AVANT Phalaka) :**
> - le **record custom / Ghost Clone** (manifeste 18) — l'identité persistante qui poste et prend des missions,
> - la **fédération** (manifestes 2, 6, 17) — le transport du panneau et du courrier,
> - la **boîte auto-publieuse / messages dans le temps** (manifeste 17, partie 3, + 7 DTN) — les missions qui patientent,
> - **Dakṣiṇā** (manifeste 27) — la récompense,
> - **Saṃyojaka** (manifeste 25) — celui qui décide qu'il ne peut pas, et qui publie.
> **Lien vision :** le [Nœud Personnel](19_personal_node.md) est un ange gardien **numérique**. Phalaka lui donne des mains **physiques**, en confiant à des équipes de secours ce qu'aucun octet ne peut faire.

---

## Pourquoi « Phalaka »

Dans *Pokémon Donjon Mystère*, la Place Pokémon abrite un bureau de poste : le **Poste Bekipan**. Devant lui, un simple **panneau d'affichage**. C'est là que tout se joue :

- Un Pokémon en détresse **épingle** une mission — *« je suis perdu dans les Bois Persévérants, secourez-moi »*, *« livrez ce colis au fond de la Grotte de l'Éclair »*.
- Une **équipe de secours** passe, **lit** les missions, en **décroche** une, et descend dans le donjon pour l'accomplir.
- La mission affiche sa **récompense**. À la clé : des Poké, des objets, et un **rang** qui monte (Bronze, Argent, Or, Diamant, Ultra).
- La **Banque Persian** garde l'argent de l'équipe à l'abri — si l'on tombe dans le donjon, on ne perd pas tout.
- À la fin, la **lettre de remerciement** arrive par le courrier de Pelipper.

Ce n'est ni un marché centralisé, ni une entreprise : c'est **un panneau, un courrier, une récompense, une banque, et des équipes qui s'entraident**.

> *Phalaka n'exécute pas. Phalaka affiche. Et c'est en affichant qu'il rend possible ce que le nœud ne peut pas faire seul.*

---

## Le constat en une phrase

> **Saṃyojaka sait presque tout faire dans le numérique : lire, chercher, écrire, appeler des API, envoyer des SMS. Mais il échoue dès que la solution n'est pas un octet. Le jour où l'on demande « livre-moi un dromadaire sous la tour Eiffel dans cinq jours », aucun outil du registre ne répond — et le juge le sanctionne. Il faut alors un panneau : afficher la mission, et qu'une équipe de secours la décroche.**

La frontière est nette, et c'est elle qui fonde Phalaka :

- **Faisable en CLI / API / sandbox** → Saṃyojaka le fait lui-même, souverainement.
- **Physique, légal, humain, relationnel** (un colis, une présence, un coup de main, un savoir local) → **Phalaka** : le nœud épingle la mission, une équipe la décroche.

---

## Le concept

Un **panneau d'affichage public et fédéré**. Chaque nœud peut y épingler une mission ; chaque nœud (ou chaque humain, via son nœud) peut la décrocher.

```
                ┌─────────────────────────────────────────────┐
                │   Phalaka — le panneau (fédéré, public)       │
                │                                              │
  Demandeur ───►│  📜 « livre un dromadaire sous la tour Eiffel │
  (nœud GAFAM)  │      dans 5 jours — récompense : 50 credits » │
                │                                              │
                │  ┌───────────────────────────────┐           │
  Équipe de     │  │  « on s'en occupe »  (humain) │           │
  secours ──────┤  └───────────────────────────────┘           │
  (humain ou    │                                              │
   autre nœud)  │  🏦 récompense déposée (Banque Persian)      │
                │  ⚖️ vérification → libération                │
                │  📬 lettre de remerciement + rang qui monte  │
                └─────────────────────────────────────────────┘
```

### Les types de mission (comme au panneau)

| Type | Exemple GAFAM | Équivalent Donjon Mystère |
|---|---|---|
| **Secourir** | « quelqu'un peut-il me rejoindre physiquement / m'aider sur place ? » | Pokémon perdu à secourir |
| **Livrer** | « apporte-moi ceci, là, avant telle date » | livrer un objet au fond du donjon |
| **Retrouver** | « trouve-moi ce produit / cette personne / ce renseignement local » | retrouver un objet précis |
| **Escorter** | « accompagne cette personne / ce bien d'un point à un autre » | escorter un Pokémon |

### Les acteurs

| Rôle | Qui | Équivalent dans le jeu |
|---|---|---|
| **Demandeur** | un nœud GAFAM (son propriétaire reste le sceau) | le Pokémon qui épingle la mission |
| **Équipe de secours** | un humain (avec ou sans nœud), ou un agent d'un autre nœud | l'équipe qui décroche |
| **Poste Bekipan** | le transport — fédération Poneglyph + boîte messages-dans-le-temps | le courrier de Pelipper |
| **Banque Persian** | l'**escrow** : la récompense déposée, libérée seulement à la vérification | la banque qui garde l'argent |
| **Rang** | la réputation accumulée (Bronze → Ultra) | le rang de l'équipe |

### Le cycle de vie d'une mission

```
brouillon → épinglée → décrochée → négociation → mission scellée
          → exécution (hors-ligne, par l'équipe)
          → vérification → libération (réussite) ou restitution (échec) → clôture + rang
```

1. **Brouillon** — Saṃyojaka a échoué (juge `failed`), ou le demandeur demande explicitement un humain. Il rédige la mission : objet, contraintes (délai, lieu), deal, récompense.
2. **Épinglée** — affichée au panneau fédéré, signée (ed25519), datée. Elle voyage par **Poneglyph** : chacun publie chez soi, les autres la lisent en scannant.
3. **Décrochée** — une équipe de secours se signale. La mission n'est plus libre.
4. **Négociation** — dialogue (Samvada, manifeste 28) sur le deal : prix, délai, conditions. Le Demandeur garde le dernier mot ; l'humain reste le sceau final.
5. **Mission scellée** — l'accord est enregistré, signé par les deux, et la **récompense est déposée à la Banque** (escrow).
6. **Exécution** — se passe dans le monde réel. Le nœud ne peut ni l'exécuter ni le contrôler ; il patiente et relance aux échéances.
7. **Vérification** — le Demandeur confirme (preuve, photo, signature, témoin). L'humain tranche.
8. **Libération / Restitution** — la Banque libère la récompense à l'équipe, ou la restitue au Demandeur. Chaque issue fait monter (ou descendre) le **rang** des deux.

---

## La sécurité de la transaction — le cœur du problème

*« je ne sais pas comment l'encadrer »* — c'est la bonne question, et c'est elle qui fait de Phalaka un manifeste et pas un gadget. Sans garde-fou, c'est un terrain de jeu pour arnaqueurs. L'encadrement, par couches, du plus simple au plus robuste :

1. **La Banque Persian (escrow souverain).** La récompense (en crédits Dakṣiṇā, ou une promesse de paiement signée ed25519) est **déposée** avant l'exécution — exactement comme l'équipe dépose son argent à la banque pour ne pas le perdre dans le donjon. Le dépôt ne se déverrouille qu'avec la signature de **libération** du Demandeur (réussite) ou de **restitution** (échec/annulation). Personne ne peut fuir avec : il est soit libéré, soit restitué.

2. **Le maître de quête (tiers de confiance facultatif).** Pour les grosses missions, un **Garant humain** choisi d'un commun accord (un gardien de la Web of Trust, manifeste 5) reçoit une clé de médiation : en cas de litige, il tranche et sa signature débloque le dépôt.

3. **Le rang, jamais une note globale.** Chaque mission close dépose une **attestation signée** dans le graphe de confiance. Une équipe est jugée sur l'historique vérifiable de ses missions, pas sur un score facile à tricher. Un nouveau venu commence **Bronze** (petites missions, petites sommes) ; le **Ultra** se mérite mission après mission — comme au panneau, le rang ouvre des missions plus grandes.

4. **Le nœud ne s'engage jamais au-delà de son dépôt.** Saṃyojaka n'a pas de compte en banque, pas de corps. Il ne peut promettre que ce que le propriétaire a explicitement autorisé. Tout deal qui engagerait plus que la récompense déposée est **refusé** par construction.

---

## Le transport — on ne réinvente rien

Phalaka réutilise les organes déjà scellés :

| Brique existante | Rôle dans Phalaka | Équivalent du jeu |
|---|---|---|
| **Poneglyph** (17) | la publication du panneau : chaque mission est une enveloppe signée, publiée chez soi, lue chez les autres | le panneau d'affichage |
| **Boîte auto-publieuse** (17, partie 3) + **DTN** (7) | les missions « dans le temps » : un dromadaire dans 5 jours = une mission qui patiente et se rappelle aux échéances | le courrier de Pelipper qui attend son heure |
| **Signatures ed25519** (feed) | l'intégrité et la non-répudiation : qui a demandé, qui a promis, qui a libéré | — |
| **Web of Trust** (5) | le rang et le garant | le rang de l'équipe |
| **Dakṣiṇā** (27) | la récompense (crédits) | les Poké |
| **Saṃyojaka** (25) + **le juge** | décider qu'on ne peut pas, rédiger la mission, vérifier la preuve | — |
| **Samvada** (28) | la négociation éphémère entre Demandeur et équipe | — |

---

## Ce que Phalaka n'est pas

- ❌ **Pas une plateforme centralisée** : le panneau est fédéré, chaque nœud publie chez soi. Aucun serveur « phalaka.gafam.cloud » qui capte une commission.
- ❌ **Pas un marché noir ni un Uber de tout** : refus par construction de l'illégal, de l'armement, de la santé sans garde-fou, de ce qui engage la responsabilité pénale du Demandeur. Le propriétaire reste juridiquement responsable — le nœud le rappelle avant chaque publication.
- ❌ **Pas un remplacement de l'agent** : Phalaka n'est appelé qu'**après** l'échec souverain. Le réflexe reste « le faire soi-même ».
- ❌ **Pas une DAO ni un token** : pas de gouvernance déléguée, pas de spéculation. Le rang n'est pas un actif financier, c'est de la mémoire signée — une lettre de remerciement, pas un titre.

---

## Phases (roadmap V2)

| Phase | Contenu | Dépend |
|---|---|---|
| **0 — socle** | record custom / Ghost Clone, fédération, boîte messages-dans-le-temps | — |
| **1 — le panneau** | épingler / scanner / décrocher une mission (enveloppes signées, états, types de mission) | 0 |
| **2 — la Banque** | dépôt verrouillé, libération/restitution signées, maître de quête optionnel | 0 |
| **3 — le rang** | attestations signées dans la Web of Trust, progression Bronze → Ultra | 1, 2 |
| **4 — autonomie** | Saṃyojaka épingle tout seul après échec, négocie, vérifie la preuve — sous approbation humaine | 1, 2, 3 |

---

## Questions ouvertes

- Le **nom** : Phalaka est un nom de travail (celui-ci au moins colle à la chose : le panneau). Autre candidat ?
- L'**unité de la récompense** : crédits Dakṣiṇā ? Une promesse signée de paiement externe ? Les deux selon la taille ?
- Le **maître de quête** : un pool de garants de confiance (les gardiens de la Web of Trust) ou un garant élu par mission ?
- La **preuve de réception** : photo signée, géolocalisation, témoin — quelle est la barre minimale pour libérer le dépôt ?
- La **lettre de remerciement** : doit-elle être automatique (générée par le nœud à la clôture), comme le courrier de Pelipper ?
