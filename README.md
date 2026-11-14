# Linos

**Le blind test portable et hors ligne.** La TV joue les extraits, les téléphones servent de buzzers.

[![Release](https://img.shields.io/github/v/release/gwenaf/Linos)](https://github.com/gwenaf/Linos/releases)
[![Licence : AGPL-3.0](https://img.shields.io/badge/licence-AGPL--3.0-blue)](LICENSE)

Linos est un seul fichier exécutable, sans installation ni compte : il tient sur une clé USB et fonctionne sans Internet. Les joueurs rejoignent la partie en scannant un QR code, sans rien installer.

## Fonctionnalités

- **Trois écrans** : le pilotage sur le PC, l'affichage du jeu sur la TV, une manette sur chaque téléphone.
- **Buzzer ou réponses simultanées**, à l'oral ou tapées sur le téléphone, avec tolérance aux fautes de frappe.
- **En solo ou par équipes**, avec un maître du jeu ou en validation automatique.
- **Des règles à la carte** : points selon la rapidité, manches, choix de thème, mises, jokers, éliminations, mort subite…
- **Un éditeur intégré** : déposez vos musiques, Linos lit artiste et titre dans les tags et crée le blind test. Ajoutez ensuite manches, indices, effets visuels (flou, pixelisation) et réglez chaque extrait à l'oreille.
- **Des packs à partager** : un blind test s'exporte en un fichier `.linospack` à envoyer à vos amis.

## Démarrage rapide

1. Téléchargez le binaire de votre système dans les [releases](https://github.com/gwenaf/Linos/releases).
2. Lancez-le : la page de pilotage s'ouvre dans votre navigateur.
3. Ouvrez l'**Éditeur de packs** et déposez-y un dossier de musiques.
4. Cliquez sur **Ouvrir l'écran de jeu** pour l'afficher sur la TV, puis faites scanner le QR code aux joueurs, connectés au même Wi-Fi.

Tout est rangé à côté de l'exécutable : vos packs dans `packs/`, le journal dans `linos.log`.

<details>
<summary>macOS et Linux : le binaire ne se lance pas</summary>

Le binaire n'est pas signé par Apple. Après le téléchargement :

```sh
chmod +x linos-darwin-arm64
xattr -d com.apple.quarantine linos-darwin-arm64   # macOS uniquement
./linos-darwin-arm64
```

</details>

<details>
<summary>Les téléphones n'arrivent pas à se connecter</summary>

La page de pilotage propose une aide pas à pas : pare-feu, réseau Wi-Fi « public », isolation des appareils sur les box et les Wi-Fi publics. En dernier recours, le point d'accès mobile de Windows fonctionne partout.

</details>

## Créer un blind test

Dans l'éditeur (`/edit`), déposez un dossier ou des fichiers audio : un pack est créé, les réponses sont tirées des tags ou du nom « Artiste - Titre ». Vérifiez-les, choisissez le début de chaque extrait, puis lancez la partie avec **Jouer ce pack**.

Pour le partager, **exportez-le** en `.linospack` : si [ffmpeg](https://ffmpeg.org) est installé, les médias sont découpés aux extraits joués pour alléger le fichier. Votre ami dépose ce fichier dans son éditeur pour l'ajouter à ses packs.

Un pack ne contient que des médias (audio, vidéo, image) et un fichier de description. Linos refuse tout autre fichier, si bien qu'un pack reçu ne peut rien exécuter.

> **Droits musicaux.** Linos ne fournit ni ne diffuse aucune musique : vous jouez vos propres fichiers. Partager un pack revient à partager ces fichiers, à faire dans le respect des droits d'auteur. Pour une diffusion publique (bar, événement), la licence de diffusion (la SACEM en France) reste à la charge de l'organisateur.

Le format des packs est décrit dans [`docs/manifest.example.json`](docs/manifest.example.json).

## Héberger sur un serveur

Une image Docker est publiée à chaque release, sous le numéro de version (`v0.1.0`…) et `latest` :

```sh
docker run -d -p 49154:49154 -v linos:/data \
  -e LINOS_ADMIN_TOKEN="$(openssl rand -base64 32)" \
  ghcr.io/gwenaf/linos:v0.1.0
```

Gardez ce jeton (`docker inspect` le retrouve) et connectez-vous avec sur `/login` pour piloter la partie et éditer les packs. Sans jeton, seules les pages des joueurs répondent. Le HTTPS (via un reverse proxy) et la sécurité du serveur restent à votre charge.

## Vérifier un téléchargement

Chaque binaire et chaque image Docker est accompagné d'une attestation signée (Sigstore, via GitHub) : elle prouve qu'il a été construit par le workflow de release de ce dépôt, depuis le code du tag. Avec la [CLI GitHub](https://cli.github.com) :

```sh
gh attestation verify linos-linux-amd64 --repo gwenaf/Linos
gh attestation verify oci://ghcr.io/gwenaf/linos:v0.1.0 --repo gwenaf/Linos
```

## Configuration

| Variable | Rôle |
|---|---|
| `LINOS_HOME` | Dossier de `packs/` et `linos.log` (défaut : celui de l'exécutable) |
| `LINOS_ADDR` | Adresse d'écoute (défaut `:49154`) |
| `LINOS_ADMIN_TOKEN` | Active `/login` pour piloter un serveur (32 caractères minimum) |
| `LINOS_PUBLIC_URL` | Adresse donnée aux joueurs (code QR) quand Linos ne la voit pas : Docker, reverse proxy. Ex. `https://quiz.example` |
| `LINOS_EDIT` | `off` désactive l'éditeur |
| `LINOS_BROWSER` | `off` n'ouvre pas le navigateur au lancement |
| `LINOS_LOG_LEVEL` | `debug` pour tout journaliser |

## Développement

Prérequis : [Go](https://go.dev) et [Node.js](https://nodejs.org).

```sh
git config core.hooksPath .githooks          # vérifications avant chaque commit
cd web && npm install && npm run build && cd ..
go test ./...
LINOS_HOME=bin go run ./cmd/linos
```

Le serveur est écrit en Go (stdlib et une poignée de dépendances, sans CGO). Le front est en TypeScript avec Preact, puis embarqué dans le binaire. Les choix techniques sont dans [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md), le protocole dans [`docs/PROTOCOL.md`](docs/PROTOCOL.md) et les specs dans [`docs/SPECS.md`](docs/SPECS.md).

## Contribuer

Toute contribution est la bienvenue. Les commits suivent [Conventional Commits](https://www.conventionalcommits.org/fr/). Le code doit-être couvert à 100% par des tests, pensez à bien tagger les fichiers de code générés automatiquement (migrations ou autre) afin de les exclure de la vérification automatique. 

## Utilisation de l'IA

Linos est développé avec l'aide de l'IA. Seul le développement, l'aspect artistique et la rédaction documentaire est assistée via l'IA. Le concept et les choix techniques restent à la charge du développeur.
Les contributions peuvent tout à fait être rédigées via assistance de l'IA, tant que le code répond aux exigences de couverture de tests.

## Licence

[AGPL-3.0](LICENSE).
