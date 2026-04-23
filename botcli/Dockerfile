# syntax=docker/dockerfile:1

# nito-bot — headless nito bot image.
#
# Two-stage build. The runtime uses distroless/static-debian12:nonroot so
# there is no shell, no package manager, and no CA bundle beyond what
# distroless ships. The bot is pure Go (CGO_ENABLED=0); the voice/audio
# stack lives in engine/sounds and is never imported from the botcli path.
#
# /data is the persistence volume: bot-state.yml, .env, and the
# ~/.nito/<broker>/... key directory all live under it (NITO_BOT_DATA +
# HOME both point here, so keys.LoadOrGenerate writes where we expect).

FROM golang:1.25-alpine AS build
WORKDIR /src

# Copy go.mod/go.sum first so the module download layer caches independent
# of source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Strip symbols + DWARF: buildinfo + basic stack traces still work, image
# shrinks by ~4MB.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$(go env GOARCH) \
    go build -trimpath -ldflags="-s -w" -o /out/nito-bot ./cmd/nito-bot

FROM gcr.io/distroless/static-debian12:nonroot
# distroless nonroot is uid 65532; /data must be writable by that uid
# before `docker run` or the bot can't write its state file. The
# generate-bot-image.sh wrapper chowns the host path.
WORKDIR /data

ENV NITO_BOT_DATA=/data \
    HOME=/data

COPY --from=build /out/nito-bot /usr/local/bin/nito-bot

# No shell, no entrypoint.sh: the binary sources /data/.env itself
# (botcli.LoadEnvFile). Users may still override with `docker run -e`.
ENTRYPOINT ["/usr/local/bin/nito-bot"]
