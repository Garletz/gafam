# 2. Le Protocole Fédéré (Messagerie)

La seule chose qui est mise en commun entre tous les utilisateurs, c'est un **Protocole de Communication standardisé**. L'identifiant unique d'un membre sur ce réseau reste son numéro de téléphone classique.

## Le Flux de Messagerie Intelligent (Routing)

Au lieu de dépendre aveuglément des opérateurs télécoms, le système agit comme un routeur intelligent de type "Matrix".

1. **Intention** : L'utilisateur A tape un message pour le numéro de l'utilisateur B sur son Client Web personnel.
2. **Découverte (Discovery)** : Le VPC de A reçoit l'ordre et interroge un annuaire distribué (DHT / Registre public chiffré) : *"Ce numéro de téléphone correspond-il à un VPC connu dans le réseau ?"*
3. **Cas Fédéré (B possède son propre système GAFAM Relay)** : 
   - Les deux VPC (celui de A et celui de B) établissent un tunnel chiffré P2P. 
   - Le message est transmis via Internet, chiffré de bout en bout. 
   - Le réseau cellulaire des opérateurs classiques est totalement ignoré.
4. **Cas Fallback (B est un utilisateur de smartphone standard)** : 
   - Le VPC de A ordonne à son Hardware Relay (le boîtier chez lui) d'envoyer le message via la carte SIM sur le réseau téléphonique traditionnel (SMS / RCS). 
   - Le message arrivera sur le téléphone de B de manière transparente, comme un SMS habituel.
