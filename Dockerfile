# Linos for self-hosting on a server, run from the browser after signing in at /login:
#   docker run -d -p 49154:49154 -e LINOS_ADMIN_TOKEN=$(openssl rand -base64 32) -v linos:/data ghcr.io/gwenaf/linos:<version>
# Without LINOS_ADMIN_TOKEN, nothing but the player pages answers from another machine.
# HTTPS (a reverse proxy) is up to the server: the admin cookie would otherwise travel in clear.
# On a local network server, --network host instead of -p gives the QR code the LAN address and lets mDNS out.
FROM node:lts-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26-alpine AS server
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /linos ./cmd/linos && mkdir /data

FROM scratch
COPY --from=server /linos /linos
# Unprivileged user; a named volume starts with this owner. A bind mount must be writable by UID 65534.
COPY --from=server --chown=65534:65534 /data /data
USER 65534:65534
ENV LINOS_HOME=/data
VOLUME /data
EXPOSE 49154
ENTRYPOINT ["/linos"]
