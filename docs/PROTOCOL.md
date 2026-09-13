# Protocole Linos

## Conventions

- Une seule connexion WebSocket par client, sur `/ws`.
- Chaque message est un objet JSON : `{ "type": "buzz", "data": { ... } }`. `data` est omis quand il n'y a rien à transmettre.
- Le serveur fait autorité : il tient l'état, les chronos, les règles et les scores. Les clients ne font qu'afficher et envoyer des intentions.
- Le serveur identifie toujours l'émetteur par sa connexion et son jeton de session, jamais par un pseudonyme fourni dans le message. Le rôle est attribué par le serveur, jamais déclaré par le client.
- Le serveur horodate à la réception les messages `buzz` et `answer`. Les points de rapidité sont calculés avec cet horodatage.
- Le chrono d'une piste démarre quand l'écran hôte envoie `media-started`, et non à l'envoi de `track-start`. Le temps de chargement du média ne pénalise donc personne.
- Reconnexion : le client renvoie `join` avec son jeton ; après `welcome`, le serveur envoie `state`, l'état complet de la partie. Une page rechargée reprend donc là où en est la partie.

## Pages et rôles

Le binaire sert trois pages. Le rôle d'une connexion découle de la page et de sa provenance : le serveur refuse ainsi un `validate` envoyé depuis un téléphone.

- `/control` → rôle `control` : page principale, ouverte sur le PC hôte. L'hôte y dirige la partie (configuration, lancement, pause, piste suivante), voit les réponses des joueurs, valide, affiche les indices et corrige les scores. Un bouton ouvre `/host` dans une nouvelle fenêtre. En validation `auto`, `control` dirige toujours la partie mais ne valide pas.
- `/host` → rôle `host` : affichage du jeu (TV ou second écran). Joue les médias et affiche la partie, sans aucune autorité.
- `/play` → rôle `player` : joueur sur téléphone. Il buzze, répond, choisit un thème, utilise un joker et mise.
- La page demande son rôle à la connexion : `/ws?role=control` ou `/ws?role=host` ; sans paramètre, le rôle est `player`.
- Les rôles `control` et `host` ne sont accordés qu'aux connexions provenant de la machine hôte : adresse de bouclage **et** en-tête `Host` local (protection contre le DNS rebinding). Sinon la connexion est refusée (HTTP 403).
- **Téléphone maître du jeu** : pour jouer avec un seul écran (`/host` en plein écran sur le PC), `control` crée une invitation (`master-invite`) affichée en QR code. Le téléphone se connecte sans paramètre de rôle et envoie `join` avec `invite` : il obtient le rôle `control` et un jeton de maître du jeu. L'invitation est à usage unique et expire après 2 minutes ; le jeton permet ensuite de se reconnecter.
- Messages réservés : `identify`, `buzz` → `player` ; `validate`, `skip` et autres commandes de pilotage → `control`. Un message hors rôle reçoit `error` avec le code `forbidden`.
- En partie par équipes, les points sont comptés par équipe. Un buzz ou une réponse d'un membre engage toute l'équipe.

## Messages

Sens : `C → S` client vers serveur, `S → C` serveur vers client(s).

### Connexion et lobby

- `join` (C → S) : rejoindre la partie. Contient `token` (jeton de session ou de maître du jeu) s'il existe, ou `invite` (code d'invitation maître du jeu). Un seul salon par serveur : aucun nom de salon. Invitation inconnue ou expirée : `error` `invalid-invite`.
- `welcome` (S → C) : réponse à `join`. Contient le jeton (nouveau ou confirmé), le rôle attribué par le serveur, le pseudonyme éventuel et l'état de la partie (`state`).
- `master-invite` (C → S, `control`) : créer une invitation maître du jeu. Le serveur répond `master-invite` avec `code` et `expiresIn` (secondes).
- `state` (S → C) : état complet de la partie, envoyé juste après chaque `welcome`, adapté au rôle. Contient toujours `state`, `lobby` (comme `lobby-update`), `scores` (comme `results` de `game-end`) et `configured` si un pack est choisi. Pendant une partie, contient aussi `track` (comme `track-start` pour ce rôle), `trackState` (`loading`, `live`, `ended`), `found` (éléments trouvés), `round`, `paused` (comme `game-paused`), `holder` (comme `buzz-accepted`), `elapsed` (secondes de piste jouées, si `live`), `trackEnd` (comme `track-end`, si `ended`) et, pour un joueur, `canBuzz`, `canAnswer` et `answered` (éléments auxquels son équipe a déjà répondu, en mode simultané).
- `leave` (C → S) : quitter le salon.
- `identify` (C → S) : choisir son pseudonyme.
- `join-team` (C → S) : rejoindre une équipe. Contient le nom de l'équipe ; l'équipe est créée si elle n'existe pas (8 au maximum, noms comparés sans tenir compte de la casse). Un nom vide quitte l'équipe. Une équipe vide et sans points est supprimée.
- `lobby-update` (S → C) : `players` (pour chacun : `name`, `team`, `ready`, `connected`), `teams` (noms des équipes) et `state`. Envoyé à chaque changement de joueur, d'équipe, de statut prêt ou de connexion.
- `list-packs` (C → S, `control`) : lister les packs du dossier `packs/`. Réponse `packs` : pour chacun `name`, `title` et `error` s'il est invalide ou utilise une fonction pas encore gérée.
- `configure` (C → S, `control`, en `lobby` ou `ready`) : choisir le pack (`pack` : nom dans `packs/`) et le contrôle (`control` : `master`, le maître du jeu valide, ou `auto`, Linos valide ; par défaut `game.control.default` du pack, sinon `master`). Diffuse `configured` avec `pack`, `control`, `title`, `author`, `rounds` (noms) et `tracks` (nombre). Pack introuvable, invalide, utilisant une fonction pas encore gérée ou combinaison impossible (contrôle non autorisé par le pack, réponses orales en `auto`, réponses simultanées orales, classement sans réponses simultanées) : `error` `invalid-pack` avec le détail.
- `kick` (C → S, `control`) : expulser un joueur, dans tout état. Contient `name`. Le joueur est retiré (son jeton ne vaut plus rien), son équipe le perd, et s'il avait la main le buzz se rouvre.
- `kicked` (S → C) : un joueur a été expulsé. Contient `name`. Le téléphone expulsé reçoit le message puis sa connexion est fermée.
- `ready` (C → S, `player`, en `lobby` ou `ready`) : le joueur signale qu'il est prêt ou non. Contient `ready` (vrai ou faux, obligatoire). Réservé aux joueurs identifiés.
- `error` (S → C) : erreur. Contient un code et un message. Un message envoyé dans un état où il n'est pas accepté reçoit le code `wrong-state`.

### Partie

- `start-game` (C → S, `control`, en `ready`) : lancer la partie. Refusé (`no-host`) tant qu'aucun écran `host` n'est connecté. Remet les scores à zéro.
- `end-game` (C → S, `control`) : terminer la partie manuellement.
- `game-start` (S → C) : la partie commence. Contient `title`, `control` et `tracks` (nombre de pistes jouées). Refusé sans pack (`no-pack`) ou si les manches ne sélectionnent aucune piste (`empty-playlist`).
- `pause` / `resume` (C → S, `control`) : mettre en pause ou reprendre la partie. `resume` est aussi accepté en pause technique : la partie continue sans les joueurs absents, tolérés jusqu'à leur retour. Sans écran `host`, `resume` laisse la partie en pause technique.
- `game-paused` (S → C) : la partie est en pause. `reason` vaut `control` (commande `pause`) ou `technical`. En pause technique, contient aussi `hostMissing` et `missingPlayers` ; renvoyé à chaque changement de connexion. Pendant une pause, les buzz en attente sont annulés, le joueur qui a la main la garde et le chrono s'arrête.
- `game-resumed` (S → C) : la partie reprend, automatiquement quand l'écran hôte et les joueurs absents sont revenus, ou sur `resume`.
- `abort` (C → S, `control`) : annuler la partie.
- `game-end` (S → C) : fin de partie. Contient `reason` (`ended` ou `aborted`) et `results` : équipes et joueurs sans équipe, triés par score décroissant. Suivi d'un `lobby-update` : retour au lobby.
- `score-adjust` (C → S, `control`) : corriger le score d'un joueur ou d'une équipe. Contient `name` (joueur) ou `team` (équipe) et `delta`.
- `score-update` (S → C) : nouveau score. Contient `name` (joueur sans équipe) ou `team`, et `score`. Les points d'un joueur en équipe vont à son équipe.

### Manche

- `round-start` (S → C) : début de manche, juste avant sa première piste. Contient `index` et `name`. Absent pour un pack sans manches.
- `theme-pick-request` (S → C) : une équipe doit choisir un thème. Contient l'équipe et les thèmes disponibles.
- `pick-theme` (C → S) : choix du thème par l'équipe désignée.
- `theme-picked` (S → C) : thème choisi. L'équipe devient propriétaire de la piste qui suit.
- `wager-request` (S → C) : les joueurs ou équipes doivent miser. Contient la mise maximale.
- `wager` (C → S) : montant misé.
- `use-joker` (C → S) : utiliser un joker avant la piste. Contient le type de joker.
- `joker-used` (S → C) : un joker a été utilisé.
- `eliminated` (S → C) : joueurs ou équipes éliminés.
- `round-end` (S → C) : fin de manche. Contient le numéro et les résultats.

### Piste

- `track-start` (S → C) : nouvelle piste, en état « chargement » : le buzz reste fermé. Tous reçoivent `index`, `total`, `round` (-1 sans manches), `duration`, `mode` (`buzz` ou `simultaneous`), `via` (`oral` ou `device`) et `guesses`.
  - L'écran hôte reçoit en plus `media` (URL `/media/…`), `start`, `playbackRate` (1 par défaut), `reveal` et `hints` ; il gère lui-même dévoilements et indices sur son lecteur.
  - Les joueurs ne reçoivent que `label`, `type` et, pour un `choice`, `choices` et `choicesAt`. `control` reçoit les éléments complets, réponses comprises.
- `media-started` (C → S, `host`) : la lecture a réellement commencé. Le chrono de la piste démarre et le buzz s'ouvre. Hors chargement : `error` `wrong-state`.
- `timer-start` (S → C) : chrono lancé. Contient `index` et `duration`. Le chrono s'arrête pendant les pauses et pendant qu'un joueur a la main ; la piste se termine quand `duration` a été jouée.
- `reveal` (S → C, `host`) : étape de dévoilement (son, image, flou).
- `hint` (S → C) : indice à afficher, déclenché par le chrono (manifest) ou par `show-hint`.
- `show-hint` (C → S, `control`) : afficher un indice immédiatement.
- `choices` (S → C) : réponses proposées pour un élément de type `choice`.
- `buzz-available` (S → C) : le buzz est ouvert pour le destinataire, par exemple à la fin de l'avance du propriétaire ou après un blocage.
- `buzz` (C → S) : le joueur buzze. Aucune donnée : le serveur identifie le joueur via sa session. En mode `simultaneous` : `error` `no-buzz`.
- `buzz-accepted` (S → C) : un joueur a la main. Contient le pseudonyme, son équipe éventuelle et le temps imparti pour répondre. Si `pauseOnBuzz` est actif, le média et le chrono sont mis en pause.
- `buzz-blocked` (S → C) : le buzz est fermé pour le destinataire.
- `answer` (C → S, `player`) : réponse tapée ou choisie sur le téléphone, pour une piste `via` `device` en cours (sinon `oral-answers` ou `wrong-state`). Contient `guess` (libellé ; par défaut le premier élément restant) et `value`. En mode `buzz`, réservé au joueur qui a la main, une réponse par main (`not-your-turn`) : en contrôle `auto`, le serveur valide aussitôt (`answer-result`) ; en `master`, il transmet `answer-submitted` et attend `validate`. En mode `simultaneous`, chaque équipe (ou joueur sans équipe) répond une fois à chaque élément (`unknown-guess` sinon) ; la piste se termine quand tous les joueurs connectés ont répondu à tout.
- `answer-submitted` (S → C, `control`) : réponse du joueur qui a la main, en mode `buzz` avec maître du jeu. Contient `name`, `team`, `guess`, `value` et `correct` (verdict proposé par Linos). Le maître du jeu décide avec `validate`.
- `validate` (C → S, `control`) : validation de la réponse du joueur qui a la main (orale, ou tapée et transmise par `answer-submitted`). Contient `correct` (obligatoire) et `guess` (libellé de l'élément trouvé ; par défaut le premier non trouvé). Élément inconnu ou déjà trouvé : `error` `unknown-guess`. En contrôle `auto` : `error` `auto-control`.
- `answer-result` (S → C) : résultat d'une réponse. Contient `name`, `guess` (vide si faux), `correct` et `points` (rapidité, fixe ou classement selon le barème de l'élément ; pénalité si faux). En mode `buzz`, diffusé à tous ; bonne réponse avec d'autres éléments restants : le buzz se rouvre pour tous. En mode `simultaneous`, envoyé seulement à l'équipe qui a répondu et à `control`, avec `value` ; les points sont appliqués (`score-update`) à la fin de la piste, pour ne pas révéler qui a trouvé.
- `answered` (S → C) : en mode `simultaneous`, un joueur a répondu à un élément. Contient `name`, `team` et `guess`, jamais la justesse de la réponse.
- `lockout` (S → C) : un joueur est bloqué après une mauvaise réponse. Contient le pseudonyme et la durée.
- `skip` (C → S, `control`) : passer à la piste suivante, en terminant la piste en cours. Après la dernière piste, la partie se termine (`game-end`).
- `track-end` (S → C) : fin de la piste. Contient `index`, `reason` (`found` : tout trouvé, `answered` : tout le monde a répondu, `time` : durée écoulée, `skipped`) et `guesses` avec les réponses. La piste suivante attend `skip`.

## États

### Partie

- `lobby` : état initial. Les joueurs rejoignent le salon, s'identifient, choisissent une équipe et se déclarent prêts. Le pack et les règles sont configurés.
- `ready` : tous les joueurs identifiés et connectés sont prêts (au moins un). Seul état où `start-game` est accepté. Le serveur bascule automatiquement entre `lobby` et `ready`.
- `in-progress` : la partie est en cours et les manches s'enchaînent.
- `paused` : pause demandée par le maître du jeu. Personne ne peut buzzer ni répondre.
- `technical-pause` : pause automatique quand l'écran hôte ou un joueur identifié perd la connexion. Personne ne peut buzzer ni répondre. Sortie automatique quand tout le monde est revenu, ou manuelle par `control` : `kick` du joueur absent, ou `resume` pour continuer sans lui (impossible sans écran hôte).
- Fin de partie (`end-game` ou `abort`) : `game-end` publie les résultats, puis la partie revient en `lobby`, tous les joueurs « pas prêts ». Équipes et joueurs sont conservés ; les scores repartent à zéro au prochain `start-game`.

### Manche (uniquement pendant `in-progress`)

- `round-intro` : présentation de la manche et de ses règles.
- `theme-pick` : une équipe choisit un thème.
- `wager` : les joueurs misent.
- `playing` : les pistes s'enchaînent.
- `round-ended` : la manche est terminée. Les résultats et éliminations sont affichés.

### Piste (uniquement pendant `playing`)

- `loading` : le média se charge sur l'écran hôte.
- `live` : le média joue et le chrono tourne. Les joueurs peuvent buzzer ou répondre selon les règles.
- `answering` : un joueur a la main après un buzz. Le média et le chrono peuvent être en pause.
- `track-ended` : la piste est terminée. Les réponses et les points sont affichés.
