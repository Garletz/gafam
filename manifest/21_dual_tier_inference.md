# 21. Inférence à deux niveaux — VPC léger · Téléphone profond

> **Statut :** manifeste de conception (évolution de [Suparna](20_suparna.md)).  
> **Idée en une phrase :** le VPC garde un **petit cerveau toujours disponible** pour les micro-tâches ; pour les tâches lourdes, il **réquisitionne la RAM du téléphone** via l’APK, récupère la réponse, et l’affiche sur le front.

---

## Alignement produit (ce qu’on veut)

| Niveau | Où | RAM | Rôle |
| :--- | :--- | :--- | :--- |
| **L1 — Micro** | VPC (`gafam-qwen` 0.6B) | ~500 Mo, wake court | Petits logs, SMS courts, tags, classif rapide — **algo modulaire** |
| **L2 — Profond** | Téléphone (APK) | 2–4 Go réservables à la demande | Long contexte, journée entière, synthèse riche — **wake distant** |

Le VPC n’est **pas** le seul computeur. C’est l’**orchestrateur** : il choisit L1 ou L2, route, attend, renvoie au front.

---

## L1 — Qwen très léger sur le VPC (déjà en place, à spécialiser)

**Modèle :** Qwen3-0.6B GGUF Q4 · `llama.cpp` sidecar · stoppé par défaut.

**Cas d’usage (exemples) :**

- Résumer **un** SMS entrant
- Classifier une alerte (`spam` / `2FA` / `banque`)
- Lire **10–30 lignes** de logs filtrés
- Pré-traiter avant envoi L2 (optionnel)

**Comportement :** même pattern qu’aujourd’hui — disque → RAM à la demande → auto-stop.

**Limite acceptée :** qualité faible, contexte 2048 tokens — **suffisant pour les petites tâches**, pas pour une journée complète.

---

## L2 — Réquisition RAM téléphone via l’APK

**Principe :** le téléphone expose une **ressource d’inférence distante** que le VPC (ou le front via le VPC) peut **réveiller**.

```
Front (gafam.cloud)
    │  POST /api/infer { task, payload, tier: "auto"|"light"|"deep" }
    ▼
VPC (gafam-api)
    │  tier=light  → gafam-qwen local (L1)
    │  tier=deep   → forward vers APK pairé
    ▼
APK (EdgeInferenceService)
    │  wake : charge GGUF en RAM (foreground service)
    │  infer : llama.cpp on-device
    │  sleep : libère RAM
    ▼
VPC ← JSON résultat ← APK
    ▼
Front affiche
```

**Qui peut appeler L2 :**

- Onglet **Logs** (Suparna journée complète)
- **Front** (question utilisateur, enrichissement UI)
- **VPC** (batch, règles, enrichissement SMS entrant)
- **Manager** (via relay existant)

Une seule brique edge — **plusieurs surfaces produit**.

---

## Routage automatique (`tier: auto`)

| Critère | → L1 VPC | → L2 Téléphone |
| :--- | :--- | :--- |
| Tokens estimés | &lt; ~800 | &gt; ~800 |
| Durée attendue | &lt; 30 s | &gt; 30 s |
| Téléphone joignable | — | **requis** |
| scrcpy / heavy actif | — | **refus** (mutex) |
| Fallback si L2 offline | L1 dégradé ou file d’attente | — |

---

## Mutex & RAM (inchangé philosophiquement)

- **VPC 1 Go :** jamais L1 + scrcpy cloud stream en parallèle.
- **Téléphone :** jamais L2 + scrcpy en parallèle.
- **Une inférence à la fois** par nœud (`inferMu`).

---

## Contrat API (esquisse)

```json
// Requête (front ou VPC)
{
  "task": "suparna|sms_summarize|classify|…",
  "tier": "auto",
  "prompt": "…",
  "max_tokens": 512
}

// Réponse
{
  "content": "…",
  "engine": "qwen-vpc-0.6b | qwen-phone-1.5b",
  "tier_used": "light | deep",
  "ram_peak_mb": 420,
  "latency_ms": 95000
}
```

---

## Ce que le VPC **ne fait plus** seul (vision cible)

- Analyser 500+ lignes de logs avec qualité
- Tenir un long contexte conversationnel
- Remplacer le téléphone comme « vrai » cerveau

Le VPC reste : **logs, SMS, relay, honeypot, auth, routage, fallback L1**.

---

## Phases d’implémentation

| Phase | Livrable |
| :--- | :--- |
| **0** *(fait)* | L1 VPC Suparna wake/stop, async polling front |
| **1** | Router `tier` + tâches L1 dédiées (SMS, micro-logs) |
| **2** | `EdgeInferenceService` APK + protocole relay VPC↔tel |
| **3** | L2 Suparna journée + fallback L1 si tel offline |
| **4** | Appels L2 depuis front (hors logs) |

---

## Liens

| # | Relation |
| :--- | :--- |
| **20** | Suparna Phase 1 = premier cas L1 |
| **14** | Mutex scrcpy / bridge |
| **19** | Nœud personnel = téléphone comme ressource |
| **1** | Cerveau distribué : VPC coordonne, tel compute quand il faut |

---

## Synthèse

> **VPC = petit algo toujours prêt. Téléphone = réserve RAM profonde qu’on réquisitionne à distance. Le front ne sait pas où ça tourne — il envoie une tâche, il reçoit une réponse.**

*Rankamura a prouvé l’inférence on-device. GAFAM généralise : même wake/stop, mais pilotable depuis le cloud.*
