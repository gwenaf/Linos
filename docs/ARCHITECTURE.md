# Linos — Architecture technique

Décisions techniques prises. Les specs fonctionnelles sont dans `SPECS.md`, le protocole dans `PROTOCOL.md`.

## 1. État de partie et chrono

- Tout est hébergé sur le PC hôte. Le serveur Go fait autorité sur l'état, les règles, les scores et les chronos.
- La logique de jeu vit dans `internal/game`, sans dépendance réseau ; `internal/ws` ne fait que transporter les messages. Les tests de jeu s'exécutent sans WebSocket.
- Chaque salon est géré par **une seule goroutine** propriétaire de son état. Les messages WebSocket entrants et les ticks de chrono (`time.AfterFunc`) lui parviennent par un canal. L'ordre des événements est ainsi garanti, sans mutex.
- Le chrono tourne dans le serveur. La page `/host` affiche le temps reçu du serveur. Un rechargement de page n'interrompt pas la partie.
- Le chrono d'une piste démarre à réception de `media-started` envoyé par `/host`.
- **Égalité au buzz** : les buzz reçus dans une fenêtre de 30 ms (valeur par défaut, réglable dans les règles) sont considérés comme simultanés. Le serveur en tire un au hasard.
- Pas de compensation de latence par appareil : gain faible en réseau local, complexité élevée.

## 2. Validation des réponses

1. **Normalisation** de la réponse et des réponses acceptées : minuscules, suppression des accents (`golang.org/x/text`), ponctuation supprimée sans espace (« AC/DC » devient « acdc », « a-ha » devient « aha »), espaces fusionnés.
2. **Comparaison** par distance de Levenshtein (implémentation locale, sans dépendance).
3. La réponse est acceptée si `distance / longueur ≤ fuzziness`. Valeur par défaut : 0,2, réglable via `answer.fuzziness`.
4. Aucune erreur tolérée pour les réponses de moins de 4 caractères.
5. Type `number` : acceptée si l'écart avec la bonne réponse est inférieur ou égal à `tolerance`.
6. Type `choice` : la valeur normalisée doit égaler la réponse normalisée.
7. En contrôle `master` avec réponses sur téléphone, le verdict de Linos n'est qu'une suggestion : le maître du jeu valide. En contrôle `auto`, il est appliqué directement ; le maître du jeu peut toujours corriger un score.
8. Réponses simultanées : points calculés à la réception (rapidité selon le temps de piste joué, rang selon l'ordre des bonnes réponses, au-delà de `ranks` : 0) mais appliqués en fin de piste.

Exemples :

| Saisie | Réponse attendue | Distance | Résultat |
|---|---|---|---|
| `the les i know the beter` | `the less i know the better` | 2 / 26 | accepté |
| `dido` | `daho` | 2 / 4 | refusé |

## 3. Médias

### Lecture

- Les médias sont lus **uniquement par la page `/host`**, sur le PC hôte, via `<audio>` / `<video>`. La route `/media/…` refuse toute requête qui ne vient pas du PC hôte : un téléphone pourrait sinon récupérer l'extrait et l'identifier. Seuls les fichiers référencés par le manifest sont servis (jamais `manifest.json`, qui contient les réponses).
- Le serveur sert les médias avec `http.ServeContent`, qui gère les requêtes HTTP Range. Le navigateur ne télécharge que les portions nécessaires pour atteindre `start` et lire l'extrait.
- Les médias d'un gamepack sont lus sans extraction (voir « Gamepack »).
- Les fichiers importés par glisser-déposer sont lus directement depuis le disque.
- Formats garantis : MP3, AAC, MP4 (H.264/AAC), JPEG, PNG, WebP. Un format non lisible par le navigateur affiche « format non supporté, convertir avec ffmpeg ».

### Gamepack

- Deux formes acceptées dès la v1, avec le même contenu :
  - fichier `.linospack` : une archive ZIP (ZIP64 supporté), forme de distribution ;
  - dossier non zippé contenant `manifest.json` : forme d'édition (création à la main ou `/edit` avant export).
- Les deux formes sont lues par le même code via `fs.FS` (`zip.Reader` ou `os.DirFS`). Seul l'accès direct aux médias diffère : `http.ServeContent` sur le fichier pour un dossier, `DataOffset` + `io.SectionReader` pour un `.linospack`.
- Bibliothèque : dossier `packs/` à côté de l'exécutable, pour rester portable sur clé USB.
- Organisation :

  ```
  manifest.json   à la racine, compression autorisée
  media/…         médias stockés sans compression (méthode Store)
  cover.jpg       facultatif, déclaré dans le manifest (champ "cover")
  ```

- Lecture d'un média : `archive/zip` (`DataOffset`) + `io.SectionReader`, servi par `http.ServeContent` (requêtes Range). Un média compressé dans l'archive est refusé au chargement (l'export écrit en `Store`) : pas de cache d'extraction.
- Validation au chargement :
  - chemins de médias relatifs uniquement, `..` et chemins absolus refusés (protection zip slip) ;
  - chaque `id` de piste ou de thème référencé existe, chaque fichier média est présent ;
  - manifest décodé dans des structures Go qui refusent les champs inconnus ;
  - `version` plus récente que celle supportée : pack refusé avec un message explicite ;
  - valeurs énumérées (modes, types, sélections) et bornes (`min` ≤ `max`, `fuzziness` entre 0 et 1) contrôlées ;
  - toutes les erreurs sont renvoyées en une fois, préfixées par leur chemin (`tracks[2].guesses[0].answer`).
- Intégrité : le CRC32 du ZIP est vérifié pour `manifest.json` (lu via `archive/zip`), pas pour les médias lus en place. Un média corrompu joue mal mais ne peut pas lire hors de son entrée. Pas de signature de pack.
- Import rapide (`/control`, PC hôte) : glisser-déposer d'un dossier ou de fichiers audio (mp3, m4a, aac, ogg, opus, flac, wav), envoyés à `POST /api/import` (PC hôte, même origine) et enregistrés dans un nouveau pack dossier `packs/import-<date>/`. Le `manifest.json` est généré depuis les tags (`dhowden/tag`) ou, à défaut, le nom « Artiste - Titre » (numéro de piste retiré) : éléments « Artiste » et « Titre », réponses acceptées avec et sans les extras (« (Remastered) », « feat. »). Options : début et durée des extraits, réponses sur téléphone en contrôle `auto`. Le pack importé reste réutilisable et modifiable ; un import sans fichier audio ne laisse rien.
- `duration` d'une piste prime sur la durée des règles.
- Déroulé : à `start-game`, les manches sont aplaties en liste de pistes (`sequence` dans l'ordre, `random` tiré parmi les thèmes sans rejouer une piste). Règles effectives d'une piste : valeurs par défaut, puis règles du pack, puis de la manche, puis barème de l'élément deviné.
- Avant chaque piste, le serveur enchaîne : choix du thème (manches `theme-pick`, pistes tirées au moment du choix), puis mises (barème `wager`), puis la piste. `skip` abandonne ces étapes.
- Un seul calcul de points pour le buzz et les réponses simultanées : barème (rapidité, fixe, rang, mise), bonus `othersBonus` pour une autre équipe que le propriétaire du thème, puis joker double sur les points positifs.
- Fin de manche : `round-end`, puis éliminations (`eliminate`). Fin de partie : pistes épuisées (avec mort subite éventuelle), score cible atteint à la fin d'une piste, ou dernière équipe restante.
- Toutes les règles du manifest sont jouées ; seules les combinaisons impossibles sont refusées au `configure` (voir `PROTOCOL.md`). Le manifest d'exemple est jouable (vérifié par un test).
- Essais en mode buzz, par élément à deviner : `attempts` par équipe, `rebound` (`none` termine la piste, `others` donne la priorité aux autres, `all` laisse tout le monde rebuzzer), `wrongLockout` (blocage en temps de piste joué), `reboundBonus` pour une bonne réponse après une erreur. Tout est remis à zéro quand un élément est trouvé. En réponses simultanées, `attempts` et `wrongLockout` s'appliquent par équipe et par élément.

### Éditeur `/edit`

- Intégré à Linos : mêmes types Go (`internal/pack`) que le jeu, donc aucun contrat à synchroniser.
- Réservé au PC hôte (`hostOnly`, comme `/control`) ; `LINOS_EDIT=off` le désactive (serveur auto-hébergé).
- N'écrit que dans les packs dossier de `packs/`. Un `.linospack` se modifie par une copie décompressée en dossier, à réexporter.
- Effets (flou, pixelisation, image fixe…) : données du manifest appliquées en direct par `/host`, jamais incrustées dans le média.
- Export : ffmpeg facultatif, cherché dans le PATH, découpe chaque média aux segments joués (fondu de sortie compris) puis écrit le `.linospack`. Pas de ffmpeg dans le binaire. Sans ffmpeg, l'export copie les médias entiers.

### Sources de médias

- Le jeu lit **uniquement des fichiers locaux** contenus dans un gamepack ou importés par glisser-déposer.
- Aucune plateforme de streaming, les conditions d'utilisation interdisant cet usage :
  - Spotify : « Do not create a game, including trivia quizzes » ;
  - Apple (iTunes Search API) : extraits réservés à la promotion du store, « not for entertainment purposes » ;
  - SoundCloud : « You must not use clips of User Content to create games » ;
  - YouTube : publicités et masquage du lecteur incompatibles ;
  - Deezer : création d'applications fermée, stockage hors ligne interdit.
- Diffusion publique (bar, événement) : une licence de diffusion (SACEM en France) reste à la charge de l'organisateur.

## 4. Front

- **Preact** avec **`@preact/signals`**, build Vite, embarqué dans le binaire via `embed`.
- Trois routes : `/control` (pilotage, PC hôte), `/host` (affichage, TV) et `/play` (mobile), servies par un même `index.html` ; chaque page est un chunk chargé à la demande (un téléphone ne télécharge que `/play`). `/` redirige vers `/control` sur le PC hôte, vers `/play` ailleurs.
- `/api/join` (adresses de connexion) et `/qr.png?data=…` (QR code généré en Go, `skip2/go-qrcode`) sont réservés au PC hôte.
- L'écran `/host` exige un clic avant de jouer (politique d'autoplay). Il tient une horloge locale calquée sur celle du serveur (arrêtée en pause et pendant une réponse) pour le compte à rebours, les dévoilements et les indices ; le serveur reste la référence pour le score.
- `web/dist/.gitkeep` est versionné (recopié depuis `web/public/` à chaque build) pour que `go build` fonctionne avant tout build du front.
- Rôles `control` et `host` réservés aux connexions depuis l'adresse de bouclage (sinon HTTP 403). Exception : un téléphone maître du jeu obtient `control` via une invitation à usage unique (2 min) créée depuis `control`, puis un jeton de reconnexion. Cas d'usage : un seul écran, `/host` en plein écran sur le PC.

## 5. Réseau, QR code et sessions

### Adresse du QR code

- **Wi-Fi existant** : IP locale obtenue via `net.Dial("udp", "8.8.8.8:80")` puis `LocalAddr()`. Aucun paquet n'est envoyé ; le système choisit l'interface de sortie.
- **Point d'accès Windows (v1)** : l'utilisateur active le partage de connexion manuellement. Linos détecte l'interface correspondante (généralement `192.168.137.1`) et l'utilise dans le QR code.
- Pas de tunnel ni de jeu à distance : la latence 4G fausse le buzz face aux joueurs en Wi-Fi, et l'installation de `cloudflared` ajoute une étape.
- Le QR code contient toujours l'adresse IP. `blindtest.local` (mDNS) n'est qu'un confort pour la saisie manuelle : sa résolution n'est pas fiable sur tous les Android.

### Jetons de session

- Le QR code du salon ne contient **aucun jeton** : il est visible par tous.
- Le serveur génère le jeton au `join` : 32 octets via `crypto/rand`, encodés en base64url. Le client le conserve dans `localStorage`.
- Le jeton identifie le joueur et porte son rôle. Il permet la reconnexion.
- Le QR code de reconnexion affiché pour un joueur déconnecté contient un **jeton de reconnexion à usage unique**, valable 2 minutes.

### Pare-feu

Au premier lancement, Windows demande l'autorisation d'accès réseau pour l'exécutable. Un refus crée une règle de blocage. Une autorisation limitée aux réseaux privés bloque les téléphones si le Wi-Fi est classé « public ». La règle étant liée au chemin de l'exécutable, un changement de lettre de la clé USB redéclenche la demande. Un test de connexion depuis le PC hôte ne traverse pas le pare-feu : seul un téléphone peut le vérifier.

v1 :

1. **Profil réseau** : `GET /api/network` lit `Get-NetConnectionProfile` (sans droits admin). Profil « Public » : avertissement dans `/control`.
2. **Aucun joueur après 30 s** : aide affichée dans `/control` (test `/health` depuis le téléphone, même Wi-Fi, autorisation « Réseau local » des navigateurs iOS autres que Safari, pare-feu, isolation des clients). Aussi accessible par un bouton.
3. **Bouton « Autoriser Linos dans le pare-feu »** : `POST /api/firewall` (PC hôte uniquement, requête d'une autre origine refusée) lance `netsh advfirewall firewall add rule … program=<chemin>` via une élévation UAC.

**Isolation des clients** (constatée sur une Bbox : le PC ne résout même pas l'adresse MAC du téléphone) : fréquente sur les box et quasi systématique sur les Wi-Fi publics. Contournements proposés par l'aide :

- point d'accès mobile Windows : l'adresse `192.168.137.1` est détectée et signalée dans `/control`. Limité à 8 clients par défaut ; la valeur DWORD `WifiMaxPeers` (jusqu'à 128) dans `HKLM\SYSTEM\CurrentControlSet\Services\icssvc\Settings` relève la limite (non documenté par Microsoft, redémarrage requis) ;
- désactivation de l'isolation ou du mode invité sur la box ;
- mini-routeur de voyage sans isolation, recommandé pour jouer à 30 ou plus hors de chez soi.

macOS affiche une demande similaire, répétée à chaque version pour une application non signée. Linux n'a généralement pas de pare-feu actif par défaut.

### Docker

Image `ghcr.io/gwenaf/linos:<version>` (publiée à chaque release, `Dockerfile` à la racine) destinée aux utilisateurs avancés qui installent Linos sur leur serveur : `docker run -d --network host -v linos:/data ghcr.io/gwenaf/linos:<version>`, packs dans `packs/` du volume, éditeur désactivé. Le conteneur doit tourner en réseau `host` (`--network host`) : sans cela, il ne voit pas l'IP du réseau local (QR code faux) et l'annonce mDNS ne sort pas. Le pare-feu relève alors de l'administrateur du serveur. `/control` et `/host` n'étant accordés qu'en bouclage, le pilotage à distance passe par l'invitation maître du jeu ; `/host` reste local au serveur.

## 6. Journalisation

- `log/slog` (stdlib) en JSON, vers la console et `linos.log` (dossier `LINOS_HOME`, par défaut celui de l'exécutable). Pas de SDK OpenTelemetry : une vingtaine de dépendances et plusieurs Mo pour un collecteur qu'une appli hors ligne n'a pas. Le format JSON structuré reste importable dans un outil compatible OpenTelemetry.
- Niveau `info` par défaut : démarrage (dossiers, adresses), connexions WebSocket, arrivées et départs, événements de partie (pack, lancement, pistes, buzz, réponses, pauses, exclusions, fin), erreurs renvoyées aux clients, requêtes HTTP en échec. `LINOS_LOG_LEVEL=debug` ajoute chaque requête HTTP et chaque message reçu.
- Jamais de jeton dans les journaux.
- Plantage du salon : l'événement `room crashed` (valeur et pile d'appels) est écrit avant l'arrêt du programme.
- Erreurs JavaScript des pages (`error`, `unhandledrejection`, échec de chargement) envoyées par `POST /api/log` (8 Ko max) et journalisées en `front error` avec la page et le navigateur.

## 7. Points de vigilance

- **HTTP en réseau local** : l'API Wake Lock exige HTTPS. Les téléphones peuvent se mettre en veille et couper le WebSocket ; la reconnexion par jeton couvre ce cas.
- **Lecture automatique** : les navigateurs bloquent le son sans interaction préalable. La page `/host` doit obtenir un clic (par exemple « Lancer la partie ») avant la première piste.
