# Linos

Blind test open-source, portable, hors ligne. Un binaire Go unique sert la page de pilotage (`/control`, PC hôte), l'affichage du jeu (`/host`, TV) et les manettes joueurs (`/play`, mobile, rejointes via QR code). Specs complètes : `docs/SPECS.md`.

## Stack

- Backend : Go pur, `CGO_ENABLED=0` obligatoire (binaire unique cross-plateforme, lançable depuis clé USB).
- Dépendances Go autorisées : `coder/websocket`, `skip2/go-qrcode`, `dhowden/tag`, `grandcat/zeroconf`, `golang.org/x/text`. Le reste = stdlib. Toute nouvelle dépendance doit être justifiée.
- Front : TypeScript + Vite + Preact (`@preact/signals`), buildé dans `web/dist` puis embarqué via `embed`.
- Décisions techniques : `docs/ARCHITECTURE.md`. Protocole : `docs/PROTOCOL.md`.
- Médias lus par `<audio>`/`<video>` natifs du navigateur. Go ne décode jamais de média.

## Arborescence

```
cmd/linos/          point d'entrée, câblage uniquement
internal/server/    HTTP, routes, service du front embarqué
internal/game/      logique de jeu (salon, lobby, buzz, scores, états), testable sans réseau
internal/ws/        transport WebSocket : rôles, lecture/écriture vers game.Room
internal/qrcode/    QR code de l'URL de connexion
internal/network/   2 modes (WiFi, hotspot Windows) : ne change que l'URL ; pare-feu
internal/mdns/      annonce blindtest.local
internal/pack/      gamepacks (manifest.json + médias locaux)
internal/tags/      lecture tags ID3 à l'import
web/                front Vite ; web/embed.go expose web/dist au binaire
web/src/control/    pilotage de la partie (PC hôte)
web/src/host/       affichage du jeu (TV)
web/src/play/       manette mobile
docs/               specs
```

Ordre de dev : serveur + protocole WebSocket d'abord, tout le reste se branche dessus.

## Conventions

- Code simple : pas d'abstraction sans deuxième implémentation réelle.
- Médias : fichiers locaux uniquement, aucune plateforme de streaming (voir `docs/ARCHITECTURE.md`).
- Go : `gofmt`, `go vet` propres. Erreurs retournées, pas de `panic` hors `main`.
- Code, identifiants et commentaires en anglais ; docs et specs en français.
- Commits : Conventional Commits (`feat:`, `fix:`, `chore:`…).
- Licence AGPL-3.0.

## Commandes

```sh
git config core.hooksPath .githooks   # une fois après clone : active le pre-commit
go vet ./...
go test ./...
CGO_ENABLED=0 go build -o bin/linos ./cmd/linos
```

Le hook `pre-commit` vérifie `gofmt` + `go vet` sur les `.go` stagés, et lance `npm run lint` / `typecheck` dans `web/` si ces scripts existent.
