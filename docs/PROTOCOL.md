# Protocole Linos

## Conventions

- Une seule connexion WebSocket par client, sur `/ws`.
- Chaque message est un objet JSON : `{ "type": "buzz", "data": { ... } }`. `data` est omis quand il n'y a rien à transmettre.
- Le serveur fait autorité : il tient l'état, les chronos, les règles et les scores. Les clients ne font qu'afficher et envoyer des intentions.
- Le serveur identifie toujours l'émetteur par sa connexion et son jeton de session, jamais par un pseudonyme fourni dans le message. Le rôle est lié au jeton.
- Le serveur horodate à la réception les messages `buzz` et `answer`. Les points de rapidité sont calculés avec cet horodatage.
- Le chrono d'une piste démarre quand l'écran hôte envoie `media-started`, et non à l'envoi de `track-start`. Le temps de chargement du média ne pénalise donc personne.
- Reconnexion : le client renvoie `join` avec son jeton, puis le serveur répond `state` avec l'état complet de la partie.

## Rôles

- `host` : écran principal (TV). Il joue les médias et affiche la partie, sans aucune autorité de validation.
- `master` : maître du jeu. Il valide les réponses et pilote la partie (pause, passer une piste, corriger un score). Il n'existe qu'en contrôle `master`. Il peut utiliser le même appareil que `host`.
- `player` : joueur sur téléphone. Il buzze, répond, choisit un thème, utilise un joker et mise.
- En partie par équipes, les points sont comptés par équipe. Un buzz ou une réponse d'un membre engage toute l'équipe.

## Messages

Sens : `C → S` client vers serveur, `S → C` serveur vers client(s).

### Connexion et lobby

- `join` (C → S) : rejoindre un salon. Contient le nom du salon et le jeton de session s'il existe.
- `welcome` (S → C) : réponse à `join`. Contient le jeton de session (nouveau ou confirmé), le rôle et l'état complet.
- `state` (S → C) : état complet de la partie. Envoyé après une reconnexion ou sur demande.
- `leave` (C → S) : quitter le salon.
- `identify` (C → S) : choisir son pseudonyme.
- `join-team` (C → S) : rejoindre une équipe. Contient le nom de l'équipe.
- `assign-team` (S → C) : un joueur est assigné à une équipe. Contient le nom de l'équipe et le pseudonyme du joueur.
- `list-teams` (S → C) : liste des équipes du salon.
- `lobby-update` (S → C) : joueurs, équipes et statut prêt/pas prêt.
- `configure` (C → S, `master` ou `host`) : choisir le pack, le contrôle (`master` ou `auto`) et les règles modifiées pour cette partie.
- `kick` (C → S, `master`) : demander l'expulsion d'un joueur.
- `kicked` (S → C) : un joueur a été expulsé.
- `auto-ready-check` (S → C) : le serveur vérifie que tous les clients sont prêts à commencer ou continuer.
  - Un client qui n'est pas prêt répond `not-ready`.
  - Si tous les clients sont prêts, la partie peut commencer ou reprendre.
  - Si un client n'est pas joignable, l'écran hôte affiche le joueur déconnecté avec un QR code de reconnexion (vérifié avec le jeton de session).
- `ready` / `not-ready` (C → S) : le joueur signale qu'il est prêt ou non.
- `error` (S → C) : erreur. Contient un code et un message.

### Partie

- `start-game` (C → S, `master`, ou `host` en contrôle `auto`) : lancer la partie.
- `game-start` (S → C) : la partie commence. Contient les paramètres de la partie et les règles effectives.
- `pause` / `resume` (C → S, `master`) : mettre en pause ou reprendre la partie.
- `game-paused` / `game-resumed` (S → C) : la partie est en pause ou reprend. Le motif est `master` ou `technical`, par exemple quand l'écran hôte est déconnecté.
- `abort` (C → S, `master`) : annuler la partie.
- `game-end` (S → C) : fin de partie. Contient les résultats, et le motif s'il s'agit d'une annulation.
- `score-adjust` (C → S, `master`) : corriger le score d'un joueur ou d'une équipe. Contient la cible et le delta de points.
- `score-update` (S → C) : nouveau score d'un joueur ou d'une équipe.

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
- `hint` (S → C) : indice à afficher.
- `choices` (S → C) : réponses proposées pour un élément de type `choice`.
- `buzz-available` (S → C) : le buzz est ouvert pour le destinataire, par exemple à la fin de l'avance du propriétaire ou après un blocage.
- `buzz` (C → S) : le joueur buzze. Aucune donnée : le serveur identifie le joueur via sa session.
- `buzz-accepted` (S → C) : un joueur a la main. Contient le pseudonyme et le temps imparti pour répondre. Si `pauseOnBuzz` est actif, le média et le chrono sont mis en pause.
- `buzz-blocked` (S → C) : le buzz est fermé pour le destinataire.
- `answer` (C → S) : réponse à un élément. Contient le libellé de l'élément et la valeur. Le serveur identifie le joueur via sa session.
- `validate` (C → S, `master`) : validation d'une réponse orale ou tapée. Contient le libellé de l'élément et `correct` (vrai ou faux).
- `answer-result` (S → C) : résultat d'une réponse. Contient le pseudonyme, l'élément, correct ou incorrect, et les points gagnés ou perdus.
- `lockout` (S → C) : un joueur est bloqué après une mauvaise réponse. Contient le pseudonyme et la durée.
- `skip` (C → S, `master`) : passer la piste.
- `track-end` (S → C) : fin de la piste. Contient les bonnes réponses et les points attribués.

## États

### Partie

- `lobby` : état initial. Les joueurs rejoignent le salon, s'identifient et choisissent une équipe. Le pack et les règles sont configurés.
- `ready` : tous les joueurs sont prêts. La partie peut démarrer.
- `in-progress` : la partie est en cours et les manches s'enchaînent.
- `paused` : pause demandée par le maître du jeu. Personne ne peut buzzer ni répondre.
- `technical-pause` : pause pour raison technique (écran hôte ou joueur déconnecté). Personne ne peut buzzer ni répondre.
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
