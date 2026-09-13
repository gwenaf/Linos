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
internal/server/    HTTP : pages, /ws, /media, /api/join, /qr.png (ces trois derniers réservés au PC hôte)
internal/game/      logique de jeu (salon, lobby, buzz, pistes, scores, états), testable sans réseau
internal/ws/        transport WebSocket : rôles, lecture/écriture vers game.Room
internal/network/   adresses IP locales pour le QR code ; pare-feu à venir
internal/mdns/      annonce blindtest.local
internal/pack/      gamepacks (manifest.json + médias locaux), validation
internal/tags/      lecture tags ID3 à l'import
web/                front Vite ; web/embed.go embarque web/dist dans le binaire
web/src/ws.ts       connexion WebSocket commune (reconnexion, jetons)
web/src/control.tsx pilotage de la partie (PC hôte ou téléphone maître du jeu)
web/src/host.tsx    affichage du jeu (TV) : médias, dévoilements, indices
web/src/play.tsx    manette mobile
docs/               specs
```

Ordre de dev : serveur + protocole WebSocket d'abord, tout le reste se branche dessus.

## Conventions

- Code simple : pas d'abstraction sans deuxième implémentation réelle.
- Médias : fichiers locaux uniquement, aucune plateforme de streaming (voir `docs/ARCHITECTURE.md`).
- Go : `gofmt`, `go vet` propres. Erreurs retournées, pas de `panic` hors `main`.
- Tests Go : couverture 100 % visée (seule `main()` est exclue). Logique de jeu testée sans réseau via `Room.Connect`/`Receive`.
- Code, identifiants et commentaires en anglais ; docs et specs en français.
- Commits : Conventional Commits (`feat:`, `fix:`, `chore:`…).
- Licence AGPL-3.0.

## Commandes

```sh
git config core.hooksPath .githooks   # une fois après clone : active le pre-commit
go vet ./...
go test -cover ./...
cd web && npm install && npm run build && cd ..   # front, à rebuilder avant go build
CGO_ENABLED=0 go build -o bin/linos ./cmd/linos   # packs à placer dans bin/packs/
```

Variables d'environnement :

- `LINOS_HOME` : dossier contenant `packs/` et `linos.log` (défaut : dossier de l'exécutable). Avec `go run`, l'exécutable est dans un dossier temporaire : lancer `LINOS_HOME=bin go run ./cmd/linos`.
- `LINOS_LOG_LEVEL` : `debug` pour tracer chaque requête et message WebSocket (défaut : info).
- `LINOS_WEB_DIR` : servir le front depuis le disque (`web/dist`), avec `npm run dev` dans `web/` qui rebuild à chaque modification.
- `LINOS_ADDR` : adresse d'écoute (défaut `:7777`).

Le hook `pre-commit` vérifie `gofmt` + `go vet` sur les `.go` stagés, et lance `npm run lint` / `typecheck` dans `web/` si ces scripts existent.
