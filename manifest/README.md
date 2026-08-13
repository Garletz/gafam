# GAFAM Relay : Index du Manifeste

Ce dossier contient l'ensemble des concepts architecturaux et philosophiques du projet, découpés par thématique, numérotés dans l'ordre de leur conception.

## Fondations

1. [Philosophie Globale & Souveraineté](manifest/1_core_philosophy.md) : L'auto-hébergement et les 3 piliers matériels et logiciels.
2. [Le Protocole Fédéré (Messagerie)](manifest/2_federated_messaging.md) : Comment le réseau gère l'envoi de messages via P2P (Matrix-like) ou via Fallback SMS.
3. [Sign-In with GAFAM Relay (SSO Décentralisé)](manifest/3_decentralized_sso.md) : L'utilisation de l'appareil comme trousseau de clés universel cryptographique pour remplacer Google Sign-In.
4. [Le Handshake GAFAM (Filtre Anti-Spam Absolu)](manifest/4_anti_spam_handshake.md) : Modèle "Zero-Trust" pour trier les robots/spams dans un Purgatoire et réserver l'alerte aux contacts validés.
5. [Social Recovery & Web of Trust (Authentification sans appareil)](manifest/5_social_recovery.md) : Utiliser son réseau d'amis de confiance pour valider un login d'urgence si on a perdu son boîtier.
6. [Topologie Réseau et Hébergement du Web Client](manifest/6_network_topology_routing.md) : L'annuaire central de redirection tout en permettant l'auto-hébergement total des interfaces et des IP dynamiques.
7. [[SECRET] Réseau Sub-Internet & DTN](manifest/7_dtn_subnetwork_secret.md) : Le VPC comme passerelle asynchrone pour la communication temporelle longue distance (espace profond / réseaux maillés).

## Réseau, sécurité & accès

8. [Réflexion : Stack Technique pour gafam.cloud](manifest/8_stack_reflection_webclient.md) : Les choix de stack pour le dashboard web.
9. [TCP Socket Bypass — Cloudflare Error 1003](manifest/9b_tcp_socket_tls_solution.md) : Le proxy socket brut vers le VPC (contournement de l'interdiction Cloudflare des IP brutes).
10. [End-to-End Encryption (AES-GCM) & Remote SMS Outbox](manifest/10_e2e_encryption_and_sms_outbox.md) : Le chiffrement applicatif de bout en bout et l'envoi de SMS à distance.
11. [Dual-Binding, SNI Spoofing & Zero-Touch](manifest/11_native_vpn_zero_touch.md) : Le réseau natif VPN zero-touch et le déguisement du trafic.
12. [Le Rendez-vous Synchrone Mécanique](manifest/12_synchronous_mechanical_rendezvous.md) : L'authentification web par défi temps + clics.

## Téléphone & plateforme

13. [Platform Mode & Social Recovery](manifest/13_platform_mode.md) : Le centre de contrôle 3D infini (pan/zoom) de la topologie du nœud.
14. [Remote Android Control via Scrcpy Bridge](manifest/14_remote_android_control.md) : Le pilotage distant du téléphone (H.264 + ADB shell).
15. [Timezone & Temporal Synchronization](manifest/15_timezone_management.md) : La gestion des fuseaux et la synchronisation temporelle.
16. [RCS : Limites d'interception & contournements](manifest/16_rcs_interception_limitations.md) : Ce que le relais peut et ne peut pas intercepter, et la voie du remplacement.

## La couche fédérée & les agents

17. [Le Canal Poneglyph — Conjugaison Temporelle](manifest/17_poneglyph_conjugation_channel.md) : Communication par publication souveraine — publier chez soi, lire chez les autres, dans le temps et l'espace.
18. **[BROUILLON]** [Le Ghost Clone — Miroir sémantique du téléphone](manifest/18_ghost_clone.md) : Pistes exploratoires (clone sémantique, ghost, logcat/LLM) — non normatif.
19. **[BROUILLON]** [Le Nœud Personnel — La présence numérique permanente](manifest/19_personal_node.md) : Carnet de vision (nœud, surfaces, peering, bourgeon) — non normatif.
20. [Suparna — सुपर्ण · l'esprit aux belles ailes](manifest/20_suparna.md) : Qwen3-0.6B + ONNX sur le VPC ; lecture des logs du jour (Phase 1), rôle futur volontairement ouvert.
21. [Inférence à deux niveaux — VPC léger · Téléphone profond](manifest/21_dual_tier_inference.md) : L1 micro-tâches sur VPC ; L2 réquisition RAM téléphone via APK pour contexte long ; routage auto front/VPC.
22. [Vātāyana — वातायन · la fenêtre distante](manifest/22_vatayana_remote_browser.md) : Firefox ESR distant via noVNC dans le dashboard web ; sidecar Docker ; agents L1/L2 utilisables comme bac à sable web.
23. [Khadyota — खद्योत · le protocole des lucioles](manifest/23_khadyota_protocol.md) : Protocole ouvert non spécifique à GAFAM. Le web s'adapte aux clones d'identité (lucioles) : MD + actions au lieu de GUI. Dīpa, Mārga, Vātāyana comme fallback.
24. [Yantraśālā — यन्त्रशाला · l'établi du VPC](manifest/24_yantrashala_sandbox.md) : Terminal, filesystem, espace de stockage (VPC + Android). Conteneur Alpine sidecar — l'atelier où l'humain et les futures lucioles travaillent.
25. [Saṃyojaka — संयोजक · l'orchestre des lucioles](manifest/25_samyojaka_agent_orchestrator.md) : Agent registry, tool registry, task queue, pipeline suggestion→approbation→exécution→rapport, routage L1↔L2.
26. [Note de compréhension — L'orchestration](manifest/26_orchestration_comprehension.md) : Analyse comparative OpenCode / OpenHands / Manus. Ce qui est important, ce qui ne l'est pas.
27. [Dakṣiṇā — दक्षिणा · l'offrande qui fait grandir le logiciel](manifest/27_daksina/README.md) : Le modèle économique. Les dons deviennent des crédits pour un kāraka codeur (OpenCode + Kimi) — sous revue humaine, jamais de push direct.
28. [Saṃvāda · संवाद — Live Ephemeral Inter-VPC Chat](manifest/28_samvada_ephemeral_chat.md) : Le chat éphémère inter-nœuds, la négociation en direct.
29. **[FINAL]** [Pūrṇa — पूर्ण · le sceau de la plénitude](manifest/29_purna.md) : Le projet est plein. Périmètre scellé, triple filtre pour toute idée future, élagage encouragé.

---

## Version 2 — au-delà du sceau

30. **[VERSION 2 · TRÈS IMPORTANT]** [Phalaka — फलक · le panneau des missions de secours](manifest/30_phalaka_public_quest_board.md) : Le panneau de quêtes public et fédéré, inspiré du panneau d'affichage de Pokémon Donjon Mystère. Quand Saṃyojaka ne peut pas résoudre une demande (physique, humain, légal), il l'épingle ; une « équipe de secours » (un autre humain) la décroche, une banque-escrow garantit la transaction, le rang monte. À finaliser **avant** le record custom (Ghost), la fédération et la boîte messages-dans-le-temps.

---

## Annexes

- [Mokṣa — Méthode de Résolution Réfléchie](manifest/moksa/method1.md) : La boucle plan/DAG.
- [Méthodes des Autres Agents — Même Pattern ?](manifest/moksa/method2.md) : Revue de Grok, OpenCode, Manus, OpenHands.
- [Réflexions & Recherche Juillet 2026](manifest/moksa/method3.md) : Réflexions sur le parallélisme.
- [Mokṣa — Méthode 4 · Tableau de Quêtes](manifest/moksa/method4.md) : Le quest board PMD-Sky implémenté dans QuestBoard.
- [Comprehensive Security & Threat Analysis](manifest/security_analysis.md) : L'analyse de sécurité transversale *(attention : le document se numérote lui-même « Manifest 16 », mais le 16 canonique est « RCS » — voir note ci-dessous).*
