# Linos — Architecture technique

Décisions techniques prises. Les specs fonctionnelles sont dans `SPECS.md`, le protocole dans `PROTOCOL.md`.

## 1. État de partie et chrono

- Tout est hébergé sur le PC hôte. Le serveur Go fait autorité sur l'état, les règles, les scores et les chronos.
- Chaque salon est géré par **une seule goroutine** propriétaire de son état. Les messages WebSocket entrants et les ticks de chrono (`time.AfterFunc`) lui parviennent par un canal. L'ordre des événements est ainsi garanti, sans mutex.
- Le chrono tourne dans le serveur. La page `/host` affiche le temps reçu du serveur. Un rechargement de page n'interrompt pas la partie.
- Le chrono d'une piste démarre à réception de `media-started` envoyé par `/host`.
- **Égalité au buzz** : les buzz reçus dans une fenêtre de 30 ms (valeur par défaut, réglable dans les règles) sont considérés comme simultanés. Le serveur en tire un au hasard.
- Pas de compensation de latence par appareil : gain faible en réseau local, complexité élevée.

## 2. Validation des réponses

1. **Normalisation** de la réponse et des réponses acceptées : minuscules, suppression des accents (`golang.org/x/text/unicode/norm`), suppression de la ponctuation, espaces fusionnés.
2. **Comparaison** par distance de Levenshtein (implémentation locale, sans dépendance).
3. La réponse est acceptée si `distance / longueur ≤ fuzziness`. Valeur par défaut : 0,2, réglable via `answer.fuzziness`.
4. Aucune erreur tolérée pour les réponses de moins de 4 caractères.
5. Type `number` : acceptée si l'écart avec la bonne réponse est inférieur ou égal à `tolerance`.
6. En contrôle `master`, le maître du jeu peut toujours corriger une validation.

Exemples :

| Saisie | Réponse attendue | Distance | Résultat |
|---|---|---|---|
| `the les i know the beter` | `the less i know the better` | 2 / 26 | accepté |
| `dido` | `daho` | 2 / 4 | refusé |

## 3. Médias

### Lecture

- Les médias sont lus **uniquement par la page `/host`**, sur le PC hôte, via `<audio>` / `<video>`. Ils ne transitent jamais vers les téléphones.
- Le serveur sert les médias avec `http.ServeContent`, qui gère les requêtes HTTP Range. Le navigateur ne télécharge que les portions nécessaires pour atteindre `start` et lire l'extrait.
- Les médias d'un gamepack sont lus sans extraction (voir « Gamepack »).
- Les fichiers importés par glisser-déposer sont lus directement depuis le disque.
- Formats garantis : MP3, AAC, MP4 (H.264/AAC), JPEG, PNG, WebP. Un format non lisible par le navigateur affiche « format non supporté, utiliser le builder ».

### Gamepack

- Deux formes acceptées dès la v1, avec le même contenu :
  - fichier `.linospack` : une archive ZIP (ZIP64 supporté), forme de distribution ;
  - dossier non zippé contenant `manifest.json` : forme d'édition (création à la main, builder avant export).
- Les deux formes sont lues par le même code via `fs.FS` (`zip.Reader` ou `os.DirFS`). Seul l'accès direct aux médias diffère : `http.ServeContent` sur le fichier pour un dossier, `DataOffset` + `io.SectionReader` pour un `.linospack`.
- Bibliothèque : dossier `packs/` à côté de l'exécutable, pour rester portable sur clé USB.
- Organisation :

  ```
  manifest.json   à la racine, compression autorisée
  media/…         médias stockés sans compression (méthode Store)
  cover.jpg       facultatif, déclaré dans le manifest (champ "cover")
  ```

- Lecture d'un média : `archive/zip` (`DataOffset`) + `io.SectionReader`, servi par `http.ServeContent` (requêtes Range). Un média compressé par erreur est extrait une fois dans un cache local.
- Validation au chargement :
  - chemins de médias relatifs uniquement, `..` et chemins absolus refusés (protection zip slip) ;
  - chaque `id` de piste ou de thème référencé existe, chaque fichier média est présent ;
  - manifest décodé dans des structures Go qui refusent les champs inconnus ;
  - `version` plus récente que celle supportée : pack refusé avec un message explicite.
- Intégrité : CRC32 intégré au ZIP. Pas de signature de pack.
- Import de mp3 par glisser-déposer : manifest généré en mémoire, aucun fichier créé.

### Builder (projet séparé)

- Linos ne découpe ni ne convertit aucun média : pas de ffmpeg dans le binaire.
- Le builder embarque ffmpeg. Il découpe les extraits sans réencodage (un clip de 2 Go devient un extrait de quelques Mo) et convertit les formats non compatibles vers H.264/AAC.
- Le builder importe des playlists pour générer le manifest.
- Contrat commun : JSON Schema du manifest, publié dans ce dépôt.

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
- Deux routes : `/host` (TV) et `/play` (mobile).

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

1. **Profil réseau** : lecture via `Get-NetConnectionProfile` (sans droits admin). Profil « public » : avertissement sur l'écran hôte avec la marche à suivre.
2. **Aucun joueur après 30 s** : aide affichée sur l'écran hôte (pare-feu, même réseau Wi-Fi, isolation des clients sur les réseaux invités).
3. **Bouton « Autoriser dans le pare-feu »** : ajoute une règle entrante pour l'exécutable (`netsh advfirewall firewall add rule … program=<chemin>`) via une élévation UAC.

macOS affiche une demande similaire, répétée à chaque version pour une application non signée. Linux n'a généralement pas de pare-feu actif par défaut.

### Docker

Image destinée aux utilisateurs avancés qui installent Linos sur leur serveur. Le conteneur doit tourner en réseau `host` (`--network host`) : sans cela, il ne voit pas l'IP du réseau local (QR code faux) et l'annonce mDNS ne sort pas. Le pare-feu relève alors de l'administrateur du serveur.

## 6. Points de vigilance

- **HTTP en réseau local** : l'API Wake Lock exige HTTPS. Les téléphones peuvent se mettre en veille et couper le WebSocket ; la reconnexion par jeton couvre ce cas.
- **Lecture automatique** : les navigateurs bloquent le son sans interaction préalable. La page `/host` doit obtenir un clic (par exemple « Lancer la partie ») avant la première piste.
