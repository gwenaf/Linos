# Linos

Blind test open-source, portable, hors ligne. Un binaire Go unique sert l'écran hôte (`/host`, TV) et les manettes joueurs (`/play`, mobile, rejointes via QR code). Specs complètes : `docs/SPECS.md`.

## Stack

- Backend : Go pur, `CGO_ENABLED=0` obligatoire (binaire unique cross-plateforme, lançable depuis clé USB).
- Dépendances Go autorisées : `coder/websocket`, `skip2/go-qrcode`, `dhowden/tag`, `grandcat/zeroconf`. Le reste = stdlib. Toute nouvelle dépendance doit être justifiée.
- Front : TypeScript + Vite (Vue ou React, pas encore tranché), buildé dans `web/dist` puis embarqué via `embed`.
- Médias lus par `<audio>`/`<video>` natifs du navigateur. Go ne décode jamais de média.

## Arborescence

```
cmd/linos/          point d'entrée, câblage uniquement
internal/server/    HTTP, routes, service du front embarqué
internal/ws/        protocole temps réel : join, buzz, validate, score
internal/qrcode/    QR code de l'URL de connexion
internal/network/   3 modes (WiFi, hotspot Windows, tunnel Cloudflare) : ne change que l'URL
internal/mdns/      annonce blindtest.local
internal/pack/      packs .zip + manifest.json
internal/tags/      lecture tags ID3 à l'import
internal/providers/ interface commune : local, Spotify, YouTube
web/                front Vite ; web/embed.go expose web/dist au binaire
web/src/host/       écran hôte
web/src/play/       manette mobile
docs/               specs
```

Ordre de dev : serveur + protocole WebSocket d'abord, tout le reste se branche dessus.

## Conventions

- Code simple : pas d'abstraction sans deuxième implémentation réelle (exception : `providers`, prévu multi-implémentation par les specs).
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
