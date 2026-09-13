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
- Route `/host` (plein écran TV) et route `/play` (mobile)
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
- Import par glisser-déposer d'un dossier de mp3, lecture automatique des tags
- Aucune plateforme de streaming dans le jeu : leurs conditions interdisent l'usage en jeu ou quiz (Spotify, Apple, SoundCloud)
- Création de gamepacks : builder dans un projet séparé, avec import de playlists. Contrat commun : JSON Schema du manifest, publié dans ce dépôt

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
- Manifestes `winget` et `scoop`, image Docker pour auto-hébergement
- Licence AGPL-3.0

## Point de départ

Serveur Go + protocole WebSocket : `join`, `buzz`, `validate`, `score`. Tout le reste se branche dessus.