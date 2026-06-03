# 3. Sign-In with GAFAM Relay (SSO Décentralisé)

Une fois le réseau GAFAM Relay en place, le Hardware Relay et le VPC ouvrent la porte à une fonctionnalité dérivée révolutionnaire : **remplacer le "Sign-in with Google" ou "Sign-in with Apple"**.

## Le Problème Actuel
Aujourd'hui, pour s'inscrire sur un site web, on utilise soit un compte GAFAM (qui traque toutes nos connexions), soit un numéro de téléphone avec l'envoi d'un code SMS de validation (OTP) peu sécurisé et contraignant.

## La Solution "GAFAM Relay"
Puisque ton Client Web est déjà connecté de manière cryptographique à ton Hardware Relay (via le scan de la caméra ou l'appui sur le bouton physique), tu possèdes **une preuve d'identité cryptographique infaillible liée à ton numéro de téléphone**.

Si des sites web tiers intègrent l'API GAFAM Relay, le flux devient magique :
1. Tu arrives sur un nouveau site web et tu cliques sur **"Se connecter avec son Numéro"**.
2. Le site interroge silencieusement ton Client Web VPC actif dans ton navigateur.
3. Le VPC confirme instantanément que tu es déjà authentifié (session active validée par ton boîtier matériel).
4. **Tu es connecté immédiatement.** Aucun SMS avec un code à 6 chiffres n'est envoyé. Aucun mot de passe n'est créé.
5. **Privacy First** : Le site web reçoit *uniquement* ton numéro de téléphone en guise d'identifiant (et aucune autre donnée personnelle comme l'e-mail, l'âge ou le nom, contrairement à Google ou Facebook).

Le Hardware Relay devient donc non seulement ton routeur de SMS, mais aussi ton **trousseau de clés universel (SSO)** pour tout Internet, hébergé chez toi.
