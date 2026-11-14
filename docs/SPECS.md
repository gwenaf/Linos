# Linos — Spécifications

Application de blind test open-source, portable et hors ligne : un hôte diffuse des médias (musique, vidéo, texte) sur grand écran, les joueurs rejoignent la partie en scannant un QR code avec leur téléphone.

## Principe

Un binaire unique lance un serveur local. L'écran hôte et les téléphones sont deux pages web servies par ce binaire : les téléphones ne sont que des manettes (buzzer, réponse, score).

## Stack

**Backend — Go (pur, sans CGO)**

| Usage | Librairie |
|---|---|
| Serveur HTTP + front embarqué | `net/http`, `embed` |
| Temps réel (buzzers, scores) | `coder/websocket` |
| QR code de connexion | `skip2/go-qrcode` |
| Tags ID3 à l'import | `dhowden/tag` |
| mDNS (`blindtest.local`) | `grandcat/zeroconf` |

**Front — TypeScript**

- Preact (`@preact/signals`), build Vite, compilé puis embarqué dans le binaire
- Route `/control` (page principale sur le PC hôte : pilotage, réponses, validation, indices), route `/host` (affichage du jeu, ouvert depuis `/control` dans une nouvelle fenêtre) et route `/play` (mobile). Un téléphone peut aussi servir de maître du jeu (invitation par QR code) pour jouer avec un seul écran
- Lecture via `<audio>` / `<video>` natifs : le navigateur décode, pas Go

**Exécution**

- Un seul `.exe` (~12 Mo), exécutable depuis une clé USB, sans installation
- Ouvre le navigateur système sur `localhost`

## Réseau — 2 modes, même code

1. **WiFi existant** — mode par défaut, aucun droit admin
2. **Hotspot Windows partagé** — optionnel, activé manuellement par l'utilisateur en v1, détecté par Linos

Seule la génération de l'URL du QR code change entre les deux. Tous les joueurs sont sur le même réseau local : pas de tunnel ni de jeu à distance (latence inéquitable au buzz, étape d'installation supplémentaire).

## Contenu

- Le jeu lit uniquement des fichiers locaux, regroupés dans un **gamepack** `.linospack` (archive ZIP) : un `manifest.json` et les médias (audio, vidéo, image), rangés dans le dossier `packs/` à côté de l'exécutable. Un dossier non zippé au même contenu est aussi accepté (forme d'édition). Exemple de manifest : `docs/manifest.example.json`
- Un gamepack décrit des pistes, des thèmes et des manches (thèmes et manches facultatifs), ainsi que les règles par défaut
- Import rapide par glisser-déposer d'un dossier de fichiers audio dans `/edit` : pack généré dans `packs/` depuis les tags, ouvert dans l'éditeur
- Aucune plateforme de streaming dans le jeu : leurs conditions interdisent l'usage en jeu ou quiz (Spotify, Apple, SoundCloud)
- Création de gamepacks : éditeur `/edit` intégré (PC hôte ou administrateur d'un serveur, désactivable par `LINOS_EDIT=off`). On y dépose ses musiques pour obtenir un pack, ou un `.linospack` reçu ; un pack exporté se télécharge pour être partagé. Un assistant guide la création (joueurs, arbitre, manches) ; les manches partent de types prêts à l'emploi, avec un résumé en clair de leurs règles ; les questions se glissent entre manches et thèmes. Manches, thèmes, règles, minutage, image fixe, effets visuels (flou, pixelisation…) appliqués en direct à la lecture
- Export : ffmpeg (facultatif, trouvé dans le PATH) découpe les médias aux segments utilisés pour réduire le `.linospack`

## Jeu

- Deux contrôles : avec maître du jeu (validation humaine) ou autonome (validation automatique des réponses)
- Individuel ou par équipes
- Réponses au buzz ou simultanées, à l'oral ou sur l'appareil ; éléments à deviner en texte libre, QCM ou nombre
- Points fixes, dégressifs selon la rapidité (baisse linéaire), par classement ou sur mise
- Règles surchargeables à trois niveaux : pack, manche, puis lobby
- Protocole et états : `docs/PROTOCOL.md`

## Distribution

- Cross-compilation Windows / macOS / Linux via GitHub Actions
- Signature SignPath (gratuite en open-source) pour éviter SmartScreen
- Homebrew (macOS, Linux) et Scoop (Windows) depuis ce dépôt, `winget` plus tard ; image Docker pour auto-hébergement (accès administrateur par jeton)
- Licence AGPL-3.0

## Point de départ

Serveur Go + protocole WebSocket : `join`, `buzz`, `validate`, `score`. Tout le reste se branche dessus.