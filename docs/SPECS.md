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

- Vue ou React, build Vite, compilé puis embarqué dans le binaire
- Route `/host` (plein écran TV) et route `/play` (mobile)
- Lecture via `<audio>` / `<video>` natifs : le navigateur décode, pas Go

**Exécution**

- Un seul `.exe` (~12 Mo), exécutable depuis une clé USB, sans installation
- Ouvre le navigateur système sur `localhost` (Chrome embarque Widevine, WebView2 non)

## Réseau — 3 modes, même code

1. **WiFi existant** — mode par défaut, aucun droit admin
2. **Hotspot Windows partagé** — optionnel, demande une élévation
3. **Tunnel Cloudflare** — joueurs en 4G, aucun réseau commun requis

Seule la génération de l'URL du QR code change entre les trois.

## Contenu

- Packs `.zip` contenant un `manifest.json`
- Import par glisser-déposer d'un dossier de mp3, lecture automatique des tags
- Éditeur de packs intégré
- Providers optionnels derrière une interface commune : fichier local, Spotify (OAuth PKCE, `client_id` saisi par l'utilisateur), YouTube

## Distribution

- Cross-compilation Windows / macOS / Linux via GitHub Actions
- Signature SignPath (gratuite en open-source) pour éviter SmartScreen
- Manifestes `winget` et `scoop`, image Docker pour auto-hébergement
- Licence AGPL (ou MIT si adoption maximale privilégiée)

## Point de départ

Serveur Go + protocole WebSocket : `join`, `buzz`, `validate`, `score`. Tout le reste se branche dessus.