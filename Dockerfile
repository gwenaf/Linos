# Linos for self-hosting on a server:
#   docker run -d --network host -v linos:/data ghcr.io/gwenaf/linos:<version>
# --network host is required: otherwise the QR code shows the container's address and mDNS stays inside.
# Packs go in the volume's packs/ folder. The pack editor is off (LINOS_EDIT=off): it only answers the host machine.
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
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /linos ./cmd/linos

FROM scratch
COPY --from=server /linos /linos
ENV LINOS_HOME=/data LINOS_EDIT=off
VOLUME /data
EXPOSE 7777
ENTRYPOINT ["/linos"]
