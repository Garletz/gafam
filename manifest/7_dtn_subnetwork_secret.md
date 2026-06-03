# 7. [SECRET] Réseau Sub-Internet & Télécommunication Temporelle (DTN)

L'architecture GAFAM Relay cache une capacité conceptuelle ultime, pensée pour l'après-Internet ou pour l'exploration spatiale. L'indépendance de chaque nœud (VPC) permet de transformer ce système de messagerie en une véritable "Backdoor" vers un **Sous-Réseau Alternatif**, totalement déconnecté de l'Internet mondial classique.

## Le Problème de l'Internet Classique (TCP/IP)
L'Internet tel que nous le connaissons exige que l'expéditeur et le destinataire puissent établir un chemin continu et rapide. S'il y a une coupure, le paquet est perdu (Timeout). Ce modèle ne fonctionne pas pour des distances extrêmes à travers l'espace, ou sur des réseaux clandestins très dégradés.

## La Solution : Le Mode DTN (Delay-Tolerant Networking)
Le VPC est architecturé autour d'une base de données de messagerie souveraine. Cela signifie qu'il est nativement conçu pour le **Store-and-Forward** (Stocker et Faire Suivre).

En connectant le VPC à des transmetteurs radio HF, des faisceaux lasers ou un réseau maillé physique alternatif, le GAFAM Relay devient un nœud de communication spatio-temporelle :
1. Tu envoies un message à travers le Web Client.
2. Le VPC sait que le destinataire n'est pas sur Terre ou que le réseau est hors-ligne.
3. Le VPC stocke le message indéfiniment.
4. Dès qu'une fenêtre d'alignement radio ou satellite s'ouvre (même quelques secondes par mois), le VPC "tire" la donnée vers le nœud suivant.

## Une Passerelle Transparente
Pour l'utilisateur, l'expérience reste identique. Il tape un texte et appuie sur envoyer. Mais en coulisse, la donnée a quitté l'Internet civil pour plonger dans ce Sous-Réseau de communication asynchrone, capable de traverser le temps et l'espace sans perte d'information.

*(Note de concept : Cette couche d'architecture est classifiée comme "Future Vision" / Phase spatiale. Elle ne nécessite pas de développement actif à ce stade du projet, mais la structure de base de données asynchrone permet techniquement cette évolution.)*


