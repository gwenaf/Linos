# Protocole Linos

## Conventions

- Une seule connexion WebSocket par client, sur `/ws`.
- Chaque message est un objet JSON : `{ "type": "buzz", "data": { ... } }`. `data` est omis quand il n'y a rien à transmettre.
- Le serveur fait autorité : il tient l'état, les chronos, les règles et les scores. Les clients ne font qu'afficher et envoyer des intentions.
- Le serveur identifie toujours l'émetteur par sa connexion et son jeton de session, jamais par un pseudonyme fourni dans le message. Le rôle est attribué par le serveur, jamais déclaré par le client.
- Le serveur horodate à la réception les messages `buzz` et `answer`. Les points de rapidité sont calculés avec cet horodatage.
- Le chrono d'une piste démarre quand l'écran hôte envoie `media-started`, et non à l'envoi de `track-start`. Le temps de chargement du média ne pénalise donc personne.
- Reconnexion : le client renvoie `join` avec son jeton, puis le serveur répond `state` avec l'état complet de la partie.

## Pages et rôles

Le binaire sert trois pages. Le rôle d'une connexion découle de la page et de sa provenance : le serveur refuse ainsi un `validate` envoyé depuis un téléphone.

- `/control` → rôle `control` : page principale, ouverte sur le PC hôte. L'hôte y dirige la partie (configuration, lancement, pause, piste suivante), voit les réponses des joueurs, valide, affiche les indices et corrige les scores. Un bouton ouvre `/host` dans une nouvelle fenêtre. En validation `auto`, `control` dirige toujours la partie mais ne valide pas.
- `/host` → rôle `host` : affichage du jeu (TV ou second écran). Joue les médias et affiche la partie, sans aucune autorité.
- `/play` → rôle `player` : joueur sur téléphone. Il buzze, répond, choisit un thème, utilise un joker et mise.
- La page demande son rôle à la connexion : `/ws?role=control` ou `/ws?role=host` ; sans paramètre, le rôle est `player`.
- Les rôles `control` et `host` ne sont accordés qu'aux connexions provenant de la machine hôte : adresse de bouclage **et** en-tête `Host` local (protection contre le DNS rebinding). Sinon la connexion est refusée (HTTP 403).
- Messages réservés : `identify`, `buzz` → `player` ; `validate`, `skip` et autres commandes de pilotage → `control`. Un message hors rôle reçoit `error` avec le code `forbidden`.
- En partie par équipes, les points sont comptés par équipe. Un buzz ou une réponse d'un membre engage toute l'équipe.

## Messages

Sens : `C → S` client vers serveur, `S → C` serveur vers client(s).

### Connexion et lobby

- `join` (C → S) : rejoindre la partie. Contient le jeton de session s'il existe. Un seul salon par serveur : aucun nom de salon.
- `welcome` (S → C) : réponse à `join`. Contient le jeton de session (nouveau ou confirmé), le rôle attribué par le serveur et l'état de la partie (`state`).
- `state` (S → C) : état complet de la partie. Envoyé après une reconnexion ou sur demande.
- `leave` (C → S) : quitter le salon.
- `identify` (C → S) : choisir son pseudonyme.
- `join-team` (C → S) : rejoindre une équipe. Contient le nom de l'équipe ; l'équipe est créée si elle n'existe pas (8 au maximum, noms comparés sans tenir compte de la casse). Un nom vide quitte l'équipe. Une équipe vide et sans points est supprimée.
- `lobby-update` (S → C) : `players` (pseudonyme et équipe de chacun) et `teams` (noms des équipes). Statut prêt/pas prêt à venir.
- `configure` (C → S, `control`) : choisir le pack, la validation (`master` : par la page `control`, ou `auto`) et les règles modifiées pour cette partie.
- `kick` (C → S, `control`) : demander l'expulsion d'un joueur.
- `kicked` (S → C) : un joueur a été expulsé.
- `auto-ready-check` (S → C) : le serveur vérifie que tous les clients sont prêts à commencer ou continuer.
  - Un client qui n'est pas prêt répond `not-ready`.
  - Si tous les clients sont prêts, la partie peut commencer ou reprendre.
  - Si un client n'est pas joignable, l'écran hôte affiche le joueur déconnecté avec un QR code de reconnexion (vérifié avec le jeton de session).
- `ready` / `not-ready` (C → S) : le joueur signale qu'il est prêt ou non.
- `error` (S → C) : erreur. Contient un code et un message. Un message envoyé dans un état où il n'est pas accepté reçoit le code `wrong-state`.

### Partie

- `start-game` (C → S, `control`) : lancer la partie. Refusé (`no-host`) tant qu'aucun écran `host` n'est connecté.
- `end-game` (C → S, `control`) : terminer la partie manuellement.
- `game-start` (S → C) : la partie commence. Contient les paramètres de la partie et les règles effectives.
- `pause` / `resume` (C → S, `control`) : mettre en pause ou reprendre la partie.
- `game-paused` / `game-resumed` (S → C) : la partie est en pause ou reprend. Motif `control` (commande `pause`) ou `technical` : le dernier écran `host` s'est déconnecté. La partie reprend automatiquement quand un écran `host` se reconnecte. Pendant la pause, les buzz en attente sont annulés, le joueur qui a la main la garde et le chrono s'arrête.
- `abort` (C → S, `control`) : annuler la partie.
- `game-end` (S → C) : fin de partie. Contient `reason` (`ended` ou `aborted`) et `results` : équipes et joueurs sans équipe, triés par score décroissant.
- `score-adjust` (C → S, `control`) : corriger le score d'un joueur ou d'une équipe. Contient `name` (joueur) ou `team` (équipe) et `delta`.
- `score-update` (S → C) : nouveau score. Contient `name` (joueur sans équipe) ou `team`, et `score`. Les points d'un joueur en équipe vont à son équipe.

### Manche

- `round-start` (S → C) : début de manche. Contient le numéro, le nom et les règles effectives.
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

- `track-start` (S → C) : nouvelle piste.
  - L'écran hôte reçoit l'URL du média, `start`, `duration`, `playbackRate` et `reveal`.
  - Les joueurs reçoivent seulement la liste des éléments à deviner (`label` et `type`), sans les réponses.
- `media-started` (C → S, `host`) : la lecture a réellement commencé. Le chrono démarre.
- `timer-start` (S → C) : chrono lancé. Contient la durée et l'éventuelle avance du propriétaire du thème.
- `reveal` (S → C, `host`) : étape de dévoilement (son, image, flou).
- `hint` (S → C) : indice à afficher, déclenché par le chrono (manifest) ou par `show-hint`.
- `show-hint` (C → S, `control`) : afficher un indice immédiatement.
- `choices` (S → C) : réponses proposées pour un élément de type `choice`.
- `buzz-available` (S → C) : le buzz est ouvert pour le destinataire, par exemple à la fin de l'avance du propriétaire ou après un blocage.
- `buzz` (C → S) : le joueur buzze. Aucune donnée : le serveur identifie le joueur via sa session.
- `buzz-accepted` (S → C) : un joueur a la main. Contient le pseudonyme, son équipe éventuelle et le temps imparti pour répondre. Si `pauseOnBuzz` est actif, le média et le chrono sont mis en pause.
- `buzz-blocked` (S → C) : le buzz est fermé pour le destinataire.
- `answer` (C → S) : réponse à un élément. Contient le libellé de l'élément et la valeur. Le serveur identifie le joueur via sa session.
- `answer-submitted` (S → C, `control`) : réponse reçue d'un joueur. Contient le pseudonyme, l'élément, la valeur et, en validation `auto`, le verdict calculé.
- `validate` (C → S, `control`) : validation d'une réponse orale ou tapée. Contient le libellé de l'élément et `correct` (vrai ou faux).
- `answer-result` (S → C) : résultat d'une réponse. Contient le pseudonyme, l'élément, correct ou incorrect, et les points gagnés ou perdus.
- `lockout` (S → C) : un joueur est bloqué après une mauvaise réponse. Contient le pseudonyme et la durée.
- `skip` (C → S, `control`) : passer la piste.
- `track-end` (S → C) : fin de la piste. Contient les bonnes réponses et les points attribués.

## États

### Partie

- `lobby` : état initial. Les joueurs rejoignent le salon, s'identifient et choisissent une équipe. Le pack et les règles sont configurés.
- `ready` : tous les joueurs sont prêts. La partie peut démarrer.
- `in-progress` : la partie est en cours et les manches s'enchaînent.
- `paused` : pause demandée par le maître du jeu. Personne ne peut buzzer ni répondre.
- `technical-pause` : pause pour raison technique (aucun écran hôte connecté). Personne ne peut buzzer ni répondre. La déconnexion d'un joueur ne met pas la partie en pause : il se reconnecte avec son jeton.
- `game-ended` : la partie est terminée. Les résultats sont affichés.
- `game-aborted` : la partie a été annulée.

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
